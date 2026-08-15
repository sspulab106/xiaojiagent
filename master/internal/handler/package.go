package handler

import (
	"context"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"example.com/codetest/master/internal/auth"
	"example.com/codetest/master/internal/model"
	"example.com/codetest/master/internal/response"
)

// ---------------------------------------------------------------------------
// Ownership helpers
// ---------------------------------------------------------------------------

func (h *Handler) canManageNode(c *gin.Context, node *model.Node) bool {
	if auth.Role(c) == "admin" {
		return true
	}
	return node.UserID > 0 && node.UserID == auth.UID(c)
}

func (h *Handler) canManagePkg(c *gin.Context, pkg *model.Package) bool {
	if auth.Role(c) == "admin" {
		return true
	}
	return pkg.UserID > 0 && pkg.UserID == auth.UID(c)
}

// ---------------------------------------------------------------------------
// Packages
// ---------------------------------------------------------------------------

// ListPackages returns packages to buy. Buyers only see listed packages whose
// node (if any) is online; admins see everything.
func (h *Handler) ListPackages(c *gin.Context) {
	ctx := c.Request.Context()
	q := h.db.WithContext(ctx).Model(&model.Package{}).Order("id ASC")
	if auth.Role(c) != "admin" {
		q = q.Where("listed = ? AND enabled = ?", true, true)
	}
	var pkgs []model.Package
	if err := q.Find(&pkgs).Error; err != nil {
		response.Internal(c, "查询失败")
		return
	}
	if auth.Role(c) != "admin" {
		online := h.onlineNodes(ctx)
		filtered := pkgs[:0]
		for _, p := range pkgs {
			if p.NodeID > 0 && !online[p.NodeID] {
				continue
			}
			filtered = append(filtered, p)
		}
		pkgs = filtered
	}
	pkgs = h.decoratePackages(ctx, pkgs)
	response.OK(c, pkgs)
}

// ListNodePackages returns the packages of one node (owner or admin),
// including unlisted ones.
func (h *Handler) ListNodePackages(c *gin.Context) {
	node, ok := h.loadNodeForManage(c)
	if !ok {
		return
	}
	var pkgs []model.Package
	if err := h.db.WithContext(c.Request.Context()).
		Where("node_id = ?", node.ID).Order("id DESC").Find(&pkgs).Error; err != nil {
		response.Internal(c, "查询失败")
		return
	}
	pkgs = h.decoratePackages(c.Request.Context(), pkgs)
	response.OK(c, pkgs)
}

type createPackageReq struct {
	Name            string   `json:"name" binding:"required,min=1,max=64"`
	Images          []string `json:"images"`
	CpuCores        int      `json:"cpu_cores"`
	MemoryMB        int64    `json:"memory_mb"`
	DiskMB          int64    `json:"disk_mb"`
	TrafficGB       int64    `json:"traffic_gb"`
	PortSlots       int      `json:"port_slots"`
	IPv6            bool     `json:"ipv6"`
	PriceCents      int      `json:"price_cents"`
	Listed          bool     `json:"listed"`
	EarlyFullRefund bool     `json:"early_full_refund"` // 允许早期全额退款
}

// CreateNodePackage creates a package owned by the node's owner (owner/admin).
func (h *Handler) CreateNodePackage(c *gin.Context) {
	node, ok := h.loadNodeForManage(c)
	if !ok {
		return
	}
	var req createPackageReq
	if !h.bind(c, &req) {
		return
	}
	pkg := model.Package{
		NodeID:          node.ID,
		UserID:          node.UserID,
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
	decorated := h.decoratePackages(c.Request.Context(), []model.Package{pkg})
	if len(decorated) > 0 {
		pkg = decorated[0]
	}
	response.Created(c, pkg)
}

type updatePackageReq struct {
	Name            *string  `json:"name"`
	Images          []string `json:"images"`
	CpuCores        *int     `json:"cpu_cores"`
	MemoryMB        *int64   `json:"memory_mb"`
	DiskMB          *int64   `json:"disk_mb"`
	TrafficGB       *int64   `json:"traffic_gb"`
	PortSlots       *int     `json:"port_slots"`
	IPv6            *bool    `json:"ipv6"`
	PriceCents      *int     `json:"price_cents"`
	Listed          *bool    `json:"listed"`
	Enabled         *bool    `json:"enabled"`
	EarlyFullRefund *bool    `json:"early_full_refund"` // 允许早期全额退款
}

// UpdatePackage updates a package's specs or toggles its listed/enabled
// status (owner/admin).
func (h *Handler) UpdatePackage(c *gin.Context) {
	id, ok := h.parseID(c, "id")
	if !ok {
		return
	}
	var pkg model.Package
	if err := h.db.WithContext(c.Request.Context()).First(&pkg, id).Error; err != nil {
		response.NotFound(c, "套餐不存在")
		return
	}
	if !h.canManagePkg(c, &pkg) {
		response.Forbidden(c, "无权操作该套餐")
		return
	}
	var req updatePackageReq
	if !h.bind(c, &req) {
		return
	}
	updates := map[string]any{}
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Images != nil {
		updates["images"] = strings.Join(req.Images, ",")
	}
	if req.CpuCores != nil {
		updates["cpu_cores"] = *req.CpuCores
	}
	if req.MemoryMB != nil {
		updates["memory_mb"] = *req.MemoryMB
	}
	if req.DiskMB != nil {
		updates["disk_mb"] = *req.DiskMB
	}
	if req.TrafficGB != nil {
		updates["traffic_gb"] = *req.TrafficGB
	}
	if req.PortSlots != nil {
		updates["port_slots"] = *req.PortSlots
	}
	if req.IPv6 != nil {
		updates["ipv6"] = *req.IPv6
	}
	if req.PriceCents != nil {
		updates["price_cents"] = *req.PriceCents
	}
	if req.Listed != nil {
		updates["listed"] = *req.Listed
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}
	if req.EarlyFullRefund != nil {
		updates["early_full_refund"] = *req.EarlyFullRefund
	}
	if len(updates) == 0 {
		response.OK(c, pkg)
		return
	}
	if err := h.db.WithContext(c.Request.Context()).Model(&pkg).Updates(updates).Error; err != nil {
		response.Internal(c, "更新失败")
		return
	}
	response.OK(c, pkg)
}

// DeletePackage removes a package (owner/admin). Packages still in use by an
// instance cannot be deleted.
func (h *Handler) DeletePackage(c *gin.Context) {
	id, ok := h.parseID(c, "id")
	if !ok {
		return
	}
	var pkg model.Package
	if err := h.db.WithContext(c.Request.Context()).First(&pkg, id).Error; err != nil {
		response.NotFound(c, "套餐不存在")
		return
	}
	if !h.canManagePkg(c, &pkg) {
		response.Forbidden(c, "无权操作该套餐")
		return
	}
	var cnt int64
	h.db.WithContext(c.Request.Context()).Model(&model.Instance{}).Where("package_id = ?", pkg.ID).Count(&cnt)
	if cnt > 0 {
		response.BadRequest(c, "该套餐下仍有实例，请先删除实例")
		return
	}
	if err := h.db.WithContext(c.Request.Context()).Delete(&model.Package{}, pkg.ID).Error; err != nil {
		response.Internal(c, "删除失败")
		return
	}
	response.OK(c, nil)
}

// ---------------------------------------------------------------------------
// Coupons (purchase discount codes)
// ---------------------------------------------------------------------------

// ListNodeCoupons returns the coupons of one node (owner/admin).
func (h *Handler) ListNodeCoupons(c *gin.Context) {
	node, ok := h.loadNodeForManage(c)
	if !ok {
		return
	}
	var coupons []model.Coupon
	if err := h.db.WithContext(c.Request.Context()).
		Where("node_id = ?", node.ID).Order("id DESC").Find(&coupons).Error; err != nil {
		response.Internal(c, "查询失败")
		return
	}
	response.OK(c, coupons)
}

type createCouponReq struct {
	Code       string     `json:"code" binding:"required,min=2,max=32"`
	PercentOff int        `json:"percent_off" binding:"min=1,max=100"`
	MaxUses    int        `json:"max_uses"`
	Enabled    bool       `json:"enabled"`
	ExpiresAt  *time.Time `json:"expires_at"` // nil = 永不过期
}

// CreateNodeCoupon creates a discount coupon scoped to a node (owner/admin).
func (h *Handler) CreateNodeCoupon(c *gin.Context) {
	node, ok := h.loadNodeForManage(c)
	if !ok {
		return
	}
	var req createCouponReq
	if !h.bind(c, &req) {
		return
	}
	coupon := model.Coupon{
		NodeID:     node.ID,
		UserID:     node.UserID,
		Code:       strings.ToUpper(strings.TrimSpace(req.Code)),
		PercentOff: req.PercentOff,
		MaxUses:    req.MaxUses,
		Enabled:    req.Enabled,
		ExpiresAt:  req.ExpiresAt,
	}
	if err := h.db.WithContext(c.Request.Context()).Create(&coupon).Error; err != nil {
		response.BadRequest(c, "优惠码已存在")
		return
	}
	response.Created(c, coupon)
}

// DeleteNodeCoupon removes a coupon (owner/admin).
func (h *Handler) DeleteNodeCoupon(c *gin.Context) {
	id, ok := h.parseID(c, "id")
	if !ok {
		return
	}
	var coupon model.Coupon
	if err := h.db.WithContext(c.Request.Context()).First(&coupon, id).Error; err != nil {
		response.NotFound(c, "优惠码不存在")
		return
	}
	if auth.Role(c) != "admin" && (coupon.UserID == 0 || coupon.UserID != auth.UID(c)) {
		response.Forbidden(c, "无权操作该优惠码")
		return
	}
	if err := h.db.WithContext(c.Request.Context()).Delete(&model.Coupon{}, coupon.ID).Error; err != nil {
		response.Internal(c, "删除失败")
		return
	}
	response.OK(c, nil)
}

// ---------------------------------------------------------------------------
// Node live monitoring
// ---------------------------------------------------------------------------

type vmUsage struct {
	Name       string  `json:"name"`
	Status     string  `json:"status"`
	CPUPercent float64 `json:"cpu_percent"`
	MemUsedMB  int64   `json:"mem_used_mb"`
	MemLimitMB int64   `json:"mem_limit_mb"`
}

// NodeStats live-proxies the agent's health plus per-container usage so host
// owners can watch real-time resource consumption (owner/admin).
func (h *Handler) NodeStats(c *gin.Context) {
	node, ok := h.loadNodeForManage(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	client := h.svc.ClientForNode(node)
	health, err := client.Health(ctx)
	if err != nil {
		response.BadRequest(c, "节点离线或不可达")
		return
	}
	vms := []vmUsage{}
	if list, err := client.ListInstances(ctx); err == nil {
		for _, v := range list {
			st, err := client.Stats(ctx, v.Name)
			if err != nil {
				continue
			}
			vms = append(vms, vmUsage{
				Name:       v.Name,
				Status:     v.Status,
				CPUPercent: st.CPUPercent,
				MemUsedMB:  st.MemoryUsedMB,
				MemLimitMB: st.MemoryLimitMB,
			})
		}
	}
	response.OK(c, gin.H{
		"host": gin.H{
			"cpu_percent":   health.HostCPUPercent,
			"mem_used_mb":   health.HostMemUsedMB,
			"mem_total_mb":  health.HostMemTotalMB,
			"disk_used_mb":  health.HostDiskUsedMB,
			"disk_total_mb": health.HostDiskTotalMB,
			"load1":         health.Load1,
			"load5":         health.Load5,
			"load15":        health.Load15,
			"net_in_bps":    health.NetInBps,
			"net_out_bps":   health.NetOutBps,
			"uptime":        health.Uptime,
			"total_cores":   health.TotalCores,
			"running_vms":   health.RunningVMs,
			"total_vms":     health.TotalVMs,
			"ipv6_mode":     health.IPv6Mode,
			"ipv6_subnet":   health.IPv6Subnet,
			"ndp_iface":     health.NdpIface,
			"ndp_subnets":   health.NdpSubnets,
		},
		"vms": vms,
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (h *Handler) loadNodeForManage(c *gin.Context) (*model.Node, bool) {
	id, ok := h.parseID(c, "id")
	if !ok {
		return nil, false
	}
	var node model.Node
	if err := h.db.WithContext(c.Request.Context()).First(&node, id).Error; err != nil {
		response.NotFound(c, "节点不存在")
		return nil, false
	}
	if !h.canManageNode(c, &node) {
		response.Forbidden(c, "无权操作该节点")
		return nil, false
	}
	return &node, true
}

func (h *Handler) onlineNodes(ctx context.Context) map[uint]bool {
	online := map[uint]bool{}
	var nodes []model.Node
	if err := h.db.WithContext(ctx).Select("id", "status").Find(&nodes).Error; err != nil {
		return online
	}
	for _, n := range nodes {
		if n.Status == "online" {
			online[n.ID] = true
		}
	}
	return online
}

func (h *Handler) decoratePackages(ctx context.Context, pkgs []model.Package) []model.Package {
	ids := map[uint]bool{}
	for i := range pkgs {
		if pkgs[i].NodeID > 0 {
			ids[pkgs[i].NodeID] = true
		}
	}
	if len(ids) == 0 {
		return pkgs
	}
	var nodes []model.Node
	idsList := make([]uint, 0, len(ids))
	for id := range ids {
		idsList = append(idsList, id)
	}
	if err := h.db.WithContext(ctx).Where("id IN ?", idsList).Find(&nodes).Error; err != nil {
		return pkgs
	}
	byID := map[uint]model.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}
	for i := range pkgs {
		if n, ok := byID[pkgs[i].NodeID]; ok {
			pkgs[i].NodeName = n.Name
			pkgs[i].NodeRegion = n.Region
		}
	}
	return pkgs
}
