package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"example.com/codetest/master/internal/agentclient"
	"example.com/codetest/master/internal/model"
)

// CreateOptions describes a new instance request.
type CreateOptions struct {
	UserID     uint
	PackageID  uint
	Image      string
	Name       string // optional; a random name is generated when empty
	CouponCode string // optional purchase discount code
}

// CreateInstance provisions a container on the package's node (or the
// least-loaded platform node for platform packages) and records it in the
// database together with the auto-allocated SSH port mapping. Resource limits
// are fixed by the package — buyers cannot override memory/disk/swap.
func (s *Service) CreateInstance(ctx context.Context, opts CreateOptions) (*model.Instance, error) {
	var user model.User
	if err := s.db.WithContext(ctx).First(&user, opts.UserID).Error; err != nil {
		return nil, err
	}
	if user.ExpiresAt != nil && user.ExpiresAt.Before(time.Now()) {
		return nil, ErrQuota
	}
	var cnt int64
	s.db.WithContext(ctx).Model(&model.Instance{}).Where("user_id = ?", opts.UserID).Count(&cnt)
	if int(cnt) >= user.InstanceQuota {
		return nil, ErrQuota
	}

	var pkg model.Package
	if err := s.db.WithContext(ctx).First(&pkg, opts.PackageID).Error; err != nil {
		return nil, errors.New("套餐不存在")
	}
	if !pkg.Enabled {
		return nil, errors.New("套餐已停用")
	}
	// 机主套餐必须已上架才可购买。
	if pkg.NodeID > 0 && !pkg.Listed {
		return nil, errors.New("套餐未上架，暂不可购买")
	}
	// 套餐限定镜像：请求镜像必须在其可选集合内。
	if pkg.Images != "" {
		if !imageAllowed(pkg.Images, opts.Image) {
			return nil, errors.New("镜像不在该套餐可选范围内")
		}
	}

	// 目标节点：机主套餐固定在所属节点；平台套餐取最空闲在线节点。
	var node *model.Node
	if pkg.NodeID > 0 {
		n, err := s.nodeByID(ctx, pkg.NodeID)
		if err != nil {
			return nil, errors.New("套餐所属节点不存在")
		}
		if n.Status != "online" {
			return nil, errors.New("套餐所属节点离线，暂时无法购买")
		}
		node = n
	} else {
		n, err := s.PickNode(ctx)
		if err != nil {
			return nil, err
		}
		node = n
	}

	// Container names must match [a-zA-Z0-9][a-zA-Z0-9_.-]* on podman/docker,
	// so a user-supplied label (which may contain CJK) is sanitized: ASCII-only
	// labels are kept, anything else falls back to an auto-generated name. The
	// original label is stored as DisplayName for the panel.
	displayName := opts.Name
	name := sanitizeContainerName(opts.Name)
	if name == "" {
		name = fmt.Sprintf("u%d-%s", opts.UserID, randHex(6))
	}
	var dup int64
	s.db.WithContext(ctx).Model(&model.Instance{}).Where("name = ?", name).Count(&dup)
	if dup > 0 {
		if opts.Name != "" {
			name = fmt.Sprintf("%s-%s", name, randHex(3))
		} else {
			name = fmt.Sprintf("u%d-%s", opts.UserID, randHex(6))
		}
	}

	memMB := pkg.MemoryMB
	diskMB := pkg.DiskMB
	swapMB := memMB // swap mirrors memory

	client := s.ClientForNode(node)
	created, err := client.CreateInstance(ctx, agentclient.CreateInstanceReq{
		Name:     name,
		Image:    opts.Image,
		CpuCores: pkg.CpuCores,
		MemoryMB: memMB,
		DiskMB:   diskMB,
		SwapMB:   swapMB,
		IPv6:     pkg.IPv6,
	})
	if err != nil {
		return nil, fmt.Errorf("节点创建失败: %w", err)
	}

	now := time.Now()
	expires := now.AddDate(0, 1, 0)
	inst := &model.Instance{
		Name:         created.Name,
		DisplayName:  displayName,
		UserID:       opts.UserID,
		NodeID:       node.ID,
		PackageID:    pkg.ID,
		Image:        opts.Image,
		Status:       created.Status,
		IP:           created.IP,
		CpuCores:     pkg.CpuCores,
		MemoryMB:     memMB,
		DiskMB:       diskMB,
		SwapMB:       swapMB,
		SSHPassword:  created.RootPassword,
		TrafficGB:    pkg.TrafficGB,
		PortSlots:    pkg.PortSlots,
		PriceCents:   int64(pkg.PriceCents),
		PaidCents:    int64(pkg.PriceCents),
		AutoRenew:    true,
		ExpiresAt:    &expires,
		TrafficMonth: now.Format("2006-01"),
	}
	if err := s.db.WithContext(ctx).Create(inst).Error; err != nil {
		_ = client.DeleteInstance(ctx, created.Name) // best-effort rollback
		return nil, err
	}

	if created.SSHPort > 0 {
		mapping := &model.PortMapping{
			InstanceID:    inst.ID,
			AgentRuleID:   created.SSHRuleID,
			HostPort:      created.SSHPort,
			ContainerIP:   created.IP,
			ContainerPort: 22,
			Protocol:      "tcp",
		}
		if err := s.db.WithContext(ctx).Create(mapping).Error; err != nil {
			s.log.Warn("failed to persist ssh mapping", "err", err)
		}
	}

	return inst, nil
}

// imageAllowed reports whether ref is one of the package's comma-separated
// allowed image refs.
func imageAllowed(allowed, ref string) bool {
	for _, a := range strings.Split(allowed, ",") {
		if strings.TrimSpace(a) != "" && strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(ref)) {
			return true
		}
	}
	return false
}

// InstanceByID loads an instance by primary key.
func (s *Service) InstanceByID(ctx context.Context, id uint) (*model.Instance, error) {
	var inst model.Instance
	if err := s.db.WithContext(ctx).First(&inst, id).Error; err != nil {
		return nil, err
	}
	return &inst, nil
}

// Action forwards a lifecycle action (start/stop/restart/rebuild) to the
// agent and updates the local status. For rebuild an optional new image is
// forwarded and persisted; the SSH password is passed so the agent can
// re-provision sshd on the fresh image.
func (s *Service) Action(ctx context.Context, inst *model.Instance, action, image, password string) (*model.Instance, error) {
	node, err := s.nodeByID(ctx, inst.NodeID)
	if err != nil {
		return nil, err
	}
	client := s.ClientForNode(node)
	info, err := client.InstanceAction(ctx, inst.Name, action, image, password)
	if err != nil {
		return nil, err
	}
	inst.Status = info.Status
	if info.IP != "" {
		inst.IP = info.IP
	}
	updates := map[string]any{"status": inst.Status, "ip": inst.IP}
	if action == "rebuild" && image != "" {
		inst.Image = image
		updates["image"] = image
	}
	if err := s.db.WithContext(ctx).Model(inst).Updates(updates).Error; err != nil {
		return nil, err
	}
	return inst, nil
}

// Delete removes the instance on the agent and deletes it locally together
// with its NAT port mappings.
func (s *Service) Delete(ctx context.Context, inst *model.Instance) error {
	node, err := s.nodeByID(ctx, inst.NodeID)
	if err != nil {
		return err
	}
	client := s.ClientForNode(node)
	if err := client.DeleteInstance(ctx, inst.Name); err != nil {
		return fmt.Errorf("节点删除失败: %w", err)
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("instance_id = ?", inst.ID).Delete(&model.PortMapping{}).Error; err != nil {
			return err
		}
		return tx.Delete(inst).Error
	})
}

// RefundCents computes the pro-rata refund for an instance based on the time
// remaining before ExpiresAt (clamped to one month). paid is the actual amount
// charged (PaidCents), falling back to the snapshotted package price.
func (s *Service) RefundCents(inst *model.Instance) int64 {
	paid := inst.PaidCents
	if paid <= 0 {
		paid = inst.PriceCents
	}
	if paid <= 0 {
		return 0
	}
	const month = 30 * 24 * time.Hour
	now := time.Now()
	expiry := now.Add(month) // default: full month remaining
	if inst.ExpiresAt != nil {
		expiry = *inst.ExpiresAt
	}
	remaining := expiry.Sub(now)
	if remaining <= 0 {
		return 0
	}
	if remaining > month {
		remaining = month
	}
	refund := paid * int64(remaining) / int64(month)
	if refund > paid {
		refund = paid
	}
	if refund < 0 {
		return 0
	}
	return refund
}

// Stats fetches live stats from the agent and folds the cumulative network
// counters into the monthly traffic allowance. Counter baselines make this
// safe across agent/container restarts and month boundaries.
func (s *Service) Stats(ctx context.Context, inst *model.Instance) (*agentclient.StatsResp, error) {
	node, err := s.nodeByID(ctx, inst.NodeID)
	if err != nil {
		return nil, err
	}
	st, err := s.ClientForNode(node).Stats(ctx, inst.Name)
	if err != nil {
		return nil, err
	}

	month := time.Now().Format("2006-01")
	if inst.TrafficMonth != month {
		inst.TrafficMonth = month
		inst.TrafficUsedUp = 0
		inst.TrafficUsedDown = 0
	}
	if inst.LastRxBytes > 0 && int64(st.RxBytes) >= inst.LastRxBytes {
		inst.TrafficUsedUp += int64(st.RxBytes) - inst.LastRxBytes
	}
	if inst.LastTxBytes > 0 && int64(st.TxBytes) >= inst.LastTxBytes {
		inst.TrafficUsedDown += int64(st.TxBytes) - inst.LastTxBytes
	}
	inst.LastRxBytes = int64(st.RxBytes)
	inst.LastTxBytes = int64(st.TxBytes)
	inst.Status = st.Status

	s.db.WithContext(ctx).Model(inst).Updates(map[string]any{
		"traffic_month":     inst.TrafficMonth,
		"traffic_used_up":   inst.TrafficUsedUp,
		"traffic_used_down": inst.TrafficUsedDown,
		"last_rx_bytes":     inst.LastRxBytes,
		"last_tx_bytes":     inst.LastTxBytes,
		"status":            inst.Status,
	})
	return st, nil
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// SyncInstanceStatuses reconciles the locally cached instance status against
// what each online node's agent actually reports. Container statuses change
// outside the master's control (host reboot, engine restart, container crash),
// so without this loop the instance list would keep showing a stale
// "running" forever. Best-effort: unreachable nodes are skipped.
func (s *Service) SyncInstanceStatuses(ctx context.Context) {
	var nodes []model.Node
	if err := s.db.WithContext(ctx).Where("status = ?", "online").Find(&nodes).Error; err != nil {
		return
	}
	for i := range nodes {
		n := &nodes[i]
		client := s.ClientForNode(n)
		syncCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		infos, err := client.ListInstances(syncCtx)
		cancel()
		if err != nil {
			continue
		}
		statusByName := make(map[string]string, len(infos))
		ipByName := make(map[string]string, len(infos))
		for _, info := range infos {
			statusByName[info.Name] = info.Status
			if info.IP != "" {
				ipByName[info.Name] = info.IP
			}
		}
		// 批量刷新该节点下所有实例的状态与 IP（含节点上 agent 已不认识的实例 → offline）。
		var insts []model.Instance
		s.db.WithContext(ctx).Where("node_id = ?", n.ID).Find(&insts)
		for _, inst := range insts {
			status, known := statusByName[inst.Name]
			if !known {
				// 容器被外部删除/整机重启后未恢复：标记 offline，避免一直显示运行中。
				if inst.Status != "offline" {
					s.db.Model(&inst).Update("status", "offline")
				}
				continue
			}
			ip, ok := ipByName[inst.Name]
			if !ok || ip == "" {
				ip = inst.IP
			}
			if status != inst.Status || ip != inst.IP {
				s.db.Model(&inst).Updates(map[string]any{"status": status, "ip": ip})
			}
		}
	}
}

// sanitizeContainerName reduces a user label to the safe container-name
// charset [a-zA-Z0-9_.-]. Returns "" when the label contains characters
// outside it (e.g. CJK), so callers fall back to an auto-generated name
// instead of producing a mangled one like "2".
func sanitizeContainerName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			return "" // non-ASCII or invalid char present
		}
	}
	return b.String()
}
