package handler

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/smtp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"example.com/codetest/master/internal/auth"
	"example.com/codetest/master/internal/response"
)

type changeLoginPasswordReq struct {
	OldPassword string `json:"old_password" binding:"required,min=6,max=72"`
	NewPassword string `json:"new_password" binding:"required,min=6,max=72"`
}

// ChangeLoginPassword lets the user update their platform login password
// after verifying the current one.
func (h *Handler) ChangeLoginPassword(c *gin.Context) {
	var req changeLoginPasswordReq
	if !h.bind(c, &req) {
		return
	}
	user, ok := h.currentUser(c)
	if !ok {
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.OldPassword)) != nil {
		response.BadRequest(c, "当前密码不正确")
		return
	}
	if req.OldPassword == req.NewPassword {
		response.BadRequest(c, "新密码不能与当前密码相同")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		response.Internal(c, "密码加密失败")
		return
	}
	if err := h.db.WithContext(c.Request.Context()).Model(user).Update("password_hash", string(hash)).Error; err != nil {
		response.Internal(c, "保存失败")
		return
	}
	response.OK(c, nil)
}

type bindEmailReq struct {
	Email string `json:"email" binding:"required,email,max=128"`
}

// BindEmail saves the user's email. When the platform has enabled 邮箱验证
// and configured an SMTP sender, a verification code is emailed and the email
// stays unverified until VerifyEmail confirms it; otherwise it is bound
// directly as verified (verification is a platform policy toggle).
func (h *Handler) BindEmail(c *gin.Context) {
	var req bindEmailReq
	if !h.bind(c, &req) {
		return
	}
	user, ok := h.currentUser(c)
	if !ok {
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	ctx := c.Request.Context()

	// 邮箱唯一性（已绑定该邮箱的其他账号不可重复绑定）。
	var cnt int64
	h.db.WithContext(ctx).Model(&user).Where("id != ? AND email = ?", user.ID, email).Count(&cnt)
	if cnt > 0 {
		response.BadRequest(c, "该邮箱已被其他账号绑定")
		return
	}

	verifyEnabled := h.svc.Setting(ctx, "email_verify_enabled") == "1"
	smtpConfigured := h.svc.Setting(ctx, "smtp_host") != ""
	if verifyEnabled && smtpConfigured {
		code := randCode(6)
		expiry := time.Now().Add(15 * time.Minute)
		if err := h.db.WithContext(ctx).Model(user).Updates(map[string]any{
			"email": email, "email_verified": false,
			"email_verify_code": code, "email_verify_expiry": expiry,
		}).Error; err != nil {
			response.Internal(c, "保存失败")
			return
		}
		// 发送验证邮件（失败不阻断绑定，用户可稍后重新请求验证码）。
		if err := h.sendVerifyEmail(ctx, email, code); err != nil {
			h.log.Warn("send verify email", "email", email, "err", err)
		}
		response.OK(c, gin.H{
			"email": email, "email_verified": false, "code_sent": true,
			"message": "验证码已发送到邮箱，请查收后完成验证",
		})
		return
	}
	// 未开启邮箱验证（或未配置 SMTP）：直接绑定并视为已验证。
	if err := h.db.WithContext(ctx).Model(user).Updates(map[string]any{
		"email": email, "email_verified": true,
		"email_verify_code": "", "email_verify_expiry": nil,
	}).Error; err != nil {
		response.Internal(c, "保存失败")
		return
	}
	response.OK(c, gin.H{"email": email, "email_verified": true, "code_sent": false})
}

type verifyEmailReq struct {
	Code string `json:"code" binding:"required,min=4,max=16"`
}

// VerifyEmail confirms a previously sent email verification code.
func (h *Handler) VerifyEmail(c *gin.Context) {
	var req verifyEmailReq
	if !h.bind(c, &req) {
		return
	}
	user, ok := h.currentUser(c)
	if !ok {
		return
	}
	if user.Email == "" || user.EmailVerified {
		response.BadRequest(c, "当前邮箱无需验证")
		return
	}
	if user.EmailVerifyCode == "" || user.EmailVerifyExpiry == nil || time.Now().After(*user.EmailVerifyExpiry) {
		response.BadRequest(c, "验证码已过期，请重新绑定邮箱获取新验证码")
		return
	}
	if strings.TrimSpace(req.Code) != user.EmailVerifyCode {
		response.BadRequest(c, "验证码不正确")
		return
	}
	if err := h.db.WithContext(c.Request.Context()).Model(user).Updates(map[string]any{
		"email_verified": true, "email_verify_code": "", "email_verify_expiry": nil,
	}).Error; err != nil {
		response.Internal(c, "保存失败")
		return
	}
	response.OK(c, gin.H{"email": user.Email, "email_verified": true})
}

// sendVerifyEmail delivers the verification code through the configured SMTP
// sender. SMTP settings are stored as platform settings (smtp_host,
// smtp_port, smtp_user, smtp_pass, smtp_from).
func (h *Handler) sendVerifyEmail(ctx context.Context, to, code string) error {
	host := h.svc.Setting(ctx, "smtp_host")
	port := h.svc.Setting(ctx, "smtp_port")
	if port == "" {
		port = "587"
	}
	from := h.svc.Setting(ctx, "smtp_from")
	user := h.svc.Setting(ctx, "smtp_user")
	pass := h.svc.Setting(ctx, "smtp_pass")
	if from == "" {
		from = user
	}
	if host == "" || from == "" {
		return fmt.Errorf("SMTP 未配置")
	}
	addr := host + ":" + port
	body := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: =?UTF-8?B?%s?=\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n你的验证码是：%s\n有效期 15 分钟。若非本人操作请忽略。\n",
		from, to, base64Header("独角鲸云 - 邮箱验证"), code)
	authSMTP := smtp.PlainAuth("", user, pass, host)
	return smtp.SendMail(addr, authSMTP, from, []string{to}, []byte(body))
}

// base64Header encodes a UTF-8 subject for RFC 2047.
func base64Header(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// ---- 平台设置（admin） ----

// platformSettingKeys lists the admin-editable platform settings.
var platformSettingKeys = []struct{ Key, Label string }{
	{"smtp_host", "SMTP 发件服务器"},
	{"smtp_port", "SMTP 端口"},
	{"smtp_user", "SMTP 用户名"},
	{"smtp_pass", "SMTP 密码"},
	{"smtp_from", "发件人地址"},
	{"email_verify_enabled", "开启邮箱验证"},
	{"cloudflare_captcha_enabled", "开启 Cloudflare 人机验证（预留）"},
}

// GetSettings returns the platform settings (admin only).
func (h *Handler) GetSettings(c *gin.Context) {
	if auth.Role(c) != "admin" {
		response.Forbidden(c, "仅管理员可查看平台设置")
		return
	}
	ctx := c.Request.Context()
	out := gin.H{}
	for _, k := range platformSettingKeys {
		out[k.Key] = h.svc.Setting(ctx, k.Key)
	}
	response.OK(c, out)
}

type updateSettingsReq map[string]string

// UpdateSettings saves platform settings (admin only). Unknown keys are
// ignored; smtp_pass is kept when sent as an empty string (avoid wiping).
func (h *Handler) UpdateSettings(c *gin.Context) {
	if auth.Role(c) != "admin" {
		response.Forbidden(c, "仅管理员可修改平台设置")
		return
	}
	var req updateSettingsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求体无效")
		return
	}
	ctx := c.Request.Context()
	known := map[string]bool{}
	for _, k := range platformSettingKeys {
		known[k.Key] = true
	}
	for key, val := range req {
		if !known[key] {
			continue
		}
		// 密码留空 = 保持不变。
		if key == "smtp_pass" && strings.TrimSpace(val) == "" {
			continue
		}
		if err := h.svc.SetSetting(ctx, key, strings.TrimSpace(val)); err != nil {
			response.Internal(c, "保存设置失败")
			return
		}
	}
	response.OK(c, nil)
}

func randCode(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = '0' + b[i]%10
	}
	return string(b)
}
