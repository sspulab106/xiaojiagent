package handler

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"example.com/codetest/master/internal/auth"
	"example.com/codetest/master/internal/config"
	"example.com/codetest/master/internal/model"
	"example.com/codetest/master/internal/service"
)

// fakeTerminalAgent is a stand-in for the real agent's WebSocket terminal
// endpoint: it requires the node bearer token, upgrades the connection and
// echoes every message back (a terminal shell echoes typed commands).
type fakeTerminalAgent struct {
	mu      atomic.Int32 // reject this many upgrade attempts (agent booting)
	token   string
	served  atomic.Int32
	handler http.Handler
	ts      *httptest.Server
	port    int
}

func newFakeTerminalAgent(t *testing.T, port int, token string, reject int) *fakeTerminalAgent {
	t.Helper()
	fa := &fakeTerminalAgent{token: token}
	fa.mu.Store(int32(reject))
	fa.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/agent/v1/instances/") || !strings.HasSuffix(r.URL.Path, "/terminal") {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+fa.token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if fa.mu.Add(-1) >= 0 {
			// Agent still booting: reject the dial so the master retries.
			http.Error(w, "agent booting", http.StatusServiceUnavailable)
			return
		}
		fa.served.Add(1)
		up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(mt, msg); err != nil {
				return
			}
		}
	})
	ts := httptest.NewUnstartedServer(fa.handler)
	if port == 0 {
		ts.Start()
		port = ts.Listener.Addr().(*net.TCPAddr).Port
	} else {
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			t.Fatalf("bind fake agent port %d: %v", port, err)
		}
		ts.Listener = l
		ts.Start()
	}
	fa.ts = ts
	fa.port = port
	t.Cleanup(ts.Close)
	return fa
}

// restart simulates the agent rebooting: the old process dies (listener
// closed), a new one binds the same address with the same token.
func (fa *fakeTerminalAgent) restart(t *testing.T, reject int) {
	t.Helper()
	fa.ts.Close()
	fa.mu.Store(int32(reject))
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", fa.port))
	if err != nil {
		t.Fatalf("rebind fake agent port %d: %v", fa.port, err)
	}
	ts := httptest.NewUnstartedServer(fa.handler)
	ts.Listener = l
	ts.Start()
	fa.ts = ts
	t.Cleanup(ts.Close)
}

func (fa *fakeTerminalAgent) url() string { return fa.ts.URL }

// terminalTestEnv wires the master handler with an in-memory DB, a user, an
// instance and a node pointing at the fake agent.
type terminalTestEnv struct {
	ts *httptest.Server
	jwt string
}

func newTerminalTestEnv(t *testing.T, agentURL, nodeToken string) *terminalTestEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Node{}, &model.Instance{}); err != nil {
		t.Fatal(err)
	}
	user := model.User{ID: 1, Username: "alice", Role: "user"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	node := model.Node{ID: 1, UserID: 1, Name: "node1", AgentAddr: agentURL, Token: nodeToken, Status: "online"}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	inst := model.Instance{ID: 1, UserID: 1, NodeID: 1, Name: "inst1", Status: "running"}
	if err := db.Create(&inst).Error; err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{JWTSecret: "test-secret", TokenTTLHours: 24}
	authM := auth.New(cfg.JWTSecret, cfg.TokenTTLHours)
	logger := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
	svc := service.New(db, cfg, logger)
	h := &Handler{db: db, svc: svc, auth: authM, cfg: cfg, log: logger}

	r := gin.New()
	r.GET("/api/v1/instances/:id/terminal", h.Terminal)
	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)

	jwt, err := authM.IssueToken(1, "alice", "user")
	if err != nil {
		t.Fatal(err)
	}
	return &terminalTestEnv{ts: ts, jwt: jwt}
}

// dialBrowser opens a browser-side WebSocket to the master terminal endpoint.
func (e *terminalTestEnv) dialBrowser(t *testing.T) (*websocket.Conn, error) {
	t.Helper()
	u := "ws" + strings.TrimPrefix(e.ts.URL, "http") + "/api/v1/instances/1/terminal?token=" + e.jwt
	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	return conn, err
}

// assertRoundTrip sends a message through the browser WS and waits for the
// echo to come back (proves the full relay browser -> master -> agent works).
func assertRoundTrip(t *testing.T, conn *websocket.Conn, marker string) {
	t.Helper()
	if err := conn.WriteMessage(websocket.TextMessage, []byte(marker)); err != nil {
		t.Fatalf("browser send: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("browser read: %v", err)
		}
		if strings.Contains(string(msg), marker) {
			return
		}
	}
}

// TestTerminalRelayRetriesAgentBootWindow verifies the master relay survives
// the agent's boot window: the fake agent rejects the first few dials (like a
// real agent that is still starting NAT rules / listening), and the master's
// dial retry loop eventually connects.
func TestTerminalRelayRetriesAgentBootWindow(t *testing.T) {
	fa := newFakeTerminalAgent(t, 0, "node-tok", 2) // reject first 2 dials
	env := newTerminalTestEnv(t, fa.url(), "node-tok")

	conn, err := env.dialBrowser(t)
	if err != nil {
		t.Fatalf("browser dial through master: %v", err)
	}
	defer conn.Close()
	assertRoundTrip(t, conn, "E2E_BOOT_WINDOW")

	if fa.served.Load() == 0 {
		t.Fatal("fake agent never served a terminal session")
	}
}

// TestTerminalReconnectAfterAgentRestart verifies the end-to-end reconnect
// path: with the agent online the relay works; the agent reboots (listener
// dies, comes back on the same address); the browser reconnects and the
// terminal works again — exactly what the frontend auto-reconnect does after
// a host reboot.
func TestTerminalReconnectAfterAgentRestart(t *testing.T) {
	fa := newFakeTerminalAgent(t, 0, "node-tok", 0)
	env := newTerminalTestEnv(t, fa.url(), "node-tok")

	// Before the "reboot": terminal works.
	conn1, err := env.dialBrowser(t)
	if err != nil {
		t.Fatalf("browser dial: %v", err)
	}
	assertRoundTrip(t, conn1, "E2E_BEFORE_AGENT_RESTART")
	conn1.Close()

	// Agent reboots.
	fa.restart(t, 0)

	// Browser reconnects (frontend auto-reconnect) and the relay works again.
	conn2, err := env.dialBrowser(t)
	if err != nil {
		t.Fatalf("browser reconnect after agent restart: %v", err)
	}
	defer conn2.Close()
	assertRoundTrip(t, conn2, "E2E_AFTER_AGENT_RESTART")
}

// TestTerminalRelayErrorWhenAgentDown verifies the browser gets a clear error
// message instead of a silent hang when the node is unreachable (agent down
// after a crash, master retries exhausted).
func TestTerminalRelayErrorWhenAgentDown(t *testing.T) {
	fa := newFakeTerminalAgent(t, 0, "node-tok", 0)
	fa.ts.Close() // agent completely down

	env := newTerminalTestEnv(t, fa.url(), "node-tok")
	conn, err := env.dialBrowser(t)
	if err != nil {
		t.Fatalf("browser dial: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("expected an error message from the master relay, got read error: %v", err)
	}
	text := string(msg)
	if !strings.Contains(text, "无法连接节点终端") {
		t.Fatalf("expected clear relay error, got: %q", text)
	}
}
