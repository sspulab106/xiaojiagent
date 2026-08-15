package handler

import (
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"example.com/codetest/master/internal/model"
	"example.com/codetest/master/internal/response"
)

type authReq struct {
	Username string `json:"username" binding:"required,min=3,max=32"`
	Password string `json:"password" binding:"required,min=6,max=72"`
}

func (h *Handler) Login(c *gin.Context) {
	var req authReq
	if !h.bind(c, &req) {
		return
	}
	var user model.User
	if err := h.db.WithContext(c.Request.Context()).
		Where("username = ?", req.Username).First(&user).Error; err != nil {
		response.Unauthorized(c, "用户名或密码错误")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
		response.Unauthorized(c, "用户名或密码错误")
		return
	}
	if user.Banned {
		response.Forbidden(c, "账号已被封禁")
		return
	}
	h.issueToken(c, &user)
}

func (h *Handler) Register(c *gin.Context) {
	var req authReq
	if !h.bind(c, &req) {
		return
	}
	var count int64
	h.db.WithContext(c.Request.Context()).Model(&model.User{}).Count(&count)

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		response.Internal(c, "密码加密失败")
		return
	}
	user := model.User{
		Username:      req.Username,
		PasswordHash:  string(hash),
		Role:          "user",
		InstanceQuota: 2,
	}
	// The very first account becomes the admin.
	if count == 0 {
		user.Role = "admin"
	}
	if err := h.db.WithContext(c.Request.Context()).Create(&user).Error; err != nil {
		response.BadRequest(c, "用户名已存在")
		return
	}
	h.issueToken(c, &user)
}

func (h *Handler) issueToken(c *gin.Context, user *model.User) {
	token, err := h.auth.IssueToken(user.ID, user.Username, user.Role)
	if err != nil {
		response.Internal(c, "令牌签发失败")
		return
	}
	response.OK(c, gin.H{"token": token, "user": user})
}
