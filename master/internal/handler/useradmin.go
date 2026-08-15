package handler

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"example.com/codetest/master/internal/model"
	"example.com/codetest/master/internal/response"
)

// AdminUserDetail returns a user with their hosted nodes (宿主机) and current
// instances (实例) so admins can inspect a single account.
func (h *Handler) AdminUserDetail(c *gin.Context) {
	id, ok := h.parseID(c, "id")
	if !ok {
		return
	}
	ctx := c.Request.Context()
	user, err := h.userByID(c, id)
	if err != nil {
		response.NotFound(c, "用户不存在")
		return
	}

	var nodes []model.Node
	if err := h.db.WithContext(ctx).Where("user_id = ?", id).Order("id ASC").Find(&nodes).Error; err != nil {
		response.Internal(c, "查询宿主机失败")
		return
	}
	var instances []model.Instance
	if err := h.db.WithContext(ctx).Where("user_id = ?", id).Order("id DESC").Find(&instances).Error; err != nil {
		response.Internal(c, "查询实例失败")
		return
	}
	for i := range instances {
		h.decorate(ctx, &instances[i])
	}
	response.OK(c, gin.H{
		"user":      user,
		"nodes":     nodes,
		"instances": instances,
	})
}

type banUserReq struct {
	Banned bool `json:"banned"` // true=封禁 false=解封
}

// AdminBanUser bans or unbans a user. Banned users cannot log in and all of
// their API requests are rejected (see auth middleware).
func (h *Handler) AdminBanUser(c *gin.Context) {
	id, ok := h.parseID(c, "id")
	if !ok {
		return
	}
	var req banUserReq
	if !h.bind(c, &req) {
		return
	}
	user, err := h.userByID(c, id)
	if err != nil {
		response.NotFound(c, "用户不存在")
		return
	}
	if err := h.db.WithContext(c.Request.Context()).Model(user).Update("banned", req.Banned).Error; err != nil {
		response.Internal(c, "更新失败")
		return
	}
	user.Banned = req.Banned
	response.OK(c, user)
}

type resetPasswordReq struct {
	Password string `json:"password" binding:"required,min=6,max=72"`
}

// AdminResetPassword sets a new login password for a user (管理员重置登录密码).
func (h *Handler) AdminResetPassword(c *gin.Context) {
	id, ok := h.parseID(c, "id")
	if !ok {
		return
	}
	var req resetPasswordReq
	if !h.bind(c, &req) {
		return
	}
	user, err := h.userByID(c, id)
	if err != nil {
		response.NotFound(c, "用户不存在")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		response.Internal(c, "密码加密失败")
		return
	}
	if err := h.db.WithContext(c.Request.Context()).Model(user).Update("password_hash", string(hash)).Error; err != nil {
		response.Internal(c, "更新失败")
		return
	}
	response.OK(c, nil)
}

type adjustBalanceReq struct {
	DeltaCents int64  `json:"delta_cents" binding:"required"` // 正数加钱，负数扣钱
	Remark     string `json:"remark"`                         // 备注（写入交易记录）
}

// AdminAdjustBalance adds or deducts a user's available balance and writes a
// transaction record (管理员调整余额).
func (h *Handler) AdminAdjustBalance(c *gin.Context) {
	id, ok := h.parseID(c, "id")
	if !ok {
		return
	}
	var req adjustBalanceReq
	if !h.bind(c, &req) {
		return
	}
	if req.DeltaCents == 0 {
		response.BadRequest(c, "调整金额不能为 0")
		return
	}
	ctx := c.Request.Context()
	user, err := h.userByID(c, id)
	if err != nil {
		response.NotFound(c, "用户不存在")
		return
	}
	if user.BalanceCents+req.DeltaCents < 0 {
		response.BadRequest(c, "调整后余额不能为负数")
		return
	}
	remark := req.Remark
	if remark == "" {
		remark = "管理员调整余额"
	}
	abs := req.DeltaCents
	if abs < 0 {
		abs = -abs
	}
	sign := "+"
	if req.DeltaCents < 0 {
		sign = "-"
	}

	// 与充值一致：10% 入托管余额；扣减时保证可用/总额不为负且总额 >= 可用。
	// 余额更新与流水写入放在同一事务内，避免余额变了却没有记录。
	txErr := h.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var cur model.User
		if err := tx.First(&cur, user.ID).Error; err != nil {
			return err
		}
		if cur.BalanceCents+req.DeltaCents < 0 {
			return errBalanceNegative
		}
		hostingDelta := req.DeltaCents / 10
		newBalance := cur.BalanceCents + req.DeltaCents
		newHostingTotal := cur.HostingTotalCents + hostingDelta
		newHostingAvail := cur.HostingAvailableCents + hostingDelta
		if newHostingAvail < 0 {
			newHostingAvail = 0
		}
		if newHostingTotal < 0 {
			newHostingTotal = 0
		}
		if newHostingTotal < newHostingAvail {
			newHostingTotal = newHostingAvail
		}
		if err := tx.Model(&cur).Updates(map[string]any{
			"balance_cents":           newBalance,
			"hosting_total_cents":     newHostingTotal,
			"hosting_available_cents": newHostingAvail,
		}).Error; err != nil {
			return err
		}
		return tx.Create(&model.Transaction{
			UserID:      user.ID,
			Type:        "admin_adjust",
			AmountCents: abs,
			Status:      "success",
			Description: fmt.Sprintf("%s（%s $%.2f）", remark, sign, float64(abs)/100),
		}).Error
	})
	if txErr == errBalanceNegative {
		response.BadRequest(c, "调整后余额不能为负数")
		return
	}
	if txErr != nil {
		response.Internal(c, "更新失败")
		return
	}
	// 回写内存对象用于响应。
	user.BalanceCents += req.DeltaCents
	user.HostingTotalCents += req.DeltaCents / 10
	user.HostingAvailableCents += req.DeltaCents / 10
	if user.HostingAvailableCents < 0 {
		user.HostingAvailableCents = 0
	}
	if user.HostingTotalCents < user.HostingAvailableCents {
		user.HostingTotalCents = user.HostingAvailableCents
	}
	response.OK(c, user)
}

var errBalanceNegative = errCard("余额不能为负")
