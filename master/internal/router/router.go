package router

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"example.com/codetest/master/internal/auth"
	"example.com/codetest/master/internal/config"
	"example.com/codetest/master/internal/handler"
	"example.com/codetest/master/internal/service"
)

// New wires up the full Gin engine: middleware, public routes, authed routes,
// admin routes, and the WebSocket terminal endpoint.
func New(cfg config.Config, db *gorm.DB, logger *slog.Logger) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery(), gin.Logger())

	authM := auth.New(cfg.JWTSecret, cfg.TokenTTLHours)
	svc := service.New(db, cfg, logger)
	h := handler.New(db, svc, authM, cfg, logger)

	api := r.Group("/api/v1")

	// Public: login & register.
	api.POST("/auth/login", h.Login)
	api.POST("/auth/register", h.Register)

	// Public: one-shot install scripts report the host's reachable address and
	// IP back so nodes created without an agent_addr can be discovered. The
	// node token in the body is the shared secret that authenticates the call.
	api.POST("/nodes/register", h.RegisterNode)

	// Authenticated: tenant-facing endpoints.
	authed := api.Group("")
	authed.Use(authM.Middleware(), h.BlockBanned())
	{
		authed.GET("/user/profile", h.Profile)
		authed.POST("/user/recharge", h.Recharge)
		authed.POST("/user/redeem", h.RedeemGiftCard)
		authed.GET("/user/transactions", h.ListTransactions)
		authed.GET("/user/coupons", h.ListCoupons)
		authed.GET("/announcements", h.ListAnnouncements)
		authed.GET("/packages", h.ListPackages)
		authed.GET("/images", h.ListImages)

		// 托管中心（用户自己的宿主机）
		authed.GET("/nodes", h.MyNodes)
		authed.POST("/nodes", h.AddMachine)
		authed.GET("/nodes/:id", h.GetNode)
		authed.GET("/nodes/:id/packages", h.ListNodePackages)
		authed.POST("/nodes/:id/packages", h.CreateNodePackage)
		authed.PUT("/packages/:id", h.UpdatePackage)
		authed.DELETE("/packages/:id", h.DeletePackage)
		authed.GET("/nodes/:id/coupons", h.ListNodeCoupons)
		authed.POST("/nodes/:id/coupons", h.CreateNodeCoupon)
		authed.PUT("/coupons/:id", h.UpdateCoupon)
		authed.DELETE("/coupons/:id", h.DeleteNodeCoupon)
		authed.GET("/nodes/:id/stats", h.NodeStats)
		authed.POST("/nodes/:id/selfcheck", h.NodeSelfCheckOwner)
		authed.GET("/nodes/:id/selfcheck/:runId", h.NodeSelfCheckOwnerStatus)
		authed.GET("/nodes/:id/firewall", h.NodeFirewall)
		authed.POST("/nodes/:id/firewall", h.NodeFirewallCreate)
		authed.DELETE("/nodes/:id/firewall/:ruleId", h.NodeFirewallDelete)

		authed.GET("/instances", h.ListInstances)
		authed.POST("/instances", h.CreateInstance)
		authed.GET("/instances/:id", h.GetInstance)
		authed.POST("/instances/:id/action", h.InstanceAction)
		authed.POST("/instances/:id/auto-renew", h.SetAutoRenew)
		authed.POST("/instances/:id/password", h.ChangeInstancePassword)
		authed.DELETE("/instances/:id", h.DeleteInstance)
		authed.GET("/instances/:id/stats", h.InstanceStats)
		authed.GET("/instances/:id/ports", h.ListPorts)
		authed.POST("/instances/:id/ports", h.AddPort)
		authed.DELETE("/ports/:id", h.DeletePort)

		// 交易市场（二手实例买卖：按剩余时间/流量折合剩余价值定价）
		authed.GET("/market/listings", h.ListMarketListings)
		authed.GET("/market/listings/mine", h.ListMyMarketListings)
		authed.POST("/market/listings", h.CreateMarketListing)
		authed.DELETE("/market/listings/:id", h.CancelMarketListing)
		authed.POST("/market/listings/:id/buy", h.BuyMarketListing)

		// 技术支持工单：用户针对自己的实例提工单 → 路由到宿主机技术支持页面
		authed.GET("/tickets", h.ListTickets)
		authed.GET("/tickets/unread-count", h.TicketUnreadCount)
		authed.POST("/tickets", h.CreateTicket)
		authed.GET("/tickets/:id", h.GetTicket)
		authed.POST("/tickets/:id/reply", h.ReplyTicket)
		authed.POST("/tickets/:id/resolve", h.ResolveTicket)
		authed.POST("/tickets/:id/reopen", h.ReopenTicket)

		// 个人信息：修改登录密码、绑定邮箱
		authed.POST("/user/password", h.ChangeLoginPassword)
		authed.POST("/user/email", h.BindEmail)
		authed.POST("/user/email/verify", h.VerifyEmail)

		// 平台设置（管理员：发件邮箱、邮箱验证开关等）
		authed.GET("/settings", h.GetSettings)
		authed.PUT("/settings", h.UpdateSettings)
	}

	// Admin only.
	admin := authed.Group("/admin")
	admin.Use(authM.RequireAdmin())
	{
		admin.GET("/stats", h.AdminStats)
		admin.GET("/nodes", h.ListNodes)
		admin.POST("/nodes", h.AddNode)
		admin.POST("/hosts", h.AddHost)
		admin.DELETE("/nodes/:id", h.DeleteNode)
		admin.POST("/nodes/:id/install-script", h.NodeInstallScript)
		admin.POST("/nodes/:id/selfcheck", h.NodeSelfCheck)
		admin.GET("/nodes/:id/selfcheck/:runId", h.NodeSelfCheckStatus)
		admin.GET("/users", h.ListUsers)
		admin.GET("/users/:id", h.AdminUserDetail)
		admin.POST("/users/:id/extend", h.ExtendUser)
		admin.POST("/users/:id/ban", h.AdminBanUser)
		admin.POST("/users/:id/password", h.AdminResetPassword)
		admin.POST("/users/:id/balance", h.AdminAdjustBalance)
		admin.GET("/packages", h.ListPackages)
		admin.POST("/packages", h.AddPackage)
		admin.DELETE("/packages/:id", h.DeletePackage)
		admin.GET("/coupons", h.ListCouponsAdmin)
		admin.POST("/coupons", h.AddCoupon)
		admin.PUT("/coupons/:id", h.AdminUpdateCoupon)
		admin.DELETE("/coupons/:id", h.DeleteCoupon)
		admin.GET("/gift-cards", h.AdminListGiftCards)
		admin.POST("/gift-cards", h.AdminCreateGiftCards)
		admin.PUT("/gift-cards/:id", h.AdminUpdateGiftCard)
		admin.DELETE("/gift-cards/:id", h.AdminDeleteGiftCard)
	}

	// WebSocket terminal: registered outside the authed group because the
	// browser passes the JWT as a query parameter (no custom headers on
	// browser WebSockets). The handler validates it itself.
	api.GET("/instances/:id/terminal", h.Terminal)

	// Agent binary download for the one-shot install scripts (public).
	r.GET("/downloads/agent", h.DownloadAgent)

	// Background job: keep node health/status fresh.
	go svc.NodeHealthLoop()

	return r
}
