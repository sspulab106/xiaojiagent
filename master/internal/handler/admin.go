package handler

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"example.com/codetest/master/internal/auth"
	"example.com/codetest/master/internal/model"
	"example.com/codetest/master/internal/response"
)

func isAdminRequest(c *gin.Context) bool {
	return auth.Role(c) == "admin"
}

// AdminStats returns platform-wide counts for the dashboard.
func (h *Handler) AdminStats(c *gin.Context) {
	ctx := c.Request.Context()
	var nodes, nodesOnline, instances, users int64
	h.db.WithContext(ctx).Model(&model.Node{}).Count(&nodes)
	h.db.WithContext(ctx).Model(&model.Node{}).Where("status = ?", "online").Count(&nodesOnline)
	h.db.WithContext(ctx).Model(&model.Instance{}).Count(&instances)
	h.db.WithContext(ctx).Model(&model.User{}).Count(&users)
	response.OK(c, gin.H{
		"nodes":        nodes,
		"nodes_online": nodesOnline,
		"instances":    instances,
		"users":        users,
	})
}

func (h *Handler) ListNodes(c *gin.Context) {
	var nodes []model.Node
	if err := h.db.WithContext(c.Request.Context()).Order("id ASC").Find(&nodes).Error; err != nil {
		response.Internal(c, "查询失败")
		return
	}
	response.OK(c, nodes)
}

type addNodeReq struct {
	Name      string `json:"name" binding:"required,min=1,max=64"`
	AgentAddr string `json:"agent_addr"` // 可留空：安装脚本装好后会自动回传
	Token     string `json:"token" binding:"required"`
	Region    string `json:"region"` // 可留空：按 IP 自动识别
}

func (h *Handler) AddNode(c *gin.Context) {
	var req addNodeReq
	if !h.bind(c, &req) {
		return
	}
	node := model.Node{
		UserID:    auth.UID(c),
		Name:      req.Name,
		AgentAddr: normalizeAgentAddr(req.AgentAddr),
		Token:     req.Token,
		Region:    req.Region,
		Status:    "offline",
	}
	if err := h.db.WithContext(c.Request.Context()).Create(&node).Error; err != nil {
		response.BadRequest(c, "节点名称已存在")
		return
	}
	response.Created(c, node)
}

func (h *Handler) DeleteNode(c *gin.Context) {
	id, ok := h.parseID(c, "id")
	if !ok {
		return
	}
	var count int64
	h.db.WithContext(c.Request.Context()).Model(&model.Instance{}).Where("node_id = ?", id).Count(&count)
	if count > 0 {
		response.BadRequest(c, "节点上仍有实例，请先删除实例")
		return
	}
	if err := h.db.WithContext(c.Request.Context()).Delete(&model.Node{}, id).Error; err != nil {
		response.Internal(c, "删除失败")
		return
	}
	response.OK(c, nil)
}

func (h *Handler) ListUsers(c *gin.Context) {
	var users []model.User
	if err := h.db.WithContext(c.Request.Context()).Order("id ASC").Find(&users).Error; err != nil {
		response.Internal(c, "查询失败")
		return
	}
	response.OK(c, users)
}

type extendUserReq struct {
	Days int `json:"days" binding:"required,min=1,max=3650"`
}

// ExtendUser extends a user's subscription expiry by the given number of days.
func (h *Handler) ExtendUser(c *gin.Context) {
	id, ok := h.parseID(c, "id")
	if !ok {
		return
	}
	var req extendUserReq
	if !h.bind(c, &req) {
		return
	}
	var user model.User
	if err := h.db.WithContext(c.Request.Context()).First(&user, id).Error; err != nil {
		response.NotFound(c, "用户不存在")
		return
	}
	base := time.Now()
	if user.ExpiresAt != nil && user.ExpiresAt.After(base) {
		base = *user.ExpiresAt
	}
	expires := base.AddDate(0, 0, req.Days)
	if err := h.db.WithContext(c.Request.Context()).Model(&user).
		Update("expires_at", expires).Error; err != nil {
		response.Internal(c, "更新失败")
		return
	}
	user.ExpiresAt = &expires
	response.OK(c, user)
}

// AddPlatformPackage creates an admin-owned platform package (node_id = 0).
// Platform packages appear on the marketplace alongside host-owner packages.
func (h *Handler) AddPackage(c *gin.Context) {
	var req struct {
		Name            string   `json:"name" binding:"required,min=1,max=64"`
		Images          []string `json:"images"`
		CpuCores        int      `json:"cpu_cores" binding:"min=0"`
		MemoryMB        int64    `json:"memory_mb" binding:"min=0"`
		DiskMB          int64    `json:"disk_mb" binding:"min=0"`
		TrafficGB       int64    `json:"traffic_gb" binding:"min=0"`
		PortSlots       int      `json:"port_slots" binding:"min=0"`
		IPv6            bool     `json:"ipv6"`
		PriceCents      int      `json:"price_cents"`
		Listed          bool     `json:"listed"`
		EarlyFullRefund bool     `json:"early_full_refund"` // 允许早期全额退款
	}
	if !h.bind(c, &req) {
		return
	}
	pkg := model.Package{
		NodeID:          0,
		UserID:          0,
		Name:            req.Name,
		Images:          strings.Join(req.Images, ","),
		CpuCores:        req.CpuCores,
		MemoryMB:        req.MemoryMB,
		DiskMB:          req.DiskMB,
		TrafficGB:       req.TrafficGB,
		PortSlots:       req.PortSlots,
		IPv6:            req.IPv6,
		PriceCents:      req.PriceCents,
		Listed:          req.Listed,
		EarlyFullRefund: req.EarlyFullRefund,
		Enabled:         true,
	}
	if err := h.db.WithContext(c.Request.Context()).Create(&pkg).Error; err != nil {
		response.Internal(c, "创建失败")
		return
	}
	response.Created(c, pkg)
}
