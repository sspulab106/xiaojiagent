package handler

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"example.com/codetest/master/internal/auth"
	"example.com/codetest/master/internal/model"
	"example.com/codetest/master/internal/response"
)

// ListAnnouncements returns platform announcements (control panel).
func (h *Handler) ListAnnouncements(c *gin.Context) {
	var anns []model.Announcement
	if err := h.db.WithContext(c.Request.Context()).Order("id DESC").Find(&anns).Error; err != nil {
		response.Internal(c, "查询失败")
		return
	}
	response.OK(c, anns)
}

type rechargeReq struct {
	AmountCents int64 `json:"amount_cents" binding:"required,min=1,max=100000000"`
}

// Recharge adds balance to the current user's wallet (demo: instant, no
// payment gateway). Also credits hosting balance proportionally.
func (h *Handler) Recharge(c *gin.Context) {
	var req rechargeReq
	if !h.bind(c, &req) {
		return
	}
	user, ok := h.currentUser(c)
	if !ok {
		return
	}

	credited := req.AmountCents
	err := h.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		hosting := credited / 10 // demo split: 10% goes to hosting balance
		user.BalanceCents += credited
		user.HostingTotalCents += hosting
		user.HostingAvailableCents += hosting
		if err := tx.Model(user).Updates(map[string]any{
			"balance_cents":           user.BalanceCents,
			"hosting_total_cents":     user.HostingTotalCents,
			"hosting_available_cents": user.HostingAvailableCents,
		}).Error; err != nil {
			return err
		}
		return tx.Create(&model.Transaction{
			UserID:      user.ID,
			Type:        "recharge",
			AmountCents: credited,
			Status:      "success",
			Description: "余额充值",
		}).Error
	})
	if err != nil {
		response.Internal(c, "充值失败")
		return
	}
	response.OK(c, user)
}

type changeInstancePasswordReq struct {
	Password string `json:"password" binding:"required,min=6,max=64"`
}

// ChangeInstancePassword updates the container's root password via the agent
// and stores it on the instance record.
func (h *Handler) ChangeInstancePassword(c *gin.Context) {
	inst, ok := h.loadInstance(c, true)
	if !ok {
		return
	}
	var req changeInstancePasswordReq
	if !h.bind(c, &req) {
		return
	}
	node, err := h.svc.NodeByID(c.Request.Context(), inst.NodeID)
	if err != nil {
		response.NotFound(c, "节点不存在")
		return
	}
	if err := h.svc.ClientForNode(node).ChangePassword(c.Request.Context(), inst.Name, req.Password); err != nil {
		response.BadRequest(c, "修改密码失败: "+err.Error())
		return
	}
	if err := h.db.WithContext(c.Request.Context()).Model(inst).Update("ssh_password", req.Password).Error; err != nil {
		response.Internal(c, "保存失败")
		return
	}
	inst.SSHPassword = req.Password
	h.decorate(c.Request.Context(), inst)
	response.OK(c, inst)
}

// currentUser loads the authenticated user from the DB.
func (h *Handler) currentUser(c *gin.Context) (*model.User, bool) {
	user, err := h.userByID(c, auth.UID(c))
	if err != nil {
		response.Unauthorized(c, "用户不存在")
		return nil, false
	}
	return user, true
}

type autoRenewReq struct {
	Enabled bool `json:"enabled"`
}

// SetAutoRenew toggles the auto-renew flag on an instance.
func (h *Handler) SetAutoRenew(c *gin.Context) {
	inst, ok := h.loadInstance(c, true)
	if !ok {
		return
	}
	var req autoRenewReq
	if !h.bind(c, &req) {
		return
	}
	inst.AutoRenew = req.Enabled
	if err := h.db.WithContext(c.Request.Context()).Model(inst).Update("auto_renew", req.Enabled).Error; err != nil {
		response.Internal(c, "更新失败")
		return
	}
	h.decorate(c.Request.Context(), inst)
	response.OK(c, inst)
}

// MyNodes returns the current user's nodes (admin sees all), each with its
// instance count. Registered at user-facing GET /nodes; the admin-only
// /admin/nodes endpoint uses ListNodes in admin.go.
func (h *Handler) MyNodes(c *gin.Context) {
	ctx := c.Request.Context()
	q := h.db.WithContext(ctx).Model(&model.Node{})
	if auth.Role(c) != "admin" {
		q = q.Where("user_id = ?", auth.UID(c))
	}
	var nodes []model.Node
	if err := q.Order("id ASC").Find(&nodes).Error; err != nil {
		response.Internal(c, "查询失败")
		return
	}
	for i := range nodes {
		var cnt int64
		h.db.WithContext(ctx).Model(&model.Instance{}).Where("node_id = ?", nodes[i].ID).Count(&cnt)
		nodes[i].InstanceCount = cnt
	}
	response.OK(c, nodes)
}

// GetNode returns one node for the owner (or admin). The owner sees the agent
// token so they can bind/rebind the machine.
func (h *Handler) GetNode(c *gin.Context) {
	id, ok := h.parseID(c, "id")
	if !ok {
		return
	}
	var node model.Node
	if err := h.db.WithContext(c.Request.Context()).First(&node, id).Error; err != nil {
		response.NotFound(c, "节点不存在")
		return
	}
	if auth.Role(c) != "admin" && node.UserID != auth.UID(c) {
		response.Forbidden(c, "无权访问该节点")
		return
	}
	var cnt int64
	h.db.WithContext(c.Request.Context()).Model(&model.Instance{}).Where("node_id = ?", node.ID).Count(&cnt)
	node.InstanceCount = cnt
	// The model hides Token (json:"-"); expose it to the owner explicitly.
	response.OK(c, gin.H{"node": nodeDetailResp{Node: node, Token: node.Token}, "stats": nil})
}

// nodeDetailResp embeds the node and re-exposes the agent token to the owner.
type nodeDetailResp struct {
	model.Node
	Token string `json:"token"`
}

// AddMachine is the user-facing "托管中心 → 添加机器" flow: creates a node
// with a fresh token and returns the install script (mirrors AddHost but is
// open to regular users).
func (h *Handler) AddMachine(c *gin.Context) {
	var req addHostReq
	if !h.bind(c, &req) {
		return
	}
	token := randHex(16)
	node := model.Node{
		UserID:    auth.UID(c),
		Name:      req.Name,
		AgentAddr: normalizeAgentAddr(req.AgentAddr),
		Token:     token,
		Region:    req.Region,
		Status:    "offline",
	}
	if err := h.db.WithContext(c.Request.Context()).Create(&node).Error; err != nil {
		response.BadRequest(c, "节点名称已存在")
		return
	}
	script := h.buildInstallScript(c, token, req.VirtType, req.WebPassword, req.WithToken, req.SocketPath, req.CLIBin)
	response.Created(c, gin.H{"node": node, "token": token, "script": script})
}
