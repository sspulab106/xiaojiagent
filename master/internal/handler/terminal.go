package handler

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"example.com/codetest/master/internal/model"
)

// proxyTerminal bridges a browser WebSocket to the agent's WebSocket
// terminal endpoint. The agent authenticates the relay connection with the
// node's bearer token (set as an HTTP header on the dial, which only the
// server side can do).
func (h *Handler) proxyTerminal(c *gin.Context, node *model.Node, instanceName string) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	clientConn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer clientConn.Close()

	// 节点离线时直接给出明确原因，而不是让浏览器看到晦涩的传输层错误。
	if node.Status != "online" {
		_ = clientConn.WriteMessage(websocket.TextMessage, []byte("节点当前离线，无法连接终端，请检查宿主机 Agent 状态"))
		return
	}

	// Build the agent WebSocket URL: http -> ws, https -> wss.
	scheme := "ws"
	base := strings.TrimPrefix(node.AgentAddr, "http://")
	if strings.HasPrefix(node.AgentAddr, "https://") {
		scheme = "wss"
		base = strings.TrimPrefix(node.AgentAddr, "https://")
	}
	agentURL := scheme + "://" + base + "/agent/v1/instances/" + instanceName + "/terminal"

	// 整机重启后 Agent 可能仍在启动窗口（main 会先重建 NAT/IPv6 规则再监听），
	// 单次拨号失败即永久断连。重试几次短退避，跳过 Agent 启动竞态。
	header := http.Header{"Authorization": []string{"Bearer " + node.Token}}
	var agentConn *websocket.Conn
	dialCtx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	for attempt := 1; attempt <= 5; attempt++ {
		agentConn, _, err = websocket.DefaultDialer.DialContext(dialCtx, agentURL, header)
		if err == nil {
			break
		}
		h.log.Warn("agent terminal dial failed", "node", node.Name, "addr", node.AgentAddr, "attempt", attempt, "err", err)
		if attempt < 5 {
			select {
			case <-time.After(time.Duration(attempt) * 300 * time.Millisecond):
			case <-dialCtx.Done():
			}
		}
	}
	if agentConn == nil {
		_ = clientConn.WriteMessage(websocket.TextMessage, []byte("无法连接节点终端("+node.AgentAddr+"): "+err.Error()))
		return
	}
	defer agentConn.Close()

	// Bidirectional relay. Browser -> agent and agent -> browser.
	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			_, msg, err := clientConn.ReadMessage()
			if err != nil {
				return
			}
			if err := agentConn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			_, msg, err := agentConn.ReadMessage()
			if err != nil {
				return
			}
			if err := clientConn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(24 * time.Hour):
	}
}
