package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"example.com/codetest/agent/internal/api"
	"example.com/codetest/agent/internal/config"
	"example.com/codetest/agent/internal/provider"
	"example.com/codetest/agent/internal/service"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg := config.Load()

	prov, err := provider.New(cfg.VirtType, cfg.SocketPath, provider.Options{
		Pool:       cfg.IncusPool,
		Network:    cfg.IncusNetwork,
		IPv6Mode:   cfg.IPv6Mode,
		IPv6Subnet: cfg.IPv6Subnet,
	})
	if err != nil {
		logger.Error("init provider", "err", err)
		os.Exit(1)
	}

	svc := service.New(cfg, prov, logger)
	if err := svc.RebuildNAT(); err != nil {
		logger.Warn("rebuild NAT rules", "err", err)
	}
	// 整机重启后容器应像真 VPS 一样自动恢复：为存量实例补设 always 重启策略
	// （新创建的实例已在 Create 中带上该策略）。
	if rp, ok := prov.(interface{ EnsureRestartPolicies(context.Context) error }); ok {
		if err := rp.EnsureRestartPolicies(context.Background()); err != nil {
			logger.Warn("ensure container restart policy", "err", err)
		}
	}
	if err := svc.EnsureIPv6(); err != nil {
		logger.Warn("configure ipv6", "err", err)
	}

	// Disk limits for OCI instances rely on overlay project quotas. Warn when
	// the storage filesystem lacks them so admins know `df -h` inside
	// containers will show the host disk and the disk limit is not enforced.
	if qs, ok := prov.(interface{ QuotaSupported(context.Context) bool }); ok && !qs.QuotaSupported(context.Background()) {
		logger.Warn("容器磁盘配额不可用：容器存储目录所在文件系统未启用 project quota，请使用 XFS(pquota) 或 ext4(prjquota) 挂载，否则容器内 df -h 显示宿主机磁盘、磁盘限额不生效")
	}

	handler := api.New(cfg, svc)

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("agent listening", "addr", cfg.Listen, "virt", cfg.VirtType)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
