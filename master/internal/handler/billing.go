package handler

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"example.com/codetest/master/internal/auth"
	"example.com/codetest/master/internal/model"
	"example.com/codetest/master/internal/response"
)

// ListTransactions returns the current user's billing history (交易记录),
// newest first.
func (h *Handler) ListTransactions(c *gin.Context) {
	var txs []model.Transaction
	if err := h.db.WithContext(c.Request.Context()).
		Where("user_id = ?", auth.UID(c)).
		Order("created_at DESC").
		Find(&txs).Error; err != nil {
		response.Internal(c, "查询失败")
		return
	}
	response.OK(c, txs)
}

// ListCoupons returns coupons the current user can still use: enabled, with
// remaining uses and not expired.
func (h *Handler) ListCoupons(c *gin.Context) {
	var coupons []model.Coupon
	if err := h.db.WithContext(c.Request.Context()).
		Where("enabled = ? AND (max_uses = 0 OR used_count < max_uses) AND (expires_at IS NULL OR expires_at > ?)", true, time.Now()).
		Order("id ASC").
		Find(&coupons).Error; err != nil {
		response.Internal(c, "查询失败")
		return
	}
	response.OK(c, coupons)
}

// ListCouponsAdmin returns all coupons (admin).
func (h *Handler) ListCouponsAdmin(c *gin.Context) {
	var coupons []model.Coupon
	if err := h.db.WithContext(c.Request.Context()).
		Order("id ASC").
		Find(&coupons).Error; err != nil {
		response.Internal(c, "查询失败")
		return
	}
	response.OK(c, coupons)
}

type addCouponReq struct {
	Code       string     `json:"code" binding:"required,min=1,max=32"`
	PercentOff int        `json:"percent_off" binding:"min=0,max=100"`
	MaxUses    int        `json:"max_uses"`
	Enabled    bool       `json:"enabled"`
	ExpiresAt  *time.Time `json:"expires_at"` // nil = 永不过期
}

// AddCoupon creates a new coupon (admin).
func (h *Handler) AddCoupon(c *gin.Context) {
	var req addCouponReq
	if !h.bind(c, &req) {
		return
	}
	coupon := model.Coupon{
		Code:       strings.ToUpper(strings.TrimSpace(req.Code)),
		PercentOff: req.PercentOff,
		MaxUses:    req.MaxUses,
		Enabled:    req.Enabled,
		ExpiresAt:  req.ExpiresAt,
	}
	if err := h.db.WithContext(c.Request.Context()).Create(&coupon).Error; err != nil {
		response.BadRequest(c, "优惠码已存在")
		return
	}
	response.Created(c, coupon)
}

type updateCouponReq struct {
	PercentOff  *int       `json:"percent_off"`
	MaxUses     *int       `json:"max_uses"`
	Enabled     *bool      `json:"enabled"`
	ExpiresAt   *time.Time `json:"expires_at"`   // 设置/延长到期时间
	ClearExpiry *bool      `json:"clear_expiry"` // true 时清除到期时间
}

// updateCoupon applies an edit to a coupon. The caller must have already
// verified ownership/role. Supports: 修改折扣、增加次数、启停、设置/延长/清除到期时间.
func (h *Handler) updateCoupon(c *gin.Context, coupon *model.Coupon) {
	var req updateCouponReq
	if !h.bind(c, &req) {
		return
	}
	updates := map[string]any{}
	if req.PercentOff != nil {
		if *req.PercentOff < 0 || *req.PercentOff > 100 {
			response.BadRequest(c, "折扣比例需在 0-100 之间")
			return
		}
		updates["percent_off"] = *req.PercentOff
	}
	if req.MaxUses != nil {
		if *req.MaxUses < 0 {
			response.BadRequest(c, "使用次数不能为负数")
			return
		}
		if *req.MaxUses < coupon.UsedCount {
			response.BadRequest(c, "新的使用上限不能小于已使用次数")
			return
		}
		updates["max_uses"] = *req.MaxUses
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}
	if req.ClearExpiry != nil && *req.ClearExpiry {
		updates["expires_at"] = nil
	} else if req.ExpiresAt != nil {
		updates["expires_at"] = *req.ExpiresAt
	}
	if len(updates) == 0 {
		response.OK(c, coupon)
		return
	}
	if err := h.db.WithContext(c.Request.Context()).Model(coupon).Updates(updates).Error; err != nil {
		response.Internal(c, "更新失败")
		return
	}
	// 回写内存对象，确保响应返回更新后的值。
	if req.PercentOff != nil {
		coupon.PercentOff = *req.PercentOff
	}
	if req.MaxUses != nil {
		coupon.MaxUses = *req.MaxUses
	}
	if req.Enabled != nil {
		coupon.Enabled = *req.Enabled
	}
	if req.ClearExpiry != nil && *req.ClearExpiry {
		coupon.ExpiresAt = nil
	} else if req.ExpiresAt != nil {
		coupon.ExpiresAt = req.ExpiresAt
	}
	response.OK(c, coupon)
}

// AdminUpdateCoupon edits any coupon (admin).
func (h *Handler) AdminUpdateCoupon(c *gin.Context) {
	id, ok := h.parseID(c, "id")
	if !ok {
		return
	}
	var coupon model.Coupon
	if err := h.db.WithContext(c.Request.Context()).First(&coupon, id).Error; err != nil {
		response.NotFound(c, "优惠码不存在")
		return
	}
	h.updateCoupon(c, &coupon)
}

// DeleteCoupon removes a coupon (admin).
func (h *Handler) DeleteCoupon(c *gin.Context) {
	id, ok := h.parseID(c, "id")
	if !ok {
		return
	}
	if err := h.db.WithContext(c.Request.Context()).Delete(&model.Coupon{}, id).Error; err != nil {
		response.Internal(c, "删除失败")
		return
	}
	response.OK(c, nil)
}

// UpdateCoupon edits a node coupon (owner/admin).
func (h *Handler) UpdateCoupon(c *gin.Context) {
	id, ok := h.parseID(c, "id")
	if !ok {
		return
	}
	var coupon model.Coupon
	if err := h.db.WithContext(c.Request.Context()).First(&coupon, id).Error; err != nil {
		response.NotFound(c, "优惠码不存在")
		return
	}
	if auth.Role(c) != "admin" && (coupon.UserID == 0 || coupon.UserID != auth.UID(c)) {
		response.Forbidden(c, "无权操作该优惠码")
		return
	}
	h.updateCoupon(c, &coupon)
}
