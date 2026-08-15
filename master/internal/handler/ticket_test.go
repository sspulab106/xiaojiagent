package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"example.com/codetest/master/internal/config"
	"example.com/codetest/master/internal/model"
	"example.com/codetest/master/internal/service"
)

// ticketTestEnv wires an in-memory DB with the ticket + settings routes. Auth
// identity/role come from X-Test-UID / X-Test-Role headers.
type ticketTestEnv struct {
	h  *Handler
	db *gorm.DB
	r  *gin.Engine
}

var ticketEnvSeq int

func newTicketTestEnv(t *testing.T) *ticketTestEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	// 命名共享缓存的 in-memory DB：事务会占用连接，池中的其他连接必须能看到
	// 同一份数据；每个 env 用唯一名称避免测试间串数据。
	ticketEnvSeq++
	dsn := fmt.Sprintf("file:memdb%d?mode=memory&cache=shared", ticketEnvSeq)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.User{}, &model.Node{}, &model.Instance{},
		&model.Ticket{}, &model.TicketReply{}, &model.Setting{},
	); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{JWTSecret: "test-secret", TokenTTLHours: 24}
	svc := service.New(db, cfg, nil)
	h := &Handler{db: db, svc: svc, cfg: cfg}

	r := gin.New()
	authed := r.Group("/api/v1")
	authed.Use(func(c *gin.Context) {
		uid, _ := strconv.ParseUint(c.GetHeader("X-Test-UID"), 10, 64)
		role := c.GetHeader("X-Test-Role")
		if role == "" {
			role = "user"
		}
		c.Set("auth.uid", uint(uid))
		c.Set("auth.role", role)
		c.Set("auth.username", "u")
		c.Next()
	})
	authed.GET("/tickets", h.ListTickets)
	authed.GET("/tickets/unread-count", h.TicketUnreadCount)
	authed.POST("/tickets", h.CreateTicket)
	authed.GET("/tickets/:id", h.GetTicket)
	authed.POST("/tickets/:id/reply", h.ReplyTicket)
	authed.POST("/tickets/:id/resolve", h.ResolveTicket)
	authed.POST("/tickets/:id/reopen", h.ReopenTicket)
	authed.GET("/settings", h.GetSettings)
	authed.PUT("/settings", h.UpdateSettings)
	return &ticketTestEnv{h: h, db: db, r: r}
}

func (e *ticketTestEnv) seedUser(t *testing.T, id uint, username string, role string) {
	t.Helper()
	u := model.User{ID: id, Username: username, Role: role}
	if err := e.db.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
}

func (e *ticketTestEnv) seedNode(t *testing.T, id uint, owner uint) {
	t.Helper()
	n := model.Node{ID: id, Name: "node" + strconv.Itoa(int(id)), UserID: owner}
	if err := e.db.Create(&n).Error; err != nil {
		t.Fatal(err)
	}
}

func (e *ticketTestEnv) seedInstance(t *testing.T, id, userID, nodeID uint) {
	t.Helper()
	inst := model.Instance{ID: id, Name: "inst", DisplayName: "鸡", UserID: userID, NodeID: nodeID}
	if err := e.db.Create(&inst).Error; err != nil {
		t.Fatal(err)
	}
}

func (e *ticketTestEnv) do(t *testing.T, method, path string, uid uint, role string, body any) (int, envelope) {
	t.Helper()
	var rdrBody io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdrBody = strings.NewReader(string(b))
	}
	req := httptest.NewRequest(method, path, rdrBody)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-UID", strconv.Itoa(int(uid)))
	if role != "" {
		req.Header.Set("X-Test-Role", role)
	}
	rec := httptest.NewRecorder()
	e.r.ServeHTTP(rec, req)
	var env envelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	return rec.Code, env
}

func decodeTicket(t *testing.T, raw json.RawMessage) model.Ticket {
	t.Helper()
	var tk model.Ticket
	if err := json.Unmarshal(raw, &tk); err != nil {
		t.Fatalf("decode ticket: %v", err)
	}
	return tk
}

// TestTicketFlow covers the full support-ticket lifecycle with permission
// isolation: user submits for own instance → routed to node owner → unread
// bubbles on both sides → replies flip unread → resolve clears → reopen.
func TestTicketFlow(t *testing.T) {
	env := newTicketTestEnv(t)
	// uid 1 = 机主（宿主机所有者）, uid 2 = 普通用户, uid 3 = 平台管理员
	env.seedUser(t, 1, "host", "user")
	env.seedUser(t, 2, "buyer", "user")
	env.seedUser(t, 3, "root", "admin")
	env.seedNode(t, 1, 1)
	env.seedNode(t, 2, 2)
	env.seedInstance(t, 1, 2, 1) // 用户 2 的实例，在节点 1（机主 1）上
	env.seedInstance(t, 2, 2, 2) // 用户 2 的实例，在节点 2（机主 2）上

	// 不能为别人的实例提工单。
	if code, envResp := env.do(t, "POST", "/api/v1/tickets", 1, "", map[string]any{
		"instance_id": 1, "title": "帮忙看看", "content": "SSH 连不上",
	}); code != http.StatusForbidden {
		t.Fatalf("foreign ticket: got %d %s", code, envResp.Message)
	}
	// 用户 2 为自己的实例提工单（节点 1 上的实例）。
	if code, envResp := env.do(t, "POST", "/api/v1/tickets", 2, "", map[string]any{
		"instance_id": 1, "title": "SSH 连不上", "content": "重启后 SSH 拒绝连接",
	}); code != http.StatusCreated {
		t.Fatalf("create ticket: got %d %s", code, envResp.Message)
	}
	// 用户 2 再为节点 2 上的实例提一张。
	env.do(t, "POST", "/api/v1/tickets", 2, "", map[string]any{
		"instance_id": 2, "title": "流量异常", "content": "流量消耗很快",
	})

	// 可见性：机主 1 只看到路由到自己节点的工单；用户 2 看到自己的两张；管理员看到全部。
	var list []model.Ticket
	_, envResp := env.do(t, "GET", "/api/v1/tickets", 1, "", nil)
	_ = json.Unmarshal(envResp.Data, &list)
	if len(list) != 1 || list[0].InstanceName != "鸡" || list[0].UserName != "buyer" {
		t.Fatalf("host sees wrong tickets: %+v", list)
	}
	_, envResp = env.do(t, "GET", "/api/v1/tickets", 2, "", nil)
	_ = json.Unmarshal(envResp.Data, &list)
	if len(list) != 2 {
		t.Fatalf("buyer sees %d tickets, want 2", len(list))
	}
	_, envResp = env.do(t, "GET", "/api/v1/tickets", 3, "admin", nil)
	_ = json.Unmarshal(envResp.Data, &list)
	if len(list) != 2 {
		t.Fatalf("admin sees %d tickets, want 2", len(list))
	}

	// 未读气泡：机主 1 有 1 条 node_unread（节点 1 上的工单）；用户 2 无未读（自己的工单已读）。
	type unread struct {
		NodeUnread int64 `json:"node_unread"`
		UserUnread int64 `json:"user_unread"`
	}
	var u unread
	_, envResp = env.do(t, "GET", "/api/v1/tickets/unread-count", 1, "", nil)
	_ = json.Unmarshal(envResp.Data, &u)
	if u.NodeUnread != 1 || u.UserUnread != 0 {
		t.Fatalf("host unread wrong: %+v", u)
	}
	_, envResp = env.do(t, "GET", "/api/v1/tickets/unread-count", 2, "", nil)
	_ = json.Unmarshal(envResp.Data, &u)
	if u.NodeUnread != 0 || u.UserUnread != 0 {
		t.Fatalf("buyer unread wrong: %+v", u)
	}

	// 机主 1 查看工单 1 → node_read 置位，未读清零。
	if code, _ := env.do(t, "GET", "/api/v1/tickets/1", 1, "", nil); code != http.StatusOK {
		t.Fatalf("get ticket: %d", code)
	}
	_, envResp = env.do(t, "GET", "/api/v1/tickets/unread-count", 1, "", nil)
	_ = json.Unmarshal(envResp.Data, &u)
	if u.NodeUnread != 0 {
		t.Fatalf("host unread after view: %+v", u)
	}

	// 机主回复 → 用户 2 未读 +1；工单 1 用户 2 查看后清零。
	if code, _ := env.do(t, "POST", "/api/v1/tickets/1/reply", 1, "", map[string]any{"content": "请提供 SSH 端口"}); code != http.StatusCreated {
		t.Fatalf("reply: %d", code)
	}
	_, envResp = env.do(t, "GET", "/api/v1/tickets/unread-count", 2, "", nil)
	_ = json.Unmarshal(envResp.Data, &u)
	if u.UserUnread != 1 {
		t.Fatalf("buyer unread after reply: %+v", u)
	}
	if code, _ := env.do(t, "GET", "/api/v1/tickets/1", 2, "", nil); code != http.StatusOK {
		t.Fatalf("buyer get: %d", code)
	}
	_, envResp = env.do(t, "GET", "/api/v1/tickets/unread-count", 2, "", nil)
	_ = json.Unmarshal(envResp.Data, &u)
	if u.UserUnread != 0 {
		t.Fatalf("buyer unread after view: %+v", u)
	}

	// 用户回复 → 机主再次未读；回复可见且带用户名。先查未读再取详情
	// （取详情会把读取方标记为已读）。
	if code, envResp := env.do(t, "POST", "/api/v1/tickets/1/reply", 2, "", map[string]any{"content": "端口 22"}); code != http.StatusCreated {
		t.Fatalf("user reply: %d %s", code, envResp.Message)
	}
	_, envResp = env.do(t, "GET", "/api/v1/tickets/unread-count", 1, "", nil)
	_ = json.Unmarshal(envResp.Data, &u)
	if u.NodeUnread != 1 {
		t.Fatalf("host unread after user reply: %+v", u)
	}
	full := env.fetchTicket(t, 1, 2, "") // 以买家身份取详情验证回复（买家侧读取不会清机主未读）
	if len(full.Replies) != 2 || full.Replies[0].UserName != "host" || full.Replies[1].UserName != "buyer" {
		t.Fatalf("replies wrong: %+v", full.Replies)
	}

	// 无关用户 2 的工单 2 不能被机主 1 看到。
	if code, _ := env.do(t, "GET", "/api/v1/tickets/2", 1, "", nil); code != http.StatusForbidden {
		t.Fatalf("host sees foreign ticket: %d", code)
	}

	// 机主 1 标记已解决 → 未读清零；解决后不能回复。
	if code, _ := env.do(t, "POST", "/api/v1/tickets/1/resolve", 1, "", nil); code != http.StatusOK {
		t.Fatalf("resolve: %d", code)
	}
	if code, _ := env.do(t, "POST", "/api/v1/tickets/1/reply", 1, "", map[string]any{"content": "再补一句"}); code != http.StatusBadRequest {
		t.Fatalf("reply resolved: %d", code)
	}
	_, envResp = env.do(t, "GET", "/api/v1/tickets/unread-count", 1, "", nil)
	_ = json.Unmarshal(envResp.Data, &u)
	if u.NodeUnread != 0 {
		t.Fatalf("host unread after resolve: %+v", u)
	}

	// 重新打开后可继续回复。
	if code, _ := env.do(t, "POST", "/api/v1/tickets/1/reopen", 1, "", nil); code != http.StatusOK {
		t.Fatalf("reopen: %d", code)
	}
	if code, _ := env.do(t, "POST", "/api/v1/tickets/1/reply", 2, "", map[string]any{"content": "已解决，谢谢"}); code != http.StatusCreated {
		t.Fatalf("reply reopened: %d", code)
	}

	// 管理员可代看全部并可解决工单 2。
	if code, _ := env.do(t, "GET", "/api/v1/tickets/2", 3, "admin", nil); code != http.StatusOK {
		t.Fatalf("admin get ticket 2: %d", code)
	}
	if code, _ := env.do(t, "POST", "/api/v1/tickets/2/resolve", 3, "admin", nil); code != http.StatusOK {
		t.Fatalf("admin resolve ticket 2: %d", code)
	}
}

func (e *ticketTestEnv) fetchTicket(t *testing.T, id uint, uid uint, role string) model.Ticket {
	t.Helper()
	code, envResp := e.do(t, "GET", "/api/v1/tickets/"+strconv.Itoa(int(id)), uid, role, nil)
	if code != http.StatusOK {
		t.Fatalf("fetch ticket %d: %d %s", id, code, envResp.Message)
	}
	return decodeTicket(t, envResp.Data)
}

// TestSettingsAdminOnly verifies platform settings are admin-only and that an
// empty smtp_pass keeps the previously stored password (no accidental wipe).
func TestSettingsAdminOnly(t *testing.T) {
	env := newTicketTestEnv(t)
	env.seedUser(t, 1, "host", "user")
	env.seedUser(t, 3, "root", "admin")

	// 非管理员读写都被拒绝。
	if code, _ := env.do(t, "GET", "/api/v1/settings", 1, "", nil); code != http.StatusForbidden {
		t.Fatalf("non-admin get settings: %d", code)
	}
	if code, _ := env.do(t, "PUT", "/api/v1/settings", 1, "", map[string]any{"smtp_host": "x"}); code != http.StatusForbidden {
		t.Fatalf("non-admin put settings: %d", code)
	}

	// 管理员写入。
	if code, envResp := env.do(t, "PUT", "/api/v1/settings", 3, "admin", map[string]any{
		"smtp_host": "smtp.example.com", "smtp_port": "587", "smtp_user": "noreply@example.com",
		"smtp_pass": "secret1", "smtp_from": "独角鲸云 <noreply@example.com>",
		"email_verify_enabled": "1", "cloudflare_captcha_enabled": "0",
	}); code != http.StatusOK {
		t.Fatalf("admin put settings: %d %s", code, envResp.Message)
	}
	code, envResp := env.do(t, "GET", "/api/v1/settings", 3, "admin", nil)
	if code != http.StatusOK {
		t.Fatalf("admin get settings: %d", code)
	}
	var got map[string]string
	_ = json.Unmarshal(envResp.Data, &got)
	if got["smtp_host"] != "smtp.example.com" || got["email_verify_enabled"] != "1" {
		t.Fatalf("settings wrong: %+v", got)
	}

	// 密码留空 → 保持原值。
	env.do(t, "PUT", "/api/v1/settings", 3, "admin", map[string]any{"smtp_pass": "", "smtp_host": "smtp2.example.com"})
	_, envResp = env.do(t, "GET", "/api/v1/settings", 3, "admin", nil)
	_ = json.Unmarshal(envResp.Data, &got)
	if got["smtp_pass"] != "secret1" {
		t.Fatalf("smtp_pass was wiped: %+v", got)
	}
	if got["smtp_host"] != "smtp2.example.com" {
		t.Fatalf("smtp_host not updated: %+v", got)
	}

	// 未知键被忽略。
	env.do(t, "PUT", "/api/v1/settings", 3, "admin", map[string]any{"evil_key": "x"})
	_, envResp = env.do(t, "GET", "/api/v1/settings", 3, "admin", nil)
	_ = json.Unmarshal(envResp.Data, &got)
	if _, exists := got["evil_key"]; exists {
		t.Fatalf("unknown key persisted: %+v", got)
	}
}
