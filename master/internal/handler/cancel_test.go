package handler

import (
	"context"
	"testing"
	"time"

	"example.com/codetest/master/internal/model"
)

// seedPkg creates a package row (with the early-full-refund flag) and returns its ID.
func (e *marketTestEnv) seedPkg(t *testing.T, name string, earlyFullRefund bool) uint {
	t.Helper()
	p := model.Package{
		NodeID:          1,
		UserID:          1,
		Name:            name,
		PriceCents:      10000,
		Listed:          true,
		Enabled:         true,
		EarlyFullRefund: earlyFullRefund,
	}
	if err := e.db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	return p.ID
}

// TestCancelRefundRules verifies the cancel-machine refund matrix:
// full refund only when the package enables early full refund AND the instance
// is within 1 hour of creation AND used traffic <= 1GB; otherwise 0.
func TestCancelRefundRules(t *testing.T) {
	env := newMarketTestEnv(t)
	env.seedUser(t, 1, "alice", 10000)

	pkgOn := env.seedPkg(t, "允许早期退款", true)
	pkgOff := env.seedPkg(t, "不允许早期退款", false)

	now := time.Now()
	cases := []struct {
		name       string
		pkgID      uint
		age        time.Duration
		used       int64
		paid       int64
		wantRefund int64
	}{
		{"开启+1小时内+流量≤1G → 全额", pkgOn, 30 * time.Minute, 100 * 1024 * 1024, 8000, 8000},
		{"开启+超过1小时 → 不退款", pkgOn, 2 * time.Hour, 0, 8000, 0},
		{"开启+1小时内+流量>1G → 不退款", pkgOn, 30 * time.Minute, 2 * 1024 * 1024 * 1024, 8000, 0},
		{"开启+正好1G → 全额", pkgOn, 30 * time.Minute, 1024 * 1024 * 1024, 8000, 8000},
		{"未开启 → 一律不退款", pkgOff, 5 * time.Minute, 0, 8000, 0},
		{"未开启+即使早期 → 不退款", pkgOff, 5 * time.Minute, 0, 8000, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inst := &model.Instance{
				PackageID:       tc.pkgID,
				UserID:          1,
				CreatedAt:       now.Add(-tc.age),
				TrafficUsedUp:   tc.used,
				TrafficUsedDown: 0,
				PaidCents:       tc.paid,
				PriceCents:      10000,
			}
			got := env.h.cancelRefundCents(context.Background(), inst)
			if got != tc.wantRefund {
				t.Fatalf("cancelRefundCents = %d, want %d", got, tc.wantRefund)
			}
		})
	}
}
