package handler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"example.com/codetest/master/internal/auth"
	"example.com/codetest/master/internal/model"
	"example.com/codetest/master/internal/response"
)

// monthLen is the billing window used for pro-rata value/refund math.
const monthLen = 30 * 24 * time.Hour

// remainingValue computes the reference 剩余价值 of an unexpired instance:
//
//	剩余价值 = 剩余时间价值 × 流量剩余率
//
// where 剩余时间价值 is the pro-rata portion of the original payment left
// before ExpiresAt (clamped to one month) and 流量剩余率 is the fraction of
// the current month's traffic allowance still unused (1 for unlimited).
// Arithmetic is done in minutes (not nanoseconds) to keep paid×remaining
// well within int64 range.
func remainingValue(inst *model.Instance) (value, timeValue int64, trafficRatio int64, remainingDays float64) {
	paid := inst.PaidCents
	if paid <= 0 {
		paid = inst.PriceCents
	}
	now := time.Now()
	expiry := now.Add(monthLen)
	if inst.ExpiresAt != nil {
		expiry = *inst.ExpiresAt
	}
	remaining := expiry.Sub(now)
	if remaining <= 0 {
		return 0, 0, 0, 0
	}
	if remaining > monthLen {
		remaining = monthLen
	}
	remainingDays = remaining.Hours() / 24
	timeValue = paid * int64(remaining.Minutes()) / int64(monthLen.Minutes())
	if timeValue < 0 {
		timeValue = 0
	}
	trafficRatio = 100
	if inst.TrafficGB > 0 {
		total := inst.TrafficGB * 1024 * 1024 * 1024
		used := inst.TrafficUsedUp + inst.TrafficUsedDown
		if used >= total {
			trafficRatio = 0
		} else if total > 0 {
			trafficRatio = (total - used) * 100 / total
		}
	}
	value = timeValue * trafficRatio / 100
	return value, timeValue, trafficRatio, remainingDays
}

// decorateMarketListing fills the computed fields (seller name, remaining
// value breakdown, embedded instance) on a listing.
func (h *Handler) decorateMarketListing(ctx context.Context, l *model.MarketListing) {
	var inst model.Instance
	if err := h.db.WithContext(ctx).First(&inst, l.InstanceID).Error; err == nil {
		h.decorate(ctx, &inst)
		l.Instance = &inst
		l.ValueCents, l.TimeValueCents, l.TrafficRatio, l.RemainingDays = remainingValue(&inst)
		l.Expired = inst.ExpiresAt == nil || inst.ExpiresAt.Before(time.Now())
	} else {
		l.Expired = true
	}
	var seller model.User
	if err := h.db.WithContext(ctx).Select("username").First(&seller, l.SellerID).Error; err == nil {
		l.SellerName = seller.Username
	}
}

// ListMarketListings returns all buyable listings (listed, instance still
// alive and unexpired), newest first, each with the embedded instance and the
// computed remaining value.
func (h *Handler) ListMarketListings(c *gin.Context) {
	ctx := c.Request.Context()
	var listings []model.MarketListing
	if err := h.db.WithContext(ctx).Where("status = ?", "listed").Order("id DESC").Find(&listings).Error; err != nil {
		response.Internal(c, "查询失败")
		return
	}
	active := listings[:0]
	for i := range listings {
		h.decorateMarketListing(ctx, &listings[i])
		if listings[i].Expired || listings[i].Instance == nil {
			continue
		}
		active = append(active, listings[i])
	}
	response.OK(c, active)
}

// ListMyMarketListings returns the caller's listings (all statuses), newest
// first, with computed values.
func (h *Handler) ListMyMarketListings(c *gin.Context) {
	ctx := c.Request.Context()
	var listings []model.MarketListing
	if err := h.db.WithContext(ctx).Where("seller_id = ?", auth.UID(c)).Order("id DESC").Find(&listings).Error; err != nil {
		response.Internal(c, "查询失败")
		return
	}
	for i := range listings {
		h.decorateMarketListing(ctx, &listings[i])
	}
	response.OK(c, listings)
}

type createMarketListingReq struct {
	InstanceID uint  `json:"instance_id" binding:"required"`
	PriceCents int64 `json:"price_cents" binding:"required,min=1"`
}

// CreateMarketListing lists one of the caller's unexpired instances for sale.
// The price must be between 1 cent and what the seller originally paid for it.
func (h *Handler) CreateMarketListing(c *gin.Context) {
	var req createMarketListingReq
	if !h.bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	sellerID := auth.UID(c)

	var inst model.Instance
	if err := h.db.WithContext(ctx).First(&inst, req.InstanceID).Error; err != nil {
		response.NotFound(c, "实例不存在")
		return
	}
	if inst.UserID != sellerID && auth.Role(c) != "admin" {
		response.Forbidden(c, "无权操作该实例")
		return
	}
	if inst.ExpiresAt == nil || inst.ExpiresAt.Before(time.Now()) {
		response.BadRequest(c, "实例已过期，无法上架")
		return
	}
	var dup int64
	h.db.WithContext(ctx).Model(&model.MarketListing{}).
		Where("instance_id = ? AND status = ?", inst.ID, "listed").Count(&dup)
	if dup > 0 {
		response.BadRequest(c, "该实例已在出售中")
		return
	}

	// 价格上限 = 该实例的原支付价（剩余时间最多一个计费月，不允许超过原价抬价）。
	capCents := inst.PaidCents
	if capCents <= 0 {
		capCents = inst.PriceCents
	}
	if capCents > 0 && req.PriceCents > capCents {
		response.BadRequest(c, "售价不能超过实例原价")
		return
	}

	listing := model.MarketListing{
		InstanceID: inst.ID,
		SellerID:   sellerID,
		PriceCents: req.PriceCents,
		Status:     "listed",
	}
	if err := h.db.WithContext(ctx).Create(&listing).Error; err != nil {
		response.Internal(c, "上架失败")
		return
	}
	h.decorateMarketListing(ctx, &listing)
	response.Created(c, listing)
}

// CancelMarketListing takes an active listing off the market (seller or
// admin only).
func (h *Handler) CancelMarketListing(c *gin.Context) {
	id, ok := h.parseID(c, "id")
	if !ok {
		return
	}
	ctx := c.Request.Context()
	var listing model.MarketListing
	if err := h.db.WithContext(ctx).First(&listing, id).Error; err != nil {
		response.NotFound(c, "挂单不存在")
		return
	}
	if listing.SellerID != auth.UID(c) && auth.Role(c) != "admin" {
		response.Forbidden(c, "无权操作该挂单")
		return
	}
	if listing.Status != "listed" {
		response.BadRequest(c, "该挂单已结束，无法下架")
		return
	}
	now := time.Now()
	if err := h.db.WithContext(ctx).Model(&listing).Updates(map[string]any{
		"status": "cancelled", "cancelled_at": now,
	}).Error; err != nil {
		response.Internal(c, "下架失败")
		return
	}
	listing.Status = "cancelled"
	listing.CancelledAt = &now
	response.OK(c, listing)
}

// BuyMarketListing purchases an active listing: the buyer's balance is
// deducted, the seller is credited, and the instance ownership (with its
// remaining term, traffic usage, ports and SSH password) transfers to the
// buyer. The instance's PaidCents is set to the sale price so later refunds
// stay proportional to what the buyer actually paid.
func (h *Handler) BuyMarketListing(c *gin.Context) {
	id, ok := h.parseID(c, "id")
	if !ok {
		return
	}
	ctx := c.Request.Context()
	buyerID := auth.UID(c)

	var listing model.MarketListing
	if err := h.db.WithContext(ctx).First(&listing, id).Error; err != nil {
		response.NotFound(c, "挂单不存在")
		return
	}
	if listing.Status != "listed" {
		response.BadRequest(c, "该挂单已被购买或已下架")
		return
	}
	if listing.SellerID == buyerID {
		response.BadRequest(c, "不能购买自己发布的挂单")
		return
	}
	var inst model.Instance
	if err := h.db.WithContext(ctx).First(&inst, listing.InstanceID).Error; err != nil {
		response.BadRequest(c, "实例不存在")
		return
	}
	if inst.ExpiresAt == nil || inst.ExpiresAt.Before(time.Now()) {
		response.BadRequest(c, "实例已过期，无法购买")
		return
	}

	// 预检：余额与实例配额（事务内还会重读一次余额防并发超扣）。
	var buyer model.User
	if err := h.db.WithContext(ctx).First(&buyer, buyerID).Error; err != nil {
		response.BadRequest(c, "用户不存在")
		return
	}
	if buyer.BalanceCents < listing.PriceCents {
		response.BadRequest(c, "余额不足，请先充值")
		return
	}
	var cnt int64
	h.db.WithContext(ctx).Model(&model.Instance{}).Where("user_id = ?", buyerID).Count(&cnt)
	if int(cnt) >= buyer.InstanceQuota {
		response.BadRequest(c, "实例数量已达配额上限")
		return
	}

	err := h.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 先读卖家以取得用户名用于流水描述（二手交易全款给卖家，平台不抽成）。
		var s model.User
		if err := tx.First(&s, listing.SellerID).Error; err != nil {
			return err
		}

		// 原子抢占：仅当仍是 listed 时才可成交，防止并发重复购买。
		res := tx.Model(&model.MarketListing{}).
			Where("id = ? AND status = ?", listing.ID, "listed").
			Updates(map[string]any{"status": "sold", "buyer_id": buyerID, "sold_at": time.Now()})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errors.New("该挂单已被购买或已下架")
		}

		// 扣买家余额。
		var b model.User
		if err := tx.First(&b, buyerID).Error; err != nil {
			return err
		}
		if b.BalanceCents < listing.PriceCents {
			return errors.New("余额不足，请先充值")
		}
		b.BalanceCents -= listing.PriceCents
		if err := tx.Model(&b).Update("balance_cents", b.BalanceCents).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.Transaction{
			UserID:      buyerID,
			Type:        "purchase",
			AmountCents: listing.PriceCents,
			Status:      "success",
			Description: fmt.Sprintf("市场购买实例 %s（卖家 %s）", inst.DisplayName, s.Username),
		}).Error; err != nil {
			return err
		}

		// 入卖家余额。
		s.BalanceCents += listing.PriceCents
		if err := tx.Model(&s).Update("balance_cents", s.BalanceCents).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.Transaction{
			UserID:      listing.SellerID,
			Type:        "sale_income",
			AmountCents: listing.PriceCents,
			Status:      "success",
			Description: fmt.Sprintf("出售实例 %s", inst.DisplayName),
		}).Error; err != nil {
			return err
		}

		// 过户实例：剩余时长/流量/端口/SSH 密码全部保留，PaidCents 记为成交价。
		if err := tx.Model(&model.Instance{}).Where("id = ?", inst.ID).Updates(map[string]any{
			"user_id":    buyerID,
			"paid_cents": listing.PriceCents,
		}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	// 事务已更新挂单（sold/buyer）与实例（paid_cents = 成交价），重新读取后再返回。
	if err := h.db.WithContext(ctx).First(&listing, listing.ID).Error; err != nil {
		response.Internal(c, "查询失败")
		return
	}
	h.decorateMarketListing(ctx, &listing)
	response.OK(c, listing)
}
