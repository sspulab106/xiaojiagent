package handler

import (
	"context"
	"log/slog"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"example.com/codetest/master/internal/auth"
	"example.com/codetest/master/internal/config"
	"example.com/codetest/master/internal/model"
	"example.com/codetest/master/internal/response"
	"example.com/codetest/master/internal/service"
)

// Handler groups all HTTP handlers with their dependencies.
type Handler struct {
	db   *gorm.DB
	svc  *service.Service
	auth *auth.Middleware
	cfg  config.Config
	log  *slog.Logger
}

func New(db *gorm.DB, svc *service.Service, authM *auth.Middleware, cfg config.Config, logger *slog.Logger) *Handler {
	return &Handler{db: db, svc: svc, auth: authM, cfg: cfg, log: logger}
}

// BlockBanned is a middleware that rejects requests from banned users. It must
// run after the auth middleware so auth.UID(c) is populated.
func (h *Handler) BlockBanned() gin.HandlerFunc {
	return func(c *gin.Context) {
		var user model.User
		if err := h.db.WithContext(c.Request.Context()).Select("id", "banned").First(&user, auth.UID(c)).Error; err != nil {
			c.Abort()
			response.Unauthorized(c, "账号不存在")
			return
		}
		if user.Banned {
			c.Abort()
			response.Forbidden(c, "账号已被封禁")
			return
		}
		c.Next()
	}
}

// bind decodes a JSON body; on error it writes a 400 and returns false.
func (h *Handler) bind(c *gin.Context, out any) bool {
	if err := c.ShouldBindJSON(out); err != nil {
		response.BadRequest(c, "请求体无效: "+err.Error())
		return false
	}
	return true
}

// parseID converts a path id to uint, writing a 400 on failure.
func (h *Handler) parseID(c *gin.Context, field string) (uint, bool) {
	v, err := strconv.ParseUint(c.Param(field), 10, 64)
	if err != nil {
		response.BadRequest(c, "无效的 ID")
		return 0, false
	}
	return uint(v), true
}

// loadInstance loads an instance and, when requireOwner is set, verifies the
// caller owns it (admins always pass).
func (h *Handler) loadInstance(c *gin.Context, requireOwner bool) (*model.Instance, bool) {
	id, ok := h.parseID(c, "id")
	if !ok {
		return nil, false
	}
	inst, err := h.svc.InstanceByID(c.Request.Context(), id)
	if err != nil {
		response.NotFound(c, "实例不存在")
		return nil, false
	}
	if requireOwner && inst.UserID != auth.UID(c) && auth.Role(c) != "admin" {
		response.Forbidden(c, "无权访问该实例")
		return nil, false
	}
	return inst, true
}

// userByID loads a user by primary key.
func (h *Handler) userByID(c *gin.Context, id uint) (*model.User, error) {
	var user model.User
	if err := h.db.WithContext(c.Request.Context()).First(&user, id).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// decorate fills the computed JSON fields (node/package names, country, OS)
// on an instance.
func (h *Handler) decorate(ctx context.Context, inst *model.Instance) {
	var node model.Node
	if err := h.db.WithContext(ctx).First(&node, inst.NodeID).Error; err == nil {
		inst.NodeName = node.Name
		inst.NodeRegion = node.Region
		inst.NodeHostIP = node.HostIP
		inst.Country = node.Country
		if inst.Country == "" {
			inst.Country = node.Region
		}
		inst.OS = osLabel(inst.Image, node.VirtType)
	}
	var pkg model.Package
	if err := h.db.WithContext(ctx).First(&pkg, inst.PackageID).Error; err == nil {
		inst.PackageName = pkg.Name
		inst.EarlyFullRefund = pkg.EarlyFullRefund
	}
}

// osLabel renders an instance OS label, e.g. "Debian Podman".
func osLabel(image, virtType string) string {
	name := image
	if i := strings.IndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	if i := strings.IndexByte(name, ':'); i >= 0 {
		name = name[:i]
	}
	name = strings.Title(name)
	switch virtType {
	case "oci":
		return name + " Podman"
	default:
		return name + " LXD"
	}
}
