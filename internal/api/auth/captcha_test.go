package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// testCaptchaCtx 构造带来源(RemoteAddr/UA)的测试上下文。
func testCaptchaCtx(ip, ua string) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	c.Request.RemoteAddr = ip + ":12345"
	c.Request.Header.Set("User-Agent", ua)
	return c
}

func TestVerifyCaptcha(t *testing.T) {
	c := testCaptchaCtx("203.0.113.7", "test-agent")
	id := randomCode(16)
	captchaStore.m[id] = captchaEntry{answer: "AB3D", bind: sourceKey(c), expires: time.Now().Add(captchaTTL)}

	// 大小写不敏感 + 首尾空白容忍
	if !VerifyCaptcha(c, id, " ab3d ") {
		t.Fatal("正确验证码应通过(大小写不敏感)")
	}
	// 一次性:销毁后同码重放必须失败
	if VerifyCaptcha(c, id, "AB3D") {
		t.Fatal("验证码应一次性,重放须失败")
	}

	// 来源绑定:换来源(IP 或 UA 变化)不得通过
	id2 := randomCode(16)
	captchaStore.m[id2] = captchaEntry{answer: "XY7K", bind: sourceKey(c), expires: time.Now().Add(captchaTTL)}
	if VerifyCaptcha(testCaptchaCtx("203.0.113.8", "test-agent"), id2, "XY7K") {
		t.Fatal("换来源重放须失败")
	}
	id2b := randomCode(16)
	captchaStore.m[id2b] = captchaEntry{answer: "XY7K", bind: sourceKey(c), expires: time.Now().Add(captchaTTL)}
	if VerifyCaptcha(testCaptchaCtx("203.0.113.7", "other-agent"), id2b, "XY7K") {
		t.Fatal("换 User-Agent 重放须失败")
	}

	// 过期
	id3 := randomCode(16)
	captchaStore.m[id3] = captchaEntry{answer: "XY7K", bind: sourceKey(c), expires: time.Now().Add(-time.Minute)}
	if VerifyCaptcha(c, id3, "XY7K") {
		t.Fatal("过期验证码须失败")
	}

	// 空入参
	if VerifyCaptcha(c, "", "ABCD") || VerifyCaptcha(c, "someid", "") {
		t.Fatal("空 id 或空码须失败")
	}
}

func TestCaptchaSVGNoPlaintext(t *testing.T) {
	// 取全字母答案,确保"答案字符出现在 SVG 中"只可能来自明文回显(几何指令只含 M/h/v/z 与数字)
	svg := renderSVG("WXYZ")
	if !strings.HasPrefix(svg, `<svg xmlns="http://www.w3.org/2000/svg"`) {
		t.Fatal("SVG 输出格式不对")
	}
	if strings.Contains(svg, "<text") {
		t.Fatal("下发的 SVG 不得包含文本节点")
	}
	for _, ch := range "WXYZ" {
		if strings.Contains(svg, string(ch)) {
			t.Fatalf("下发的 SVG 不得包含答案字符 %c", ch)
		}
	}
	if n := strings.Count(svg, "<path"); n != 4 {
		t.Fatalf("应绘制 4 个字形路径,实际 %d", n)
	}
	if got := randomCode(4); len(got) != 4 {
		t.Fatalf("randomCode 长度=%d,应为 4", len(got))
	}
}
