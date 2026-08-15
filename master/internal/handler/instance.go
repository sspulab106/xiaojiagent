package handler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"example.com/codetest/master/internal/auth"
	"example.com/codetest/master/internal/model"
	"example.com/codetest/master/internal/response"
	"example.com/codetest/master/internal/service"
)

type createInstanceReq struct {
	PackageID  uint   `json:"package_id" binding:"required"`
	Image      string `json:"image" binding:"required"`
	Name       string `json:"name"`
	CouponCode string `json:"coupon_code"`
}

type instanceActionReq struct {
	Action string `json:"action" binding:"required,oneof=start stop restart rebuild"`
	Image  string `json:"image"` // rebuild 时可选：重装为指定镜像
}

func (h *Handler) ListInstances(c *gin.Context) {
	ctx := c.Request.Context()
	q := h.db.WithContext(ctx).Model(&model.Instance{})
	if auth.Role(c) != "admin" {
		q = q.Where("user_id = ?", auth.UID(c))
	}
	var instances []model.Instance
	if err := q.Order("id DESC").Find(&instances).Error; err != nil {
		response.Internal(c, "查询失败")
		return
	}
	// 节点离线时其上的实例状态无法确认真实状态，统一显示为离线
	// （后台 SyncInstanceStatuses 会周期性校准，这里保证页面即时反馈）。
	nodeStatus := h.nodeStatusMap(ctx)
	for i := range instances {
		if ns, ok := nodeStatus[instances[i].NodeID]; ok && ns != "online" {
			instances[i].Status = "offline"
		}
		h.decorate(ctx, &instances[i])
	}
	response.OK(c, instances)
}

// nodeStatusMap returns node id -> status for all nodes.
func (h *Handler) nodeStatusMap(ctx context.Context) map[uint]string {
	var nodes []model.Node
	h.db.WithContext(ctx).Select("id", "status").Find(&nodes)
	m := make(map[uint]string, len(nodes))
	for _, n := range nodes {
		m[n.ID] = n.Status
	}
	return m
}

func (h *Handler) CreateInstance(c *gin.Context) {
	var req createInstanceReq
	if !h.bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	buyerID := auth.UID(c)

	var pkg model.Package
	if err := h.db.WithContext(ctx).First(&pkg, req.PackageID).Error; err != nil {
		response.BadRequest(c, "套餐不存在")
		return
	}
	if !pkg.Enabled || (pkg.NodeID > 0 && !pkg.Listed) {
		response.BadRequest(c, "套餐未上架或已停用")
		return
	}

	// 余额预检：避免为余额不足的用户白白创建容器。
	var buyer model.User
	if err := h.db.WithContext(ctx).First(&buyer, buyerID).Error; err != nil {
		response.BadRequest(c, "用户不存在")
		return
	}
	if buyer.BalanceCents < int64(pkg.PriceCents) {
		response.BadRequest(c, "余额不足，请先充值")
		return
	}

	inst, err := h.svc.CreateInstance(ctx, service.CreateOptions{
		UserID:     buyerID,
		PackageID:  req.PackageID,
		Image:      req.Image,
		Name:       req.Name,
		CouponCode: req.CouponCode,
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrQuota):
			response.BadRequest(c, "配额不足或订阅已到期")
		case errors.Is(err, service.ErrNoNode):
			response.BadRequest(c, "当前没有在线节点")
		default:
			response.BadRequest(c, err.Error())
		}
		return
	}

	// 付款闭环：优惠码折扣 → 余额扣减 → 机主收益入账 → 流水，单事务原子执行。
	paid, payErr := h.finalizePurchase(ctx, inst, &pkg, req.CouponCode)
	if payErr != nil {
		// 付款失败则回滚刚创建的实例。
		if derr := h.svc.Delete(ctx, inst); derr != nil {
			h.log.Warn("rollback instance after payment failure", "inst", inst.Name, "err", derr)
		}
		response.BadRequest(c, payErr.Error())
		return
	}
	inst.PaidCents = paid
	h.decorate(ctx, inst)
	response.Created(c, inst)
}

// finalizePurchase applies the coupon, deducts the buyer's balance, credits the
// host owner's hosting balance (90% of the paid amount) and writes the
// purchase/host_income transactions — all atomically.
func (h *Handler) finalizePurchase(ctx context.Context, inst *model.Instance, pkg *model.Package, couponCode string) (int64, error) {
	price := int64(pkg.PriceCents)
	paid := price
	var txErr error

	err := h.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 优惠码折扣。
		desc := fmt.Sprintf("购买实例 %s（套餐 %s）", inst.Name, pkg.Name)
		if strings.TrimSpace(couponCode) != "" {
			var coupon model.Coupon
			err := tx.Where("code = ? AND enabled = ? AND (max_uses = 0 OR used_count < max_uses) AND (expires_at IS NULL OR expires_at > ?)",
				strings.TrimSpace(couponCode), true, time.Now()).
				Where("node_id = ? OR node_id = ?", 0, pkg.NodeID).
				First(&coupon).Error
			if err != nil {
				return errors.New("优惠码无效、已过期或不可用于该套餐")
			}
			discount := price * int64(coupon.PercentOff) / 100
			if discount > price {
				discount = price
			}
			paid = price - discount
			desc = fmt.Sprintf("购买实例 %s（套餐 %s，优惠码 %s 减 %d%%）", inst.Name, pkg.Name, coupon.Code, coupon.PercentOff)
			if err := tx.Model(&model.Coupon{}).Where("id = ?", coupon.ID).
				UpdateColumn("used_count", gorm.Expr("used_count + 1")).Error; err != nil {
				return err
			}
		}

		// 余额扣减（事务内重新读取，防并发超扣）。
		var buyer model.User
		if err := tx.First(&buyer, inst.UserID).Error; err != nil {
			return err
		}
		if buyer.BalanceCents < paid {
			return errors.New("余额不足，请先充值")
		}
		buyer.BalanceCents -= paid
		if err := tx.Model(&buyer).Update("balance_cents", buyer.BalanceCents).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.Transaction{
			UserID:      inst.UserID,
			Type:        "purchase",
			AmountCents: paid,
			Status:      "success",
			Description: desc,
		}).Error; err != nil {
			return err
		}
		// 实例记实际支付额。
		if err := tx.Model(&model.Instance{}).Where("id = ?", inst.ID).
			Update("paid_cents", paid).Error; err != nil {
			return err
		}

		// 机主收益：付款的 90% 入托管余额（平台抽 10%）。
		if pkg.NodeID > 0 {
			var node model.Node
			if err := tx.First(&node, pkg.NodeID).Error; err == nil && node.UserID > 0 {
				income := paid * 90 / 100
				if income > paid {
					income = paid
				}
				if income > 0 {
					var host model.User
					if err := tx.First(&host, node.UserID).Error; err == nil {
						host.HostingTotalCents += income
						host.HostingAvailableCents += income
						if err := tx.Model(&host).Updates(map[string]any{
							"hosting_total_cents":     host.HostingTotalCents,
							"hosting_available_cents": host.HostingAvailableCents,
						}).Error; err != nil {
							return err
						}
						if err := tx.Create(&model.Transaction{
							UserID:      host.ID,
							Type:        "host_income",
							AmountCents: income,
							Status:      "success",
							Description: fmt.Sprintf("套餐「%s」销售收益", pkg.Name),
						}).Error; err != nil {
							return err
						}
					}
				}
			}
		}
		return nil
	})
	txErr = err
	return paid, txErr
}

func (h *Handler) GetInstance(c *gin.Context) {
	inst, ok := h.loadInstance(c, true)
	if !ok {
		return
	}
	h.decorate(c.Request.Context(), inst)
	response.OK(c, inst)
}

func (h *Handler) InstanceAction(c *gin.Context) {
	inst, ok := h.loadInstance(c, true)
	if !ok {
		return
	}
	var req instanceActionReq
	if !h.bind(c, &req) {
		return
	}
	updated, err := h.svc.Action(c.Request.Context(), inst, req.Action, req.Image, inst.SSHPassword)
	if err != nil {
		response.BadRequest(c, "操作失败: "+err.Error())
		return
	}
	h.decorate(c.Request.Context(), updated)
	response.OK(c, updated)
}

func (h *Handler) DeleteInstance(c *gin.Context) {
	inst, ok := h.loadInstance(c, true)
	if !ok {
		return
	}
	// 实例在售期间禁止删除：必须先下架。
	var listed int64
	h.db.WithContext(c.Request.Context()).Model(&model.MarketListing{}).
		Where("instance_id = ? AND status = ?", inst.ID, "listed").Count(&listed)
	if listed > 0 {
		response.BadRequest(c, "该实例正在交易市场出售中，请先下架再删除")
		return
	}
	if err := h.svc.Delete(c.Request.Context(), inst); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	// 取消机器退款：默认不退款；仅套餐开启「允许早期全额退款」且开通 ≤1 小时、
	// 已用流量 ≤1GB 的实例返还全部余额（并回冲机主收益）。
	refund := h.cancelRefundCents(c.Request.Context(), inst)
	h.refundInstance(c.Request.Context(), inst, refund)
	response.OK(c, nil)
}

// cancelRefundCents computes the refund for canceling (取消/释放) an instance:
// the package must enable EarlyFullRefund AND the instance must be within one
// hour of creation AND have used no more than 1GB of traffic. Otherwise the
// machine is released with no refund.
func (h *Handler) cancelRefundCents(ctx context.Context, inst *model.Instance) int64 {
	if time.Since(inst.CreatedAt) > time.Hour {
		return 0
	}
	used := inst.TrafficUsedUp + inst.TrafficUsedDown
	if used > 1024*1024*1024 { // 1GB
		return 0
	}
	var pkg model.Package
	if err := h.db.WithContext(ctx).First(&pkg, inst.PackageID).Error; err != nil || !pkg.EarlyFullRefund {
		return 0
	}
	paid := inst.PaidCents
	if paid <= 0 {
		paid = inst.PriceCents
	}
	if paid <= 0 {
		return 0
	}
	return paid
}

// refundInstance credits the buyer's balance, writes the refund transaction and
// reverses the host owner's hosting income proportionally.
func (h *Handler) refundInstance(ctx context.Context, inst *model.Instance, refund int64) {
	if refund <= 0 {
		return
	}
	err := h.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var buyer model.User
		if err := tx.First(&buyer, inst.UserID).Error; err != nil {
			return err
		}
		buyer.BalanceCents += refund
		if err := tx.Model(&buyer).Update("balance_cents", buyer.BalanceCents).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.Transaction{
			UserID:      inst.UserID,
			Type:        "refund",
			AmountCents: refund,
			Status:      "success",
			Description: "删除实例退款 " + inst.Name,
		}).Error; err != nil {
			return err
		}

		// 回冲机主收益（与入账同比例 90%）。
		var node model.Node
		if err := tx.First(&node, inst.NodeID).Error; err == nil && node.UserID > 0 {
			reversal := refund * 90 / 100
			if reversal > 0 {
				var host model.User
				if err := tx.First(&host, node.UserID).Error; err == nil {
					if reversal > host.HostingAvailableCents {
						reversal = host.HostingAvailableCents
					}
					if reversal > host.HostingTotalCents {
						reversal = host.HostingTotalCents
					}
					if reversal > 0 {
						host.HostingAvailableCents -= reversal
						host.HostingTotalCents -= reversal
						if err := tx.Model(&host).Updates(map[string]any{
							"hosting_total_cents":     host.HostingTotalCents,
							"hosting_available_cents": host.HostingAvailableCents,
						}).Error; err != nil {
							return err
						}
						if err := tx.Create(&model.Transaction{
							UserID:      host.ID,
							Type:        "host_refund",
							AmountCents: reversal,
							Status:      "success",
							Description: "实例退款回冲 " + inst.Name,
						}).Error; err != nil {
							return err
						}
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		h.log.Warn("refund failed", "inst", inst.Name, "err", err)
	}
}

func (h *Handler) InstanceStats(c *gin.Context) {
	inst, ok := h.loadInstance(c, true)
	if !ok {
		return
	}
	st, err := h.svc.Stats(c.Request.Context(), inst)
	if err != nil {
		response.BadRequest(c, "获取统计失败: "+err.Error())
		return
	}
	response.OK(c, gin.H{
		"status":                  st.Status,
		"cpu_percent":             st.CPUPercent,
		"memory_used_mb":          st.MemoryUsedMB,
		"memory_limit_mb":         st.MemoryLimitMB,
		"rx_bytes":                st.RxBytes,
		"tx_bytes":                st.TxBytes,
		"traffic_used_up_bytes":   inst.TrafficUsedUp,
		"traffic_used_down_bytes": inst.TrafficUsedDown,
	})
}
