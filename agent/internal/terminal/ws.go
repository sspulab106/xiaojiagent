// Package terminal bridges a WebSocket to a pty running a shell inside a
// container. It is backend-agnostic: the service passes the concrete exec
// command (incus exec / docker exec / podman exec).
package terminal

import (
	"encoding/json"
	"net/http"
	"os/exec"

	"github.com/creack/pty"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type resizeMsg struct {
	Type string `json:"type"`
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// Handle upgrades the request to a WebSocket and relays bytes to/from the
// container shell spawned with the given command.
func Handle(c *gin.Context, cmd []string) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	if len(cmd) == 0 {
		_ = conn.WriteMessage(websocket.TextMessage, []byte("终端命令为空"))
		return
	}

	proc := exec.Command(cmd[0], cmd[1:]...)
	proc.Env = append(proc.Environ(), "TERM=xterm-256color")
	ptmx, err := pty.Start(proc)
	if err != nil {
		// 提示可能的根因：整机重启后容器若未自动拉起（或正在重启），exec 会立刻失败。
		_ = conn.WriteMessage(websocket.TextMessage, []byte("启动终端失败（实例可能已停止或正在重启，请先在实例管理页启动它）: "+err.Error()))
		return
	}
	defer func() {
		_ = ptmx.Close()
		_ = proc.Wait()
	}()

	_ = pty.Setsize(ptmx, &pty.Winsize{Rows: 24, Cols: 80})

	// Container output -> WebSocket.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				if err := conn.WriteMessage(websocket.TextMessage, buf[:n]); err != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// WebSocket -> container stdin, with resize handling.
	for {
		msgType, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if msgType == websocket.BinaryMessage || msgType == websocket.TextMessage {
			if r, ok := parseResize(msg); ok {
				_ = pty.Setsize(ptmx, &pty.Winsize{Cols: r.Cols, Rows: r.Rows})
				continue
			}
			if _, err := ptmx.Write(msg); err != nil {
				return
			}
		}
	}
}

// parseResize heuristically detects a JSON resize message vs raw terminal
// bytes: only JSON objects carrying cols/rows are treated as resizes.
func parseResize(msg []byte) (resizeMsg, bool) {
	if len(msg) == 0 || msg[0] != '{' {
		return resizeMsg{}, false
	}
	var probe resizeMsg
	if err := json.Unmarshal(msg, &probe); err != nil {
		return resizeMsg{}, false
	}
	if probe.Type == "resize" || probe.Cols > 0 || probe.Rows > 0 {
		return probe, true
	}
	return resizeMsg{}, false
}
