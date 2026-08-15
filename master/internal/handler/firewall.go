package handler

import (
	"context"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"example.com/codetest/master/internal/agentclient"
	"example.com/codetest/master/internal/auth"
	"example.com/codetest/master/internal/model"
	"example.com/codetest/master/internal/response"
)

// nodeForFirewall loads a node and verifies the caller is its owner or an
// admin (same rule as GetNode). Returns the node or aborts the request.
func (h *Handler) nodeForFirewall(c *gin.Context) *model.Node {
	id, ok := h.parseID(c, "id")
	if !ok {
		return nil
	}
	var node model.Node
	if err := h.db.WithContext(c.Request.Context()).First(&node, id).Error; err != nil {
		response.NotFound(c, "节点不存在")
		return nil
	}
	if auth.Role(c) != "admin" && node.UserID != auth.UID(c) {
		response.Forbidden(c, "无权访问该节点")
		return nil
	}
	return &node
}

// NodeFirewall returns the node's rfw status and rule list in one call.
func (h *Handler) NodeFirewall(c *gin.Context) {
	node := h.nodeForFirewall(c)
	if node == nil {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	client := h.svc.ClientForNode(node)

	status, err := client.FirewallStatus(ctx)
	if err != nil {
		response.BadRequest(c, "防火墙状态获取失败: "+err.Error())
		return
	}
	rules, err := client.FirewallRules(ctx)
	if err != nil {
		response.BadRequest(c, "防火墙规则获取失败: "+err.Error())
		return
	}
	response.OK(c, gin.H{"status": status, "rules": rules})
}

// NodeFirewallCreate adds a rule on the node's rfw daemon.
func (h *Handler) NodeFirewallCreate(c *gin.Context) {
	node := h.nodeForFirewall(c)
	if node == nil {
		return
	}
	var req agentclient.CreateFirewallRuleReq
	if !h.bind(c, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	rule, err := h.svc.ClientForNode(node).FirewallCreateRule(ctx, req)
	if err != nil {
		response.BadRequest(c, "创建规则失败: "+err.Error())
		return
	}
	response.Created(c, rule)
}

// NodeFirewallDelete removes a rule on the node by rfw numeric id.
func (h *Handler) NodeFirewallDelete(c *gin.Context) {
	node := h.nodeForFirewall(c)
	if node == nil {
		return
	}
	ruleID, err := strconv.ParseUint(c.Param("ruleId"), 10, 64)
	if err != nil {
		response.BadRequest(c, "规则 ID 无效")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	if err := h.svc.ClientForNode(node).FirewallDeleteRule(ctx, ruleID); err != nil {
		response.BadRequest(c, "删除规则失败: "+err.Error())
		return
	}
	response.OK(c, nil)
}
