package handler

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"example.com/codetest/master/internal/auth"
	"example.com/codetest/master/internal/model"
	"example.com/codetest/master/internal/response"
)

type createTicketReq struct {
	InstanceID uint   `json:"instance_id" binding:"required"`
	Title      string `json:"title" binding:"required,min=2,max=128"`
	Content    string `json:"content" binding:"required,min=2,max=4000"`
}

// CreateTicket opens a support ticket (工单) against one of the caller's
// instances. The ticket is routed to the instance's host node — the node
// owner's support page (and platform admins) will see it.
func (h *Handler) CreateTicket(c *gin.Context) {
	var req createTicketReq
	if !h.bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	userID := auth.UID(c)

	var inst model.Instance
	if err := h.db.WithContext(ctx).First(&inst, req.InstanceID).Error; err != nil {
		response.NotFound(c, "实例不存在")
		return
	}
	// 权限隔离：只能为自己拥有的机器提工单（管理员代提允许）。
	if inst.UserID != userID && auth.Role(c) != "admin" {
		response.Forbidden(c, "只能为自己名下的实例提工单")
		return
	}

	ticket := model.Ticket{
		UserID:     userID,
		NodeID:     inst.NodeID,
		InstanceID: inst.ID,
		Title:      req.Title,
		Content:    req.Content,
		Status:     "open",
		NodeRead:   h.isNodeOwner(ctx, userID, inst.NodeID), // 给自己的节点提的工单不算待处理
		UserRead:   true,
	}
	if err := h.db.WithContext(ctx).Create(&ticket).Error; err != nil {
		response.Internal(c, "创建工单失败")
		return
	}
	h.decorateTicket(ctx, &ticket)
	response.Created(c, ticket)
}

// canViewTicket reports whether the caller may see this ticket: the creator,
// the host node owner, or a platform admin.
func (h *Handler) canViewTicket(ctx context.Context, uid uint, role string, t *model.Ticket) bool {
	if role == "admin" || t.UserID == uid {
		return true
	}
	// 宿主机所有者：该工单路由到其节点，技术支持页面可见。
	var node model.Node
	if err := h.db.WithContext(ctx).First(&node, t.NodeID).Error; err == nil && node.UserID == uid {
		return true
	}
	return false
}

// ListTickets returns support tickets visible to the caller:
//   - 平台管理员：全部工单（可筛选 node）
//   - 宿主机所有者：路由到其节点的工单（技术支持页面）
//   - 普通用户：自己发起的工单
func (h *Handler) ListTickets(c *gin.Context) {
	ctx := c.Request.Context()
	uid := auth.UID(c)
	role := auth.Role(c)

	q := h.db.WithContext(ctx).Model(&model.Ticket{})
	if role == "admin" {
		if nodeID := c.Query("node_id"); nodeID != "" {
			q = q.Where("node_id = ?", nodeID)
		}
	} else {
		// 宿主机所有者看到路由到自己节点的工单；普通用户看到自己的。
		var owned []uint
		h.db.WithContext(ctx).Model(&model.Node{}).Where("user_id = ?", uid).Pluck("id", &owned)
		if len(owned) > 0 {
			q = q.Where("user_id = ? OR node_id IN ?", uid, owned)
		} else {
			q = q.Where("user_id = ?", uid)
		}
	}
	var tickets []model.Ticket
	if err := q.Order("updated_at DESC").Find(&tickets).Error; err != nil {
		response.Internal(c, "查询失败")
		return
	}
	for i := range tickets {
		h.decorateTicket(ctx, &tickets[i])
	}
	response.OK(c, tickets)
}

// TicketUnreadCount returns the unread badge counts for the caller:
//   - 宿主机/管理员：NodeUnread = 还没被处理（看过）的新工单数
//   - 用户：UserUnread = 宿主机回复后还没看的工单数
func (h *Handler) TicketUnreadCount(c *gin.Context) {
	ctx := c.Request.Context()
	uid := auth.UID(c)
	role := auth.Role(c)

	var nodeUnread, userUnread int64
	// 未处理的工单计数不包含自己发起的工单（自己提给自家节点的不算待处理）。
	if role == "admin" {
		h.db.WithContext(ctx).Model(&model.Ticket{}).
			Where("node_read = ? AND status = ? AND user_id != ?", false, "open", uid).Count(&nodeUnread)
	} else {
		var owned []uint
		h.db.WithContext(ctx).Model(&model.Node{}).Where("user_id = ?", uid).Pluck("id", &owned)
		if len(owned) > 0 {
			h.db.WithContext(ctx).Model(&model.Ticket{}).
				Where("node_id IN ? AND node_read = ? AND status = ? AND user_id != ?", owned, false, "open", uid).Count(&nodeUnread)
		}
	}
	h.db.WithContext(ctx).Model(&model.Ticket{}).
		Where("user_id = ? AND user_read = ? AND status = ?", uid, false, "open").Count(&userUnread)

	response.OK(c, gin.H{"node_unread": nodeUnread, "user_unread": userUnread})
}

// GetTicket returns one ticket with its full reply thread, and marks it read
// for the caller's side (node owner/admin → NodeRead; creator → UserRead).
func (h *Handler) GetTicket(c *gin.Context) {
	id, ok := h.parseID(c, "id")
	if !ok {
		return
	}
	ctx := c.Request.Context()
	uid := auth.UID(c)
	role := auth.Role(c)

	var ticket model.Ticket
	if err := h.db.WithContext(ctx).First(&ticket, id).Error; err != nil {
		response.NotFound(c, "工单不存在")
		return
	}
	if !h.canViewTicket(ctx, uid, role, &ticket) {
		response.Forbidden(c, "无权查看该工单")
		return
	}
	var replies []model.TicketReply
	h.db.WithContext(ctx).Where("ticket_id = ?", ticket.ID).Order("id ASC").Find(&replies)
	ticket.Replies = replies
	h.decorateTicket(ctx, &ticket)

	// 读取即标记已读：宿主机/管理员看 → NodeRead；发起人看 → UserRead。
	updates := map[string]any{}
	if role == "admin" || h.isNodeOwner(ctx, uid, ticket.NodeID) {
		updates["node_read"] = true
	} else if ticket.UserID == uid {
		updates["user_read"] = true
	}
	if len(updates) > 0 {
		h.db.WithContext(ctx).Model(&ticket).Updates(updates)
	}
	response.OK(c, ticket)
}

type replyTicketReq struct {
	Content string `json:"content" binding:"required,min=1,max=4000"`
}

// ReplyTicket appends a message to a ticket. Anyone who can view the ticket
// may reply (creator, node owner, admin). Replying toggles the other side's
// unread flag so the support page shows an unread bubble.
func (h *Handler) ReplyTicket(c *gin.Context) {
	id, ok := h.parseID(c, "id")
	if !ok {
		return
	}
	ctx := c.Request.Context()
	uid := auth.UID(c)
	role := auth.Role(c)

	var ticket model.Ticket
	if err := h.db.WithContext(ctx).First(&ticket, id).Error; err != nil {
		response.NotFound(c, "工单不存在")
		return
	}
	if !h.canViewTicket(ctx, uid, role, &ticket) {
		response.Forbidden(c, "无权回复该工单")
		return
	}
	if ticket.Status != "open" {
		response.BadRequest(c, "工单已解决，无法继续回复（可重新打开）")
		return
	}
	var req replyTicketReq
	if !h.bind(c, &req) {
		return
	}
	reply := model.TicketReply{
		TicketID: ticket.ID,
		UserID:   uid,
		Content:  req.Content,
	}
	// 事务外判定回复方角色（避免在事务内用池连接读库）。
	isNodeSide := role == "admin" || h.isNodeOwner(ctx, uid, ticket.NodeID)
	now := time.Now()
	err := h.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&reply).Error; err != nil {
			return err
		}
		// 回复后对方未读：宿主机回复 → 用户未读；用户回复 → 宿主机未读。
		if isNodeSide {
			return tx.Model(&model.Ticket{}).Where("id = ?", ticket.ID).Updates(map[string]any{
				"user_read": false, "updated_at": now,
			}).Error
		}
		return tx.Model(&model.Ticket{}).Where("id = ?", ticket.ID).Updates(map[string]any{
			"node_read": false, "updated_at": now,
		}).Error
	})
	if err != nil {
		response.Internal(c, "回复失败")
		return
	}
	response.Created(c, reply)
}

// ResolveTicket marks an open ticket as resolved (node owner/admin; also the
// creator can close their own ticket). Resolved tickets are hidden from the
// node owner's unread bubble.
func (h *Handler) ResolveTicket(c *gin.Context) {
	id, ok := h.parseID(c, "id")
	if !ok {
		return
	}
	ctx := c.Request.Context()
	uid := auth.UID(c)
	role := auth.Role(c)

	var ticket model.Ticket
	if err := h.db.WithContext(ctx).First(&ticket, id).Error; err != nil {
		response.NotFound(c, "工单不存在")
		return
	}
	if !h.canViewTicket(ctx, uid, role, &ticket) {
		response.Forbidden(c, "无权操作该工单")
		return
	}
	if ticket.Status != "open" {
		response.BadRequest(c, "该工单已解决")
		return
	}
	now := time.Now()
	if err := h.db.WithContext(ctx).Model(&ticket).Updates(map[string]any{
		"status": "resolved", "resolved_at": now, "updated_at": now,
	}).Error; err != nil {
		response.Internal(c, "操作失败")
		return
	}
	ticket.Status = "resolved"
	ticket.ResolvedAt = &now
	response.OK(c, ticket)
}

// ReopenTicket reopens a resolved ticket (creator or node side).
func (h *Handler) ReopenTicket(c *gin.Context) {
	id, ok := h.parseID(c, "id")
	if !ok {
		return
	}
	ctx := c.Request.Context()
	uid := auth.UID(c)
	role := auth.Role(c)

	var ticket model.Ticket
	if err := h.db.WithContext(ctx).First(&ticket, id).Error; err != nil {
		response.NotFound(c, "工单不存在")
		return
	}
	if !h.canViewTicket(ctx, uid, role, &ticket) {
		response.Forbidden(c, "无权操作该工单")
		return
	}
	if ticket.Status != "resolved" {
		response.BadRequest(c, "该工单未处于已解决状态")
		return
	}
	now := time.Now()
	if err := h.db.WithContext(ctx).Model(&ticket).Updates(map[string]any{
		"status": "open", "resolved_at": nil, "updated_at": now,
	}).Error; err != nil {
		response.Internal(c, "操作失败")
		return
	}
	ticket.Status = "open"
	ticket.ResolvedAt = nil
	response.OK(c, ticket)
}

// isNodeOwner reports whether uid owns the node hosting this ticket's instance.
func (h *Handler) isNodeOwner(ctx context.Context, uid uint, nodeID uint) bool {
	var node model.Node
	if err := h.db.WithContext(ctx).First(&node, nodeID).Error; err != nil {
		return false
	}
	return node.UserID == uid
}

// decorateTicket fills the computed fields (names + reply count).
func (h *Handler) decorateTicket(ctx context.Context, t *model.Ticket) {
	var u model.User
	if err := h.db.WithContext(ctx).Select("username").First(&u, t.UserID).Error; err == nil {
		t.UserName = u.Username
	}
	var n model.Node
	if err := h.db.WithContext(ctx).Select("name").First(&n, t.NodeID).Error; err == nil {
		t.NodeName = n.Name
	}
	var inst model.Instance
	if err := h.db.WithContext(ctx).Select("name", "display_name").First(&inst, t.InstanceID).Error; err == nil {
		t.InstanceName = inst.DisplayName
		if t.InstanceName == "" {
			t.InstanceName = inst.Name
		}
	}
	for i := range t.Replies {
		if err := h.db.WithContext(ctx).Select("username").First(&u, t.Replies[i].UserID).Error; err == nil {
			t.Replies[i].UserName = u.Username
		}
	}
	t.ReplyCount = len(t.Replies)
}
