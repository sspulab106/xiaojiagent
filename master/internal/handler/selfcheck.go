package handler

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"

	"example.com/codetest/master/internal/auth"
	"example.com/codetest/master/internal/model"
	"example.com/codetest/master/internal/response"
)

// selfCheckSnapshot is the JSON persisted into Node.LastSelfcheck so the
// admin node list can show the most recent result without polling the agent.
// Output is truncated to keep the column lean.
type selfCheckSnapshot struct {
	Status     string    `json:"status"` // running | done | failed
	RunID      string    `json:"run_id"`
	ExitCode   int       `json:"exit_code"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	Output     string    `json:"output,omitempty"`
}

const selfCheckOutputCap = 6000

// saveSelfCheck persists a self-check snapshot on the node (best-effort).
func (h *Handler) saveSelfCheck(nodeID uint, snap selfCheckSnapshot) {
	if len(snap.Output) > selfCheckOutputCap {
		snap.Output = snap.Output[len(snap.Output)-selfCheckOutputCap:]
	}
	b, err := json.Marshal(snap)
	if err != nil {
		return
	}
	if err := h.db.Model(&model.Node{}).Where("id = ?", nodeID).Update("last_selfcheck", string(b)).Error; err != nil {
		h.log.Warn("save node self-check snapshot", "node_id", nodeID, "err", err)
	}
}

// adminNode loads a node by path id (admin context).
func (h *Handler) adminNode(c *gin.Context) (*model.Node, bool) {
	id, ok := h.parseID(c, "id")
	if !ok {
		return nil, false
	}
	node, err := h.svc.NodeByID(c.Request.Context(), id)
	if err != nil {
		response.NotFound(c, "节点不存在")
		return nil, false
	}
	return node, true
}

// NodeSelfCheck triggers scripts/verify-ndp.sh on the node's agent (admin
// panel). The check runs asynchronously (image pulls + echo probes can take
// minutes); this handler only starts it and returns the run id, which
// NodeSelfCheckStatus polls.
func (h *Handler) NodeSelfCheck(c *gin.Context) {
	node, ok := h.adminNode(c)
	if !ok {
		return
	}
	h.startSelfCheck(c, node)
}

// NodeSelfCheckStatus polls the agent for the self-check run's live output
// and terminal status (admin panel).
func (h *Handler) NodeSelfCheckStatus(c *gin.Context) {
	node, ok := h.adminNode(c)
	if !ok {
		return
	}
	h.pollSelfCheck(c, node, c.Param("runId"))
}

// NodeSelfCheckOwner is the tenant-facing variant (托管中心): the caller must
// own the node (or be an admin).
func (h *Handler) NodeSelfCheckOwner(c *gin.Context) {
	node, ok := h.tenantNode(c)
	if !ok {
		return
	}
	h.startSelfCheck(c, node)
}

// NodeSelfCheckOwnerStatus is the tenant-facing poll variant (托管中心).
func (h *Handler) NodeSelfCheckOwnerStatus(c *gin.Context) {
	node, ok := h.tenantNode(c)
	if !ok {
		return
	}
	h.pollSelfCheck(c, node, c.Param("runId"))
}

// tenantNode loads a node the caller owns (or is an admin) from the path id.
func (h *Handler) tenantNode(c *gin.Context) (*model.Node, bool) {
	id, ok := h.parseID(c, "id")
	if !ok {
		return nil, false
	}
	var node model.Node
	if err := h.db.WithContext(c.Request.Context()).First(&node, id).Error; err != nil {
		response.NotFound(c, "节点不存在")
		return nil, false
	}
	if auth.Role(c) != "admin" && node.UserID != auth.UID(c) {
		response.Forbidden(c, "无权访问该节点")
		return nil, false
	}
	return &node, true
}

// startSelfCheck is shared by the admin and tenant variants: validates the
// agent address, starts the run, persists a 「running」 snapshot.
func (h *Handler) startSelfCheck(c *gin.Context, node *model.Node) {
	if node.AgentAddr == "" {
		response.BadRequest(c, "节点未配置 Agent 地址，无法远程自检")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	st, err := h.svc.ClientForNode(node).StartSelfCheck(ctx)
	if err != nil {
		response.BadRequest(c, "触发自检失败: "+err.Error())
		return
	}
	// 立即落一条「运行中」，节点列表无需轮询即可展示进行中的自检徽章。
	h.saveSelfCheck(node.ID, selfCheckSnapshot{Status: "running", RunID: st.ID, StartedAt: time.Now()})
	response.OK(c, gin.H{"run_id": st.ID, "status": st.Status})
}

// pollSelfCheck is shared by the admin and tenant variants: fetches the run
// snapshot and persists the terminal state once (done/failed).
func (h *Handler) pollSelfCheck(c *gin.Context, node *model.Node, runID string) {
	if runID == "" {
		response.BadRequest(c, "缺少自检任务 ID")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	st, err := h.svc.ClientForNode(node).SelfCheckStatus(ctx, runID)
	if err != nil {
		response.BadRequest(c, "查询自检状态失败: "+err.Error())
		return
	}
	// 终态（通过/失败）落库；运行中由启动时已写入的快照覆盖显示。
	if st.Status == "done" || st.Status == "failed" {
		h.saveSelfCheck(node.ID, selfCheckSnapshot{
			Status:     st.Status,
			RunID:      runID,
			ExitCode:   st.ExitCode,
			StartedAt:  st.StartedAt,
			FinishedAt: st.FinishedAt,
			Output:     st.Output,
		})
	}
	response.OK(c, st)
}
