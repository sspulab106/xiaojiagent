package handler

import (
	"github.com/gin-gonic/gin"

	"example.com/codetest/master/internal/auth"
	"example.com/codetest/master/internal/model"
	"example.com/codetest/master/internal/response"
)

type addPortReq struct {
	ContainerPort int    `json:"container_port" binding:"required,min=1,max=65535"`
	Protocol      string `json:"protocol" binding:"oneof=tcp udp"`
	HostPort      int    `json:"host_port" binding:"min=0,max=65535"`
}

// ListPorts returns the NAT port mappings of an instance.
func (h *Handler) ListPorts(c *gin.Context) {
	inst, ok := h.loadInstance(c, true)
	if !ok {
		return
	}
	var mappings []model.PortMapping
	if err := h.db.WithContext(c.Request.Context()).
		Where("instance_id = ?", inst.ID).Order("id ASC").Find(&mappings).Error; err != nil {
		response.Internal(c, "查询失败")
		return
	}
	response.OK(c, mappings)
}

// AddPort creates a NAT port mapping on the agent and records it locally.
func (h *Handler) AddPort(c *gin.Context) {
	inst, ok := h.loadInstance(c, true)
	if !ok {
		return
	}
	var req addPortReq
	if !h.bind(c, &req) {
		return
	}
	if req.Protocol == "" {
		req.Protocol = "tcp"
	}
	if req.HostPort == 0 {
		req.HostPort = 0 // explicit: let the agent auto-allocate
	}

	var used int64
	h.db.WithContext(c.Request.Context()).Model(&model.PortMapping{}).
		Where("instance_id = ?", inst.ID).Count(&used)
	if int(used) >= inst.PortSlots {
		response.BadRequest(c, "端口映射配额已用完")
		return
	}

	node, err := h.svc.NodeByID(c.Request.Context(), inst.NodeID)
	if err != nil {
		response.NotFound(c, "节点不存在")
		return
	}
	client := h.svc.ClientForNode(node)
	port, err := client.AddPort(c.Request.Context(), inst.Name, req.ContainerPort, req.Protocol, req.HostPort)
	if err != nil {
		response.BadRequest(c, "端口映射失败: "+err.Error())
		return
	}

	mapping := &model.PortMapping{
		InstanceID:    inst.ID,
		AgentRuleID:   port.ID,
		HostPort:      port.HostPort,
		ContainerIP:   port.ContainerIP,
		ContainerPort: port.ContainerPort,
		Protocol:      port.Protocol,
	}
	if err := h.db.WithContext(c.Request.Context()).Create(mapping).Error; err != nil {
		response.Internal(c, "保存失败")
		return
	}
	response.Created(c, mapping)
}

// DeletePort removes a NAT mapping from the agent and the database.
func (h *Handler) DeletePort(c *gin.Context) {
	id, ok := h.parseID(c, "id")
	if !ok {
		return
	}
	var mapping model.PortMapping
	if err := h.db.WithContext(c.Request.Context()).First(&mapping, id).Error; err != nil {
		response.NotFound(c, "映射不存在")
		return
	}

	inst, err := h.svc.InstanceByID(c.Request.Context(), mapping.InstanceID)
	if err != nil {
		response.NotFound(c, "实例不存在")
		return
	}
	// Ownership check mirrors loadInstance.
	if inst.UserID != auth.UID(c) && auth.Role(c) != "admin" {
		response.Forbidden(c, "无权操作")
		return
	}

	if mapping.AgentRuleID != "" {
		node, err := h.svc.NodeByID(c.Request.Context(), inst.NodeID)
		if err != nil {
			response.NotFound(c, "节点不存在")
			return
		}
		if err := h.svc.ClientForNode(node).RemovePort(c.Request.Context(), mapping.AgentRuleID); err != nil {
			response.BadRequest(c, "节点删除规则失败: "+err.Error())
			return
		}
	}

	if err := h.db.WithContext(c.Request.Context()).Delete(&mapping).Error; err != nil {
		response.Internal(c, "删除失败")
		return
	}
	response.OK(c, nil)
}

// Terminal relays a WebSocket terminal session to the agent. The browser
// authenticates with ?token=<jwt> in the query string (WebSockets can't set
// custom headers in the browser), so this handler validates it manually.
func (h *Handler) Terminal(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		response.Unauthorized(c, "缺少令牌")
		return
	}
	claims, err := h.auth.ValidateToken(token)
	if err != nil {
		response.Unauthorized(c, "令牌无效")
		return
	}
	h.auth.SetUser(c, claims)

	// 封禁用户不允许使用终端（该路由绕过 BlockBanned 中间件）。
	user, err := h.userByID(c, claims.UserID)
	if err != nil {
		response.Unauthorized(c, "账号不存在")
		return
	}
	if user.Banned {
		response.Forbidden(c, "账号已被封禁")
		return
	}

	inst, ok := h.loadInstance(c, true)
	if !ok {
		return
	}
	node, err := h.svc.NodeByID(c.Request.Context(), inst.NodeID)
	if err != nil {
		response.NotFound(c, "节点不存在")
		return
	}
	h.proxyTerminal(c, node, inst.Name)
}
