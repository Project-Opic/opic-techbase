// AI 模型设置 —— 多供应商账号管理（模型设置页对齐"填入各提供商 API 密钥即可使用其模型"形态）。
// 目录(GET catalog) / 账号 CRUD / 连通性测试 / 上游模型目录获取。
package adminconsole

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"gitcode.com/opic-ontology/opic-techbase/internal/api/common"
	"gitcode.com/opic-ontology/opic-techbase/pkg/errcode"
	"gitcode.com/opic-ontology/opic-techbase/pkg/llmcfg"
)

// errcodeParam 参数类错误的统一信封。
func errcodeParam(msg string) any {
	return errcode.New(errcode.ErrParam, msg, nil)
}

// maskAccount 出参打码（密钥不回显,只给 has_key）。
func maskAccount(a *llmcfg.Account) *llmcfg.Account {
	if a == nil {
		return nil
	}
	cp := *a
	cp.APIKey = ""
	cp.HasKey = true
	return &cp
}

// AICatalog 内置提供商目录。
func AICatalog(c *gin.Context) {
	common.OKJSON(c, gin.H{"catalog": llmcfg.Catalog()}, "")
}

// aiAccountBody 账号入参（api_key 空=保留原值）。
type aiAccountBody struct {
	Name         string  `json:"name"`
	Provider     string  `json:"provider"`
	BaseURL      string  `json:"base_url"`
	APIKey       string  `json:"api_key"`
	Models       string  `json:"models"`
	DefaultModel string  `json:"default_model"`
	Temperature  float64 `json:"temperature"`
	Enabled      *bool   `json:"enabled"`
}

func (b *aiAccountBody) norm() {
	b.Name = strings.TrimSpace(b.Name)
	b.Provider = strings.TrimSpace(b.Provider)
	b.BaseURL = strings.TrimSpace(b.BaseURL)
	b.Models = strings.TrimSpace(b.Models)
	b.DefaultModel = strings.TrimSpace(b.DefaultModel)
}

// AIAccountList 账号列表（打码）。
func AIAccountList(c *gin.Context) {
	common.OKJSON(c, gin.H{"accounts": llmcfg.ListAccounts()}, "")
}

// AIAccountCreate 新建账号。
func AIAccountCreate(c *gin.Context) {
	var b aiAccountBody
	if err := c.ShouldBindJSON(&b); err != nil {
		common.FailJSON(c, err)
		return
	}
	b.norm()
	if b.Name == "" || b.Provider == "" {
		c.JSON(200, errcodeParam("账号名与提供商不能为空"))
		return
	}
	enabled := true
	if b.Enabled != nil {
		enabled = *b.Enabled
	}
	a := &llmcfg.Account{
		Name: b.Name, Provider: b.Provider, BaseURL: b.BaseURL, APIKey: b.APIKey,
		Models: b.Models, DefaultModel: b.DefaultModel, Temperature: b.Temperature, Enabled: enabled,
	}
	if err := llmcfg.CreateAccount(a); err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, gin.H{"account": maskAccount(a)}, "账号已创建")
}

// AIAccountUpdate 更新账号（api_key 空=保留原值;变更端点须重新录入密钥）。
func AIAccountUpdate(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(200, errcodeParam("id 无效"))
		return
	}
	var b aiAccountBody
	if err := c.ShouldBindJSON(&b); err != nil {
		common.FailJSON(c, err)
		return
	}
	b.norm()
	old, err := llmcfg.GetAccountFull(id)
	if err != nil || old == nil {
		c.JSON(200, errcodeParam("账号不存在"))
		return
	}
	a := &llmcfg.Account{
		ID: id, Name: b.Name, Provider: b.Provider, BaseURL: b.BaseURL, APIKey: b.APIKey,
		Models: b.Models, DefaultModel: b.DefaultModel, Temperature: b.Temperature,
	}
	if b.Enabled != nil {
		a.Enabled = *b.Enabled
	} else {
		a.Enabled = old.Enabled
	}
	// 端点(含提供商默认端点)变更须同时重新录入密钥:否则已存密钥会被静默转发到本次新指定的目的地
	if b.APIKey == "" && (&llmcfg.Account{Provider: b.Provider, BaseURL: a.BaseURL}).ResolveBaseURL() != old.ResolveBaseURL() {
		c.JSON(200, errcodeParam("变更 API 地址或提供商须同时重新填写密钥"))
		return
	}
	if err := llmcfg.UpdateAccount(a); err != nil {
		common.FailJSON(c, err)
		return
	}
	full, _ := llmcfg.GetAccountFull(id)
	common.OKJSON(c, gin.H{"account": maskAccount(full)}, "账号已保存")
}

// AIAccountDelete 删除账号。
func AIAccountDelete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(200, errcodeParam("id 无效"))
		return
	}
	if err := llmcfg.DeleteAccount(id); err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, gin.H{"deleted": true}, "账号已删除")
}

// AIAccountTest 连通性测试（绿点状态依据）。
func AIAccountTest(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(200, errcodeParam("id 无效"))
		return
	}
	a, err := llmcfg.GetAccountFull(id)
	if err != nil || a == nil {
		c.JSON(200, errcodeParam("账号不存在"))
		return
	}
	if err := llmcfg.TestAccount(c.Request.Context(), a); err != nil {
		common.OKJSON(c, gin.H{"ok": false, "error": err.Error()}, "")
		return
	}
	common.OKJSON(c, gin.H{"ok": true}, "连接正常")
}

// AIUpstreamModels 获取上游可用模型目录。
// 传 id(GET) → 用已存账号自身登记的 base_url 与密钥;表单态(POST) → 用请求体中的 base_url/api_key。
// 关键约束:已存密钥只能发往该账号自身记录的端点,绝不接受调用方另外指定的目的地;
// 表单态密钥只允许走 POST 请求体,不得进 URL(查询串会落访问日志/代理记录)。
func AIUpstreamModels(c *gin.Context) {
	var baseURL, apiKey string
	if idStr := c.Query("id"); idStr != "" {
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id <= 0 {
			common.OKJSON(c, gin.H{"models": nil, "error": "id 无效"}, "")
			return
		}
		a, err := llmcfg.GetAccountFull(id)
		if err != nil || a == nil {
			common.OKJSON(c, gin.H{"models": nil, "error": "账号不存在"}, "")
			return
		}
		// 目的地与凭据同源:忽略 query 中的 base_url/api_key
		baseURL, apiKey = a.ResolveBaseURL(), a.APIKey
	} else if c.Request.Method == http.MethodPost {
		var b struct {
			BaseURL string `json:"base_url"`
			APIKey  string `json:"api_key"`
		}
		if err := c.ShouldBindJSON(&b); err != nil {
			common.OKJSON(c, gin.H{"models": nil, "error": "请求体无效"}, "")
			return
		}
		baseURL, apiKey = strings.TrimSpace(b.BaseURL), b.APIKey
	} else {
		common.OKJSON(c, gin.H{"models": nil, "error": "缺少 id;表单态请用 POST 提交"}, "")
		return
	}
	if err := llmcfg.CheckOutboundURL(baseURL); err != nil { // https/http + 解析后排除环回/链路本地/私网
		common.OKJSON(c, gin.H{"models": nil, "error": err.Error()}, "")
		return
	}
	ids, err := llmcfg.ListUpstreamModels(c.Request.Context(), baseURL, apiKey)
	if err != nil {
		common.OKJSON(c, gin.H{"models": nil, "error": err.Error()}, "")
		return
	}
	common.OKJSON(c, gin.H{"models": ids}, "")
}
