package handler

import (
	"crypto/rand"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"example.com/codetest/master/internal/auth"
	"example.com/codetest/master/internal/model"
	"example.com/codetest/master/internal/response"
)

const giftCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O/1/I

func randomGiftCode() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	chars := make([]byte, len(b))
	for i, c := range b {
		chars[i] = giftCodeAlphabet[int(c)%len(giftCodeAlphabet)]
	}
	return "GIFT-" + string(chars[:4]) + "-" + string(chars[4:])
}

// AdminListGiftCards returns all gift cards (admin).
func (h *Handler) AdminListGiftCards(c *gin.Context) {
	var cards []model.GiftCard
	if err := h.db.WithContext(c.Request.Context()).Order("id DESC").Find(&cards).Error; err != nil {
		response.Internal(c, "查询失败")
		return
	}
	response.OK(c, cards)
}

type createGiftCardReq struct {
	Code        string     `json:"code"` // empty -> auto-generate
	AmountCents int64      `json:"amount_cents" binding:"required,min=1,max=100000000"`
	Count       int        `json:"count"`      // batch, default 1
	ExpiresAt   *time.Time `json:"expires_at"` // 可选的到期时间，nil = 永不过期
}

// AdminCreateGiftCards issues one or more redemption codes (admin).
func (h *Handler) AdminCreateGiftCards(c *gin.Context) {
	var req createGiftCardReq
	if !h.bind(c, &req) {
		return
	}
	count := req.Count
	if count <= 0 {
		count = 1
	}
	if count > 100 {
		count = 100
	}
	var created []model.GiftCard
	for i := 0; i < count; i++ {
		code := strings.ToUpper(strings.TrimSpace(req.Code))
		if code == "" || count > 1 {
			code = randomGiftCode()
		}
		card := model.GiftCard{
			Code:        code,
			AmountCents: req.AmountCents,
			Status:      "issued",
			CreatedBy:   auth.UID(c),
			ExpiresAt:   req.ExpiresAt,
		}
		if err := h.db.WithContext(c.Request.Context()).Create(&card).Error; err != nil {
			if count == 1 && code != "" {
				response.BadRequest(c, "兑换码已存在")
				return
			}
			continue // 批量生成时跳过重复码
		}
		created = append(created, card)
	}
	response.Created(c, created)
}

type updateGiftCardReq struct {
	AmountCents *int64     `json:"amount_cents"` // 修改额度（仅未兑换）
	ExpiresAt   *time.Time `json:"expires_at"`   // 设置/延长到期时间
	ClearExpiry *bool      `json:"clear_expiry"` // true 时清除到期时间
}

// AdminUpdateGiftCard edits an unredeemed gift card: 修改面额、设置/延长到期时间.
func (h *Handler) AdminUpdateGiftCard(c *gin.Context) {
	id, ok := h.parseID(c, "id")
	if !ok {
		return
	}
	var card model.GiftCard
	if err := h.db.WithContext(c.Request.Context()).First(&card, id).Error; err != nil {
		response.NotFound(c, "兑换码不存在")
		return
	}
	if card.Status == "redeemed" {
		response.BadRequest(c, "已兑换的兑换码不能修改")
		return
	}
	var req updateGiftCardReq
	if !h.bind(c, &req) {
		return
	}
	updates := map[string]any{}
	if req.AmountCents != nil {
		if *req.AmountCents < 1 {
			response.BadRequest(c, "面额至少为 1 分")
			return
		}
		updates["amount_cents"] = *req.AmountCents
	}
	if req.ClearExpiry != nil && *req.ClearExpiry {
		updates["expires_at"] = nil
	} else if req.ExpiresAt != nil {
		updates["expires_at"] = *req.ExpiresAt
	}
	if len(updates) == 0 {
		response.OK(c, card)
		return
	}
	if err := h.db.WithContext(c.Request.Context()).Model(&card).Updates(updates).Error; err != nil {
		response.Internal(c, "更新失败")
		return
	}
	if req.AmountCents != nil {
		card.AmountCents = *req.AmountCents
	}
	if req.ClearExpiry != nil && *req.ClearExpiry {
		card.ExpiresAt = nil
	} else if req.ExpiresAt != nil {
		card.ExpiresAt = req.ExpiresAt
	}
	response.OK(c, card)
}

// AdminDeleteGiftCard removes an unredeemed gift card (admin).
func (h *Handler) AdminDeleteGiftCard(c *gin.Context) {
	id, ok := h.parseID(c, "id")
	if !ok {
		return
	}
	var card model.GiftCard
	if err := h.db.WithContext(c.Request.Context()).First(&card, id).Error; err != nil {
		response.NotFound(c, "兑换码不存在")
		return
	}
	if card.Status == "redeemed" {
		response.BadRequest(c, "已兑换的兑换码不能删除")
		return
	}
	if err := h.db.WithContext(c.Request.Context()).Delete(&model.GiftCard{}, card.ID).Error; err != nil {
		response.Internal(c, "删除失败")
		return
	}
	response.OK(c, nil)
}

type redeemReq struct {
	Code string `json:"code" binding:"required,min=4,max=32"`
}

// RedeemGiftCard credits the current user's balance from an issued gift card.
func (h *Handler) RedeemGiftCard(c *gin.Context) {
	var req redeemReq
	if !h.bind(c, &req) {
		return
	}
	user, ok := h.currentUser(c)
	if !ok {
		return
	}
	code := strings.ToUpper(strings.TrimSpace(req.Code))
	now := time.Now()

	err := h.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var card model.GiftCard
		if err := tx.Where("code = ?", code).First(&card).Error; err != nil {
			return errCardNotFound
		}
		if card.Status == "redeemed" {
			return errCardUsed
		}
		if card.ExpiresAt != nil && card.ExpiresAt.Before(now) {
			return errCardExpired
		}
		// 标记兑换。
		card.Status = "redeemed"
		card.RedeemedBy = user.ID
		card.RedeemedAt = &now
		if err := tx.Save(&card).Error; err != nil {
			return err
		}
		// 入账：与充值一致，10% 进托管余额。
		hosting := card.AmountCents / 10
		user.BalanceCents += card.AmountCents
		user.HostingTotalCents += hosting
		user.HostingAvailableCents += hosting
		if err := tx.Model(user).Updates(map[string]any{
			"balance_cents":           user.BalanceCents,
			"hosting_total_cents":     user.HostingTotalCents,
			"hosting_available_cents": user.HostingAvailableCents,
		}).Error; err != nil {
			return err
		}
		return tx.Create(&model.Transaction{
			UserID:      user.ID,
			Type:        "recharge",
			AmountCents: card.AmountCents,
			Status:      "success",
			Description: "兑换码充值 " + card.Code,
		}).Error
	})
	switch {
	case err == errCardNotFound:
		response.BadRequest(c, "兑换码不存在")
	case err == errCardUsed:
		response.BadRequest(c, "该兑换码已被使用")
	case err == errCardExpired:
		response.BadRequest(c, "该兑换码已过期")
	case err != nil:
		response.Internal(c, "兑换失败")
	default:
		response.OK(c, user)
	}
}

var (
	errCardNotFound = errCard("兑换码不存在")
	errCardUsed     = errCard("已使用")
	errCardExpired  = errCard("已过期")
)

type errCard string

func (e errCard) Error() string { return string(e) }
