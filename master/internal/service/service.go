package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"example.com/codetest/master/internal/agentclient"
	"example.com/codetest/master/internal/config"
	"example.com/codetest/master/internal/geo"
	"example.com/codetest/master/internal/model"
)

// ErrQuota is returned when a user hits an instance or subscription limit.
var ErrQuota = errors.New("配额不足或订阅已到期")

// ErrNoNode is returned when no node is online.
var ErrNoNode = errors.New("当前没有可用节点")

// Service bundles the business logic that touches both the database and the
// agent fleet. Handlers stay thin and delegate here.
type Service struct {
	db  *gorm.DB
	cfg config.Config
	log *slog.Logger
}

func New(db *gorm.DB, cfg config.Config, log *slog.Logger) *Service {
	return &Service{db: db, cfg: cfg, log: log}
}

// ClientForNode builds an agent client for the given node.
func (s *Service) ClientForNode(node *model.Node) *agentclient.Client {
	return agentclient.New(node.AgentAddr, node.Token)
}

// Setting returns one platform setting value (empty when unset).
func (s *Service) Setting(ctx context.Context, key string) string {
	var st model.Setting
	if err := s.db.WithContext(ctx).Where("key = ?", key).First(&st).Error; err != nil {
		return ""
	}
	return st.Value
}

// SetSetting upserts one platform setting.
func (s *Service) SetSetting(ctx context.Context, key, value string) error {
	st := model.Setting{Key: key, Value: value}
	return s.db.WithContext(ctx).Save(&st).Error
}

// PickNode returns the online node currently hosting the fewest instances.
func (s *Service) PickNode(ctx context.Context) (*model.Node, error) {
	var nodes []model.Node
	if err := s.db.WithContext(ctx).Where("status = ?", "online").Find(&nodes).Error; err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, ErrNoNode
	}
	best := nodes[0]
	bestCount := int64(-1)
	for _, n := range nodes {
		var cnt int64
		s.db.WithContext(ctx).Model(&model.Instance{}).Where("node_id = ?", n.ID).Count(&cnt)
		if bestCount < 0 || cnt < bestCount {
			best = n
			bestCount = cnt
		}
	}
	return &best, nil
}

func (s *Service) nodeByID(ctx context.Context, id uint) (*model.Node, error) {
	var n model.Node
	if err := s.db.WithContext(ctx).First(&n, id).Error; err != nil {
		return nil, err
	}
	return &n, nil
}

// NodeByID is the exported form of nodeByID, used by handlers.
func (s *Service) NodeByID(ctx context.Context, id uint) (*model.Node, error) {
	return s.nodeByID(ctx, id)
}

// NodeHealthLoop pings every node periodically to keep status/totals fresh,
// and reconciles cached instance statuses with what the agents actually
// report (so a container that went down after a host reboot no longer shows
// as "running" forever).
func (s *Service) NodeHealthLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		ctx := context.Background()
		s.refreshNodes(ctx)
		s.SyncInstanceStatuses(ctx)
	}
}

func (s *Service) refreshNodes(ctx context.Context) {
	var nodes []model.Node
	if err := s.db.WithContext(ctx).Find(&nodes).Error; err != nil {
		return
	}
	for i := range nodes {
		n := &nodes[i]
		client := s.ClientForNode(n)
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		h, err := client.Health(probeCtx)
		cancel()
		if err != nil {
			if n.Status != "offline" {
				s.db.Model(n).Update("status", "offline")
			}
			continue
		}
		updates := map[string]any{
			"status":          "online",
			"host_ip":         h.HostIP,
			"ipv6":            h.HostIPv6,
			"ipv6_mode":       h.IPv6Mode,
			"ipv6_subnet":     h.IPv6Subnet,
			"ndp_iface":       h.NdpIface,
			"ndp_subnets":     h.NdpSubnets,
			"virt_type":       h.VirtType,
			"total_cores":     h.TotalCores,
			"total_memory_mb": h.HostMemTotalMB,
			"total_disk_mb":   h.HostDiskTotalMB,
			"mem_used_mb":     h.HostMemUsedMB,
			"disk_used_mb":    h.HostDiskUsedMB,
			"bandwidth_mbps":  h.BandwidthMbps,
			"load1":           h.Load1,
			"load5":           h.Load5,
			"load15":          h.Load15,
			"uptime":          h.Uptime,
			"net_in_bps":      h.NetInBps,
			"net_out_bps":     h.NetOutBps,
		}
		// 地区留空时，首次见到新公网 IP 自动识别一次（IP 不变则不再重复请求）
		if n.Region == "" && h.HostIP != "" && h.HostIP != n.HostIP {
			if region := geo.Region(h.HostIP); region != "" {
				updates["region"] = region
			}
		}
		s.db.Model(n).Updates(updates)
	}
}
