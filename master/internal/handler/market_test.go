package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"example.com/codetest/master/internal/config"
	"example.com/codetest/master/internal/model"
	"example.com/codetest/master/internal/service"
)

// marketTestEnv wires an in-memory sqlite DB with the market routes plus the
// instance-delete route (to verify the listed-instance delete guard).
type marketTestEnv struct {
	h  *Handler
	db *gorm.DB
	r  *gin.Engine
}

func newMarketTestEnv(t *testing.T) *marketTestEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.User{}, &model.Package{}, &model.Node{}, &model.Instance{},
		&model.PortMapping{}, &model.TrafficLog{}, &model.Announcement{},
		&model.Transaction{}, &model.Coupon{}, &model.GiftCard{}, &model.MarketListing{},
	); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{JWTSecret: "test-secret", TokenTTLHours: 24}
	svc := service.New(db, cfg, nil)
	h := &Handler{db: db, svc: svc, cfg: cfg}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	authed := r.Group("/api/v1")
	// Test auth: identity comes from X-Test-UID header.
	authed.Use(func(c *gin.Context) {
		uid, _ := strconv.ParseUint(c.GetHeader("X-Test-UID"), 10, 64)
		c.Set("auth.uid", uint(uid))
		c.Set("auth.role", "user")
		c.Set("auth.username", "u")
		c.Next()
	})
	authed.GET("/market/listings", h.ListMarketListings)
	authed.GET("/market/listings/mine", h.ListMyMarketListings)
	authed.POST("/market/listings", h.CreateMarketListing)
	authed.DELETE("/market/listings/:id", h.CancelMarketListing)
	authed.POST("/market/listings/:id/buy", h.BuyMarketListing)
	authed.DELETE("/instances/:id", h.DeleteInstance)
	return &marketTestEnv{h: h, db: db, r: r}
}

func (e *marketTestEnv) seedUser(t *testing.T, id uint, username string, balance int64) {
	t.Helper()
	u := model.User{ID: id, Username: username, BalanceCents: balance, InstanceQuota: 10}
	if err := e.db.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
}

// seedInstance creates an instance owned by userID with the given remaining
// days and used-traffic share, returning its ID.
func (e *marketTestEnv) seedInstance(t *testing.T, userID uint, paidCents int64, remainingDays int, usedPct int) uint {
	t.Helper()
	expires := time.Now().Add(time.Duration(remainingDays) * 24 * time.Hour)
	inst := model.Instance{
		Name:           "inst" + strconv.Itoa(int(userID)) + strconv.Itoa(remainingDays),
		DisplayName:    "测试鸡" + strconv.Itoa(int(userID)),
		UserID:         userID,
		NodeID:         1,
		PackageID:      1,
		Image:          "debian/13",
		Status:         "running",
		TrafficGB:      1000,
		PaidCents:      paidCents,
		PriceCents:     paidCents,
		ExpiresAt:      &expires,
		TrafficMonth:   time.Now().Format("2006-01"),
		TrafficUsedUp:  1000 * 1024 * 1024 * 1024 * int64(usedPct) / 100,
		TrafficUsedDown: 0,
	}
	if err := e.db.Create(&inst).Error; err != nil {
		t.Fatal(err)
	}
	return inst.ID
}

type envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (e *marketTestEnv) do(t *testing.T, method, path string, uid uint, body any) (int, envelope) {
	t.Helper()
	var rdr *httptest.ResponseRecorder
	var rdrBody io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdrBody = strings.NewReader(string(b))
	}
	req := httptest.NewRequest(method, path, rdrBody)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-UID", strconv.Itoa(int(uid)))
	rdr = httptest.NewRecorder()
	e.r.ServeHTTP(rdr, req)
	var env envelope
	_ = json.Unmarshal(rdr.Body.Bytes(), &env)
	return rdr.Code, env
}

func decodeListing(t *testing.T, raw json.RawMessage) model.MarketListing {
	t.Helper()
	var l model.MarketListing
	if err := json.Unmarshal(raw, &l); err != nil {
		t.Fatalf("decode listing: %v", err)
	}
	return l
}

func TestMarketSellBuyFlow(t *testing.T) {
	env := newMarketTestEnv(t)
	env.seedUser(t, 1, "alice", 10000) // seller
	env.seedUser(t, 2, "bob", 50000)   // buyer
	instID := env.seedInstance(t, 1, 10000, 20, 10) // 1000GB quota, 10% used

	// A 以 5000 分上架（剩余价值应为 时间6666 × 流量90% = 5999）。
	code, envResp := env.do(t, "POST", "/api/v1/market/listings", 1, map[string]any{"instance_id": instID, "price_cents": 5000})
	if code != http.StatusCreated {
		t.Fatalf("list: got %d %s", code, envResp.Message)
	}
	l := decodeListing(t, envResp.Data)
	if l.Status != "listed" || l.SellerID != 1 || l.PriceCents != 5000 {
		t.Fatalf("bad listing: %+v", l)
	}
	if l.ValueCents != 5999 || l.TrafficRatio != 90 {
		t.Fatalf("remaining value mismatch: value=%d ratio=%d want 5999/90", l.ValueCents, l.TrafficRatio)
	}
	if l.Instance == nil || l.Instance.UserID != 1 {
		t.Fatalf("embedded instance missing or wrong owner: %+v", l.Instance)
	}

	// 重复上架被拒绝。
	if code, envResp := env.do(t, "POST", "/api/v1/market/listings", 1, map[string]any{"instance_id": instID, "price_cents": 100}); code != http.StatusBadRequest {
		t.Fatalf("duplicate list: got %d %s", code, envResp.Message)
	}
	// 超过原价抬价被拒绝。
	if code, envResp := env.do(t, "POST", "/api/v1/market/listings", 1, map[string]any{"instance_id": instID, "price_cents": 15000}); code != http.StatusBadRequest {
		t.Fatalf("overprice list: got %d %s", code, envResp.Message)
	}
	// 非属主不能上架。
	if code, envResp := env.do(t, "POST", "/api/v1/market/listings", 2, map[string]any{"instance_id": instID, "price_cents": 100}); code != http.StatusForbidden {
		t.Fatalf("foreign list: got %d %s", code, envResp.Message)
	}
	// 过期实例不能上架。
	expiredID := env.seedInstance(t, 1, 10000, -1, 0)
	if code, envResp := env.do(t, "POST", "/api/v1/market/listings", 1, map[string]any{"instance_id": expiredID, "price_cents": 100}); code != http.StatusBadRequest {
		t.Fatalf("expired list: got %d %s", code, envResp.Message)
	}

	// 市场列表（买家视角）应含该挂单。
	code, envResp = env.do(t, "GET", "/api/v1/market/listings", 2, nil)
	if code != http.StatusOK {
		t.Fatalf("list market: got %d %s", code, envResp.Message)
	}
	var all []model.MarketListing
	if err := json.Unmarshal(envResp.Data, &all); err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].SellerName != "alice" {
		t.Fatalf("market listings wrong: %+v", all)
	}

	// B 购买。
	code, envResp = env.do(t, "POST", "/api/v1/market/listings/1/buy", 2, nil)
	if code != http.StatusOK {
		t.Fatalf("buy: got %d %s", code, envResp.Message)
	}
	bought := decodeListing(t, envResp.Data)
	if bought.Status != "sold" || bought.BuyerID != 2 {
		t.Fatalf("buy result: %+v", bought)
	}

	// 账务校验：B 扣 5000，A 得 5000；实例过户且 paid_cents 记为成交价。
	var a, b model.User
	env.db.First(&a, 1)
	env.db.First(&b, 2)
	if a.BalanceCents != 15000 || b.BalanceCents != 45000 {
		t.Fatalf("balances wrong: alice=%d bob=%d", a.BalanceCents, b.BalanceCents)
	}
	var inst model.Instance
	env.db.First(&inst, instID)
	if inst.UserID != 2 || inst.PaidCents != 5000 {
		t.Fatalf("instance transfer wrong: user=%d paid=%d", inst.UserID, inst.PaidCents)
	}
	var txs []model.Transaction
	env.db.Where("user_id IN (1,2)").Order("id ASC").Find(&txs)
	if len(txs) != 2 || txs[0].Type != "purchase" || txs[0].UserID != 2 || txs[1].Type != "sale_income" || txs[1].UserID != 1 {
		t.Fatalf("transactions wrong: %+v", txs)
	}

	// 已售挂单不可再买。
	if code, envResp := env.do(t, "POST", "/api/v1/market/listings/1/buy", 2, nil); code != http.StatusBadRequest {
		t.Fatalf("rebuy sold: got %d %s", code, envResp.Message)
	}

	// 余额不足时拒绝。
	env.seedUser(t, 3, "carol", 10)
	inst2 := env.seedInstance(t, 1, 10000, 20, 0)
	env.do(t, "POST", "/api/v1/market/listings", 1, map[string]any{"instance_id": inst2, "price_cents": 5000})
	if code, envResp := env.do(t, "POST", "/api/v1/market/listings/2/buy", 3, nil); code != http.StatusBadRequest || envResp.Message != "余额不足，请先充值" {
		t.Fatalf("poor buyer: got %d %s", code, envResp.Message)
	}

	// 卖家不能买自己的挂单。
	if code, envResp := env.do(t, "POST", "/api/v1/market/listings/2/buy", 1, nil); code != http.StatusBadRequest {
		t.Fatalf("self buy: got %d %s", code, envResp.Message)
	}

	// 在售实例禁止删除。
	if code, envResp := env.do(t, "DELETE", "/api/v1/instances/"+strconv.Itoa(int(inst2)), 1, nil); code != http.StatusBadRequest {
		t.Fatalf("delete listed instance: got %d %s", code, envResp.Message)
	}

	// 下架后：状态变为 cancelled，可重新上架。
	if code, envResp := env.do(t, "DELETE", "/api/v1/market/listings/2", 1, nil); code != http.StatusOK {
		t.Fatalf("cancel: got %d %s", code, envResp.Message)
	}
	var cancelled model.MarketListing
	env.db.First(&cancelled, 2)
	if cancelled.Status != "cancelled" || cancelled.CancelledAt == nil {
		t.Fatalf("cancel result: %+v", cancelled)
	}
	if code, envResp := env.do(t, "POST", "/api/v1/market/listings", 1, map[string]any{"instance_id": inst2, "price_cents": 4000}); code != http.StatusCreated {
		t.Fatalf("relist: got %d %s", code, envResp.Message)
	}

	// 我的挂单应包含全部历史。
	code, envResp = env.do(t, "GET", "/api/v1/market/listings/mine", 1, nil)
	if code != http.StatusOK {
		t.Fatalf("mine: got %d %s", code, envResp.Message)
	}
	var mine []model.MarketListing
	if err := json.Unmarshal(envResp.Data, &mine); err != nil {
		t.Fatal(err)
	}
	statuses := map[string]int{}
	for _, m := range mine {
		statuses[m.Status]++
	}
	// 挂单 #1 已售；实例 2 下架一次 + 重新上架一次。
	if statuses["listed"] != 1 || statuses["sold"] != 1 || statuses["cancelled"] != 1 {
		t.Fatalf("mine statuses wrong: %+v", statuses)
	}
}
