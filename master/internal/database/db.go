package database

import (
	"fmt"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"example.com/codetest/master/internal/config"
	"example.com/codetest/master/internal/model"
)

// Open connects to the database and runs migrations. SQLite (pure-Go driver,
// no CGO) is the default for development; set DB_DRIVER=postgres + DB_DSN in
// production without changing any code.
func Open(cfg config.Config) (*gorm.DB, error) {
	var (
		db  *gorm.DB
		err error
	)
	switch cfg.DBDriver {
	case "postgres":
		db, err = gorm.Open(postgres.Open(cfg.DBDSN), &gorm.Config{})
	default:
		db, err = gorm.Open(sqlite.Open(cfg.DBPath), &gorm.Config{})
	}
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := db.AutoMigrate(
		&model.User{},
		&model.Package{},
		&model.Node{},
		&model.Instance{},
		&model.PortMapping{},
		&model.TrafficLog{},
		&model.Announcement{},
		&model.Transaction{},
		&model.Coupon{},
		&model.GiftCard{},
		&model.MarketListing{},
		&model.Ticket{},
		&model.TicketReply{},
		&model.Setting{},
	); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	// Data migration: platform packages (node_id = 0) default to listed so the
	// existing 「入门」 package stays buyable in the new marketplace model.
	db.Model(&model.Package{}).Where("node_id = ? AND listed = ?", 0, false).
		Update("listed", true)

	// Seed a default package so the panel is usable immediately.
	var count int64
	db.Model(&model.Package{}).Count(&count)
	if count == 0 {
		db.Create(&model.Package{
			Name:       "入门",
			CpuCores:   1,
			MemoryMB:   512,
			DiskMB:     5 * 1024,
			TrafficGB:  500,
			PortSlots:  10,
			IPv6:       false,
			PriceCents: 199,
			Listed:     true,
			Enabled:    true,
		})
	}

	// Seed a couple of announcements so the control panel has content.
	var annCount int64
	db.Model(&model.Announcement{}).Count(&annCount)
	if annCount == 0 {
		db.Create(&model.Announcement{
			Title:   "欢迎使用合租云控制台",
			Content: "多节点 NAT VPS 容器合租平台。可在「托管中心」接入你的宿主机，切分容器、管理端口与流量。",
		})
		db.Create(&model.Announcement{
			Title:   "SSH 现已自动开通",
			Content: "创建实例后系统会自动安装 sshd 并生成 root 密码，实例详情页可直接复制 SSH 命令。",
		})
	}

	return db, nil
}
