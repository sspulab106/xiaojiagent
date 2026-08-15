package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"example.com/codetest/master/internal/auth"
	"example.com/codetest/master/internal/config"
	"example.com/codetest/master/internal/model"
	"example.com/codetest/master/internal/service"
)

// newTestHandler builds a Handler on an in-memory sqlite DB with the Node
// schema migrated.
func newTestHandler(t *testing.T) (*Handler, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Node{}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{JWTSecret: "0123456789abcdef0123456789abcdef", TokenTTLHours: 72}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	svc := service.New(db, cfg, logger)
	h := New(db, svc, auth.New(cfg.JWTSecret, cfg.TokenTTLHours), cfg, logger)
	return h, db
}

// authedContext builds a gin context whose auth middleware has resolved the
// given uid/role from a real signed JWT. Route params must be set manually
// because CreateTestContext does no route matching.
func authedContext(t *testing.T, h *Handler, uid uint, role string, nodeID uint) *gin.Context {
	t.Helper()
	tok, err := auth.Sign(h.cfg.JWTSecret, time.Hour, uid, "u", role)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/nodes/1/selfcheck", nil)
	c.Request.Header.Set("Authorization", "Bearer "+tok)
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(nodeID)}}
	h.auth.Middleware()(c)
	return c
}

// TestNodeSelfCheckOwnership verifies tenantNode: owner passes, a stranger is
// forbidden (403), admins pass.
func TestNodeSelfCheckOwnership(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, db := newTestHandler(t)
	owner := model.Node{Name: "mine", UserID: 7, Status: "online", AgentAddr: "http://10.0.0.1:8792"}
	other := model.Node{Name: "theirs", UserID: 8, Status: "online", AgentAddr: "http://10.0.0.2:8792"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}

	// 机主本人可通过
	c := authedContext(t, h, 7, "user", owner.ID)
	if c.IsAborted() {
		t.Fatal("owner auth should not abort")
	}
	node, ok := h.tenantNode(c)
	if !ok || node.ID != owner.ID {
		t.Fatalf("owner should access own node, got id=%d ok=%v", node.ID, ok)
	}

	// 非机主 -> 403
	c2 := authedContext(t, h, 9, "user", other.ID)
	if c2.IsAborted() {
		t.Fatal("stranger auth should not abort")
	}
	if _, ok := h.tenantNode(c2); ok {
		t.Fatal("stranger should be forbidden")
	}
	if c2.Writer.Status() != http.StatusForbidden {
		t.Fatalf("stranger status = %d, want 403", c2.Writer.Status())
	}

	// 管理员可通过任意节点
	c3 := authedContext(t, h, 1, "admin", other.ID)
	if c3.IsAborted() {
		t.Fatal("admin auth should not abort")
	}
	if _, ok := h.tenantNode(c3); !ok {
		t.Fatal("admin should pass")
	}
}

func TestSaveSelfCheckSnapshot(t *testing.T) {
	h, db := newTestHandler(t)
	node := model.Node{Name: "jp-1", UserID: 0, Status: "online"}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}

	// 运行中快照
	h.saveSelfCheck(node.ID, selfCheckSnapshot{Status: "running", RunID: "run-1", StartedAt: time.Now()})
	var n model.Node
	if err := db.First(&n, node.ID).Error; err != nil {
		t.Fatal(err)
	}
	var snap selfCheckSnapshot
	if err := json.Unmarshal([]byte(n.LastSelfcheck), &snap); err != nil {
		t.Fatalf("last_selfcheck not valid JSON: %v (%q)", err, n.LastSelfcheck)
	}
	if snap.Status != "running" || snap.RunID != "run-1" {
		t.Fatalf("running snapshot = %+v", snap)
	}

	// 终态快照覆盖，且 output 超长被截断
	longOut := strings.Repeat("x", selfCheckOutputCap+500)
	h.saveSelfCheck(node.ID, selfCheckSnapshot{
		Status: "failed", RunID: "run-1", ExitCode: 3,
		StartedAt: time.Now().Add(-time.Minute), FinishedAt: time.Now(),
		Output: longOut,
	})
	if err := db.First(&n, node.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(n.LastSelfcheck), &snap); err != nil {
		t.Fatalf("terminal snapshot invalid: %v", err)
	}
	if snap.Status != "failed" || snap.ExitCode != 3 {
		t.Fatalf("terminal snapshot = %+v", snap)
	}
	if len(snap.Output) != selfCheckOutputCap {
		t.Fatalf("output len = %d, want cap %d", len(snap.Output), selfCheckOutputCap)
	}
	if !strings.HasSuffix(snap.Output, strings.Repeat("x", 100)) {
		t.Fatal("truncation kept the tail, expected")
	}
	if snap.FinishedAt.IsZero() {
		t.Fatal("finished_at not persisted")
	}
}
