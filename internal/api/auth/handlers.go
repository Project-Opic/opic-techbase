// Package auth —— 认证控制器(登录/登出/当前用户/认证模式;契约与旧版一致)。
package auth

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"gitcode.com/opic-ontology/opic-techbase/internal/api/common"
	"gitcode.com/opic-ontology/opic-techbase/internal/config"
	"gitcode.com/opic-ontology/opic-techbase/internal/pkg/auth"
	"gitcode.com/opic-ontology/opic-techbase/internal/service"
	"gitcode.com/opic-ontology/opic-techbase/pkg/errcode"
	"gitcode.com/opic-ontology/opic-techbase/pkg/token"
)

// Login 本地账号登录(local 模式)。须先通过图形验证码(安控加强)。
// 登录按来源与账号双维限速退避,失败落审计(LOGIN_FAIL),成功登录清零账号维度失败计数。
func Login(c *gin.Context) {
	if ok, retry := loginSourceLimit.blocked(sourceKey(c)); ok {
		service.WriteAudit(nil, 0, "", "LOGIN_FAIL", "来源限速拦截")
		c.JSON(200, errcode.New(errcode.ErrLogin, waitMsg(retry), nil))
		return
	}
	body := common.BindJSON(c)
	username := common.Trim(body["username"])
	if ok, retry := loginAccountLimit.blocked(strings.ToLower(username)); ok {
		service.WriteAudit(nil, 0, username, "LOGIN_FAIL", "账号失败退避中")
		c.JSON(200, errcode.New(errcode.ErrLogin, waitMsg(retry), nil))
		return
	}
	if !VerifyCaptcha(c, common.Trim(body["captcha_id"]), common.Trim(body["captcha_code"])) {
		loginSourceLimit.take(sourceKey(c))
		if username != "" {
			loginAccountLimit.take(strings.ToLower(username))
		}
		service.WriteAudit(nil, 0, username, "LOGIN_FAIL", "验证码错误或已过期")
		c.JSON(200, errcode.New(errcode.ErrLogin, "验证码错误或已过期", nil))
		return
	}
	password := common.Trim(body["password"])
	if username == "" || password == "" {
		c.JSON(200, errcode.New(errcode.ErrLogin, "请输入用户名和密码", nil))
		return
	}
	user, errMsg := service.Login(username, password)
	if errMsg != "" {
		loginSourceLimit.take(sourceKey(c))
		loginAccountLimit.take(strings.ToLower(username))
		service.WriteAudit(nil, 0, username, "LOGIN_FAIL", errMsg)
		c.JSON(200, errcode.New(errcode.ErrLogin, errMsg, nil))
		return
	}
	tk, err := auth.IssueLocalToken(service.Int(user["id"]), service.Str(user["username"]), service.Int(user["session_epoch"]))
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	payload, err := service.BuildLoginPayload(nil, user)
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	payload["token"] = tk
	loginSourceLimit.reset(sourceKey(c))
	loginAccountLimit.reset(strings.ToLower(username))
	service.WriteAudit(nil, service.Int(user["id"]), service.Str(user["username"]), "LOGIN", "")
	common.OKJSON(c, payload, "登录成功")
}

// Logout 退出(吊销当前会话令牌;Redis 未启用时仅审计,行为与旧版一致)。
func Logout(c *gin.Context) {
	u := common.UserOf(c)
	if claims, err := auth.ParseLocalToken(common.BearerToken(c)); err == nil && claims.ID != "" && claims.ExpiresAt != nil {
		if ttl := time.Until(claims.ExpiresAt.Time); ttl > 0 {
			_ = token.Revoke(claims.ID, ttl)
		}
	}
	service.WriteAudit(nil, u.ID, u.Username, "LOGOUT", "")
	common.OKJSON(c, nil, "已退出登录")
}

// Info 当前用户信息(user/permissions/menus)。
func Info(c *gin.Context) {
	u := common.UserOf(c)
	user, err := service.GetUser(u.ID)
	if err != nil || user == nil || service.Int(user["status"]) != 1 {
		common.AuthFail(c, errcode.ErrLogin, "账号不存在或已禁用")
		return
	}
	payload, err := service.BuildLoginPayload(nil, user)
	if err != nil {
		common.FailJSON(c, err)
		return
	}
	common.OKJSON(c, payload, "")
}

// AuthMode 当前认证模式(前端登录页据此分流)。
func AuthMode(c *gin.Context) {
	mode := authMode()
	localAllowed := mode == "local" || allowLocalLogin()
	// 用户工作台端（OPIC-SSO-01：casdoor）
	ua := config.Config.UserAuth
	userMode := ua.Mode
	if userMode == "" {
		userMode = "local"
	}
	// 用户端本地登录跟随全局 allow_local_login（用户规则 2026-10：SSO 模式下仍可切本地账号）
	userLocalAllowed := localAllowed
	c.JSON(200, errcode.OK(gin.H{
		"mode": mode, "allow_local_login": localAllowed, // 兼容旧前端（管理控制台端）
		"admin": gin.H{"mode": mode, "allow_local_login": localAllowed, "sso": mode == "zitadel"},
		"user":  gin.H{"mode": userMode, "allow_local_login": userLocalAllowed, "sso": userMode == "casdoor"},
	}))
}
