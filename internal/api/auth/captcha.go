// 图形验证码 —— 本地登录安控加强。
//
// 自绘 SVG(无第三方依赖):4 位随机码(去掉易混淆字符),大小写不敏感;
// 答案为服务端私有状态,仅以 5×7 点阵笔画路径绘制,下发的 SVG 不含答案的文本/属性表示,
// 只能由像素/路径形态人工识别;验证码一次性、5 分钟过期、答错即作废,
// 并与领取来源(IP+User-Agent 指纹)绑定,换来源重放无效。
// 领取与登录按来源/账号双维限速退避(有界内存实现;多副本部署时计数各自独立,
// 实际阈值≈配置值×副本数,网关应再叠加一层按来源限流)。
// SSO(Casdoor/ZITADEL)跳转登录不受影响。
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"gitcode.com/opic-ontology/opic-techbase/internal/api/common"
	"gitcode.com/opic-ontology/opic-techbase/pkg/errcode"
)

const captchaTTL = 5 * time.Minute

// captchaEntry 一条待验证的验证码(答案只存服务端内存,绝不随响应下发)。
type captchaEntry struct {
	answer  string
	bind    string // 领取来源指纹(sha256(IP|UA)),校验时须一致
	expires time.Time
}

var captchaStore = struct {
	sync.Mutex
	m map[string]captchaEntry
}{m: map[string]captchaEntry{}}

// captchaMaxEntries 待验证条目上限(有界内存;正常流量远低于此)。
const captchaMaxEntries = 10000

// captchaCharset 去掉 0/O/1/I/l 等易混淆字符。
const captchaCharset = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

// 限速实例(键数有界,见 throttle)。
// 来源维度(IP+UA)可被伪造的头(X-Forwarded-For)绕过,账号维度不可伪造,是抗口令爆破的主控制。
var (
	captchaIssueLimit = newThrottle(20, time.Minute, time.Minute)     // 验证码领取:每来源每分钟 20 次
	loginSourceLimit  = newThrottle(10, 5*time.Minute, 5*time.Minute) // 登录失败:每来源 5 分钟 10 次
	loginAccountLimit = newThrottle(5, 5*time.Minute, 15*time.Minute) // 登录失败:每账号 5 分钟 5 次
)

// Captcha 下发图形验证码:GET /api/auth/captcha → {captcha_id, image}。
// 按来源限速;答案与来源指纹只存服务端,响应仅含 captcha_id 与点阵图。
func Captcha(c *gin.Context) {
	src := sourceKey(c)
	if ok, retry := captchaIssueLimit.take(src); !ok {
		c.JSON(200, errcode.New(errcode.ErrLogin, waitMsg(retry), nil))
		return
	}
	code := randomCode(4)
	id := randomCode(16) + fmtTime()
	captchaStore.Lock()
	// 顺手清理过期项,防止内存无界增长
	now := time.Now()
	for k, e := range captchaStore.m {
		if now.After(e.expires) {
			delete(captchaStore.m, k)
		}
	}
	// 未过期条目过多(高并发领取)时按迭代顺序淘汰,保证内存上界
	for k := range captchaStore.m {
		if len(captchaStore.m) < captchaMaxEntries {
			break
		}
		delete(captchaStore.m, k)
	}
	captchaStore.m[id] = captchaEntry{answer: code, bind: src, expires: now.Add(captchaTTL)}
	captchaStore.Unlock()

	svg := renderSVG(code)
	common.OKJSON(c, gin.H{
		"captcha_id": id,
		"image":      "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg)),
		"ttl":        int(captchaTTL.Seconds()),
	}, "")
}

// VerifyCaptcha 校验并销毁(一次性)。id/code 为空、来源不符或已过期均 false。
// 答案比较按大小写不敏感、常量时间。
func VerifyCaptcha(c *gin.Context, id, code string) bool {
	if id == "" || code == "" {
		return false
	}
	captchaStore.Lock()
	e, ok := captchaStore.m[id]
	if ok {
		delete(captchaStore.m, id) // 无论对错都销毁,防重放
	}
	captchaStore.Unlock()
	if !ok || time.Now().After(e.expires) {
		return false
	}
	if e.bind != sourceKey(c) {
		return false // 与领取来源绑定:换来源(或跨会话共享)重放无效
	}
	return subtle.ConstantTimeCompare([]byte(strings.ToUpper(strings.TrimSpace(code))), []byte(e.answer)) == 1
}

// sourceKey 领取来源指纹(IP + User-Agent 的 sha256 前 16 位);仅存服务端,不随响应下发。
func sourceKey(c *gin.Context) string {
	sum := sha256.Sum256([]byte(c.ClientIP() + "|" + c.Request.UserAgent()))
	return hex.EncodeToString(sum[:8])
}

// waitMsg 限速退避提示(秒级粗粒度)。
func waitMsg(d time.Duration) string {
	sec := int(d.Seconds()) + 1
	if sec < 1 {
		sec = 1
	}
	return "尝试过于频繁，请 " + itoa(sec) + " 秒后再试"
}

// ---------- 有界内存限速/失败退避 ----------
//
// 单副本语义:计数保存在进程内存,多副本部署时各副本独立计数(实际阈值≈本值×副本数),
// 重启后计数清零;部署侧网关/负载均衡应再叠加一层按来源限流。

type throttleEntry struct {
	windowStart time.Time
	count       int
	blockUntil  time.Time
}

// throttle 固定窗口计数 + 超限冷却;键数有界(throttleMaxKeys),防止来源/账号被刷导致内存无界增长。
type throttle struct {
	limit    int
	window   time.Duration
	cooldown time.Duration
	mu       sync.Mutex
	m        map[string]*throttleEntry
}

const throttleMaxKeys = 4096

func newThrottle(limit int, window, cooldown time.Duration) *throttle {
	return &throttle{limit: limit, window: window, cooldown: cooldown, m: map[string]*throttleEntry{}}
}

// take 计数一次尝试:处于冷却或本次超限时返回 false 与剩余冷却时间。
func (t *throttle) take(key string) (bool, time.Duration) {
	if key == "" {
		key = "-"
	}
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.m) >= throttleMaxKeys {
		t.evictLocked(now)
	}
	e := t.m[key]
	if e == nil {
		e = &throttleEntry{windowStart: now}
		t.m[key] = e
	}
	if now.Before(e.blockUntil) {
		return false, time.Until(e.blockUntil)
	}
	if now.Sub(e.windowStart) > t.window {
		e.windowStart, e.count = now, 0
	}
	e.count++
	if e.count > t.limit {
		e.blockUntil = now.Add(t.cooldown)
		e.windowStart, e.count = now, 0
		return false, t.cooldown
	}
	return true, 0
}

// blocked 只查询是否处于冷却中(不计数)。
func (t *throttle) blocked(key string) (bool, time.Duration) {
	if key == "" {
		return false, 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.m[key]
	if e == nil || !time.Now().Before(e.blockUntil) {
		return false, 0
	}
	return true, time.Until(e.blockUntil)
}

// reset 清除键的计数与冷却(仅成功路径调用,如登录成功清零账号维度失败计数)。
func (t *throttle) reset(key string) {
	if key == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.m, key)
}

// evictLocked 有界清理:先删窗口与冷却都已过的条目;仍超上限时按迭代顺序淘汰
// (近似均匀,可能提前失效个别冷却条目,换取内存上界)。
func (t *throttle) evictLocked(now time.Time) {
	for k, e := range t.m {
		if now.After(e.blockUntil) && now.Sub(e.windowStart) > t.window {
			delete(t.m, k)
		}
	}
	for k := range t.m {
		if len(t.m) < throttleMaxKeys {
			break
		}
		delete(t.m, k)
	}
}

func randomCode(n int) string {
	b := make([]byte, n)
	max := big.NewInt(int64(len(captchaCharset)))
	for i := range b {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			v = big.NewInt(time.Now().UnixNano() % max.Int64())
		}
		b[i] = captchaCharset[v.Int64()]
	}
	return string(b)
}

// randomInt [0,n) 的加密随机数(失败回退时间纳秒);用于视觉抖动与噪声,避免由答案推导。
func randomInt(n int) int {
	if n <= 1 {
		return 0
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return int(time.Now().UnixNano() % int64(n))
	}
	return int(v.Int64())
}

func fmtTime() string { return strings.ToLower(randBase32(time.Now().UnixNano())) }

func randBase32(v int64) string {
	const digits = "0123456789abcdefghijklmnopqrstuv"
	out := make([]byte, 0, 12)
	for ; v > 0; v >>= 5 {
		out = append(out, digits[v&31])
	}
	return string(out)
}

// palette 低饱和底色,与登录页白底蓝字风格一致。
var palette = []string{"#e8f1fd", "#eef7ee", "#fdf3e7", "#f3eefb"}

// glyphRows 5×7 点阵字形(覆盖 captchaCharset 全部 30 个字符与兜底 '?';
// 每项 7 行 × 5 列,'1' 为笔画点,行内以逗号分隔)。答案字符本身只存在于服务端内存。
var glyphRows = map[rune]string{
	'2': "01110,10001,00001,00010,00100,01000,11111",
	'3': "11110,00001,00001,01110,00001,00001,11110",
	'4': "00010,00110,01010,10010,11111,00010,00010",
	'5': "11111,10000,11110,00001,00001,10001,01110",
	'6': "00110,01000,10000,11110,10001,10001,01110",
	'7': "11111,00001,00010,00100,01000,01000,01000",
	'8': "01110,10001,10001,01110,10001,10001,01110",
	'9': "01110,10001,10001,01111,00001,00010,01100",
	'A': "01110,10001,10001,11111,10001,10001,10001",
	'B': "11110,10001,10001,11110,10001,10001,11110",
	'C': "01110,10001,10000,10000,10000,10001,01110",
	'D': "11110,10001,10001,10001,10001,10001,11110",
	'E': "11111,10000,10000,11110,10000,10000,11111",
	'F': "11111,10000,10000,11110,10000,10000,10000",
	'G': "01110,10001,10000,10111,10001,10001,01111",
	'H': "10001,10001,10001,11111,10001,10001,10001",
	'J': "00111,00010,00010,00010,00010,10010,01100",
	'K': "10001,10010,10100,11000,10100,10010,10001",
	'M': "10001,11011,10101,10101,10001,10001,10001",
	'N': "10001,10001,11001,10101,10011,10001,10001",
	'P': "11110,10001,10001,11110,10000,10000,10000",
	'Q': "01110,10001,10001,10001,10101,10010,01101",
	'R': "11110,10001,10001,11110,10100,10010,10001",
	'S': "01111,10000,10000,01110,00001,00001,11110",
	'T': "11111,00100,00100,00100,00100,00100,00100",
	'U': "10001,10001,10001,10001,10001,10001,01110",
	'V': "10001,10001,10001,10001,10001,01010,00100",
	'W': "10001,10001,10001,10101,10101,11011,10001",
	'X': "10001,10001,01010,00100,01010,10001,10001",
	'Y': "10001,10001,01010,00100,00100,00100,00100",
	'Z': "11111,00001,00010,00100,01000,10000,11111",
	'?': "01110,10001,00001,00110,00100,00000,00100",
}

// renderSVG 以点阵笔画路径绘制 4 位字符(随机旋转/抖动/颜色)+ 随机噪声线。
// 交付物只含几何指令(数字与 M/h/v/z),不含答案字符的文本节点或属性。
func renderSVG(code string) string {
	var sb strings.Builder
	bg := palette[randomInt(len(palette))]
	sb.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="120" height="40" viewBox="0 0 120 40">`)
	sb.WriteString(`<rect width="120" height="40" rx="6" fill="` + bg + `"/>`)
	colors := []string{"#4c7dd0", "#5aa35a", "#c9803e", "#8a6fc0"}
	// 噪声线(坐标与答案无关,避免由几何形状反推答案)
	for i := 0; i < 4; i++ {
		x1 := 6 + randomInt(90)
		y1 := 4 + randomInt(30)
		sb.WriteString(`<line x1="` + itoa(x1) + `" y1="` + itoa(y1) +
			`" x2="` + itoa(x1+18) + `" y2="` + itoa(y1+randomInt(10)-5) +
			`" stroke="` + colors[i] + `" stroke-width="1" opacity="0.5"/>`)
	}
	// 字符:5×7 点阵按 3px 单元栅格化为路径,整体旋转/抖动
	for i, ch := range code {
		rows, ok := glyphRows[ch]
		if !ok {
			rows = glyphRows['?']
		}
		x := 10 + i*26
		y := 9 + randomInt(3)
		rot := randomInt(15) - 7 // -7..7 度
		fc := colors[randomInt(len(colors))]
		sb.WriteString(`<g transform="rotate(` + itoa(rot) + ` ` + itoa(x+8) + ` ` + itoa(y+10) + `)" fill="` + fc + `">`)
		sb.WriteString(`<path d="` + glyphPath(rows, x, y, 3) + `"/>`)
		sb.WriteString(`</g>`)
	}
	sb.WriteString(`</svg>`)
	return sb.String()
}

// glyphPath 把 5×7 点阵转换为矩形子路径(水平连续点合并为一条),只输出几何指令。
func glyphPath(rows string, x0, y0, unit int) string {
	var sb strings.Builder
	for r, row := range strings.Split(rows, ",") {
		run := 0
		for c := 0; c <= len(row); c++ {
			if c < len(row) && row[c] == '1' {
				run++
				continue
			}
			if run > 0 {
				x := x0 + (c-run)*unit
				y := y0 + r*unit
				sb.WriteString("M" + itoa(x) + " " + itoa(y) + "h" + itoa(run*unit) +
					"v" + itoa(unit) + "h-" + itoa(run*unit) + "z")
				run = 0
			}
		}
	}
	return sb.String()
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
