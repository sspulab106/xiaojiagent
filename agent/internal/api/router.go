// Package api wires the agent's HTTP surface:
//   - a token-authenticated REST API + WebSocket terminal for the master
//   - a local management panel (http://host:8792/) to bind the node token
package api

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"example.com/codetest/agent/internal/config"
	"example.com/codetest/agent/internal/service"
)

// New builds the agent router.
func New(cfg config.Config, svc *service.Service) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery(), gin.Logger())

	// Token is held in a dynamic source so the management panel can update it
	// at runtime without restarting the agent.
	tokenSrc := NewTokenSource(cfg.Token)

	// Local management panel (no auth; settings writes require WEB_PASSWORD).
	panel := NewPanelHandler(cfg, tokenSrc, svc)
	r.GET("/", panel.Page)
	r.GET("/admin/status", panel.Status)
	r.GET("/admin/diag/:name", panel.Diag)
	r.POST("/admin/settings", panel.Settings)
	r.POST("/admin/update", panel.Update)
	r.GET("/admin/firewall", panel.Firewall)
	r.POST("/admin/firewall", panel.FirewallCreate)
	r.DELETE("/admin/firewall/:id", panel.FirewallDelete)

	// Master-facing API: every route requires Authorization: Bearer <token>.
	agent := r.Group("/agent/v1")
	agent.Use(tokenAuth(tokenSrc))
	{
		agent.GET("/health", svc.HandleHealth)
		agent.GET("/instances", svc.HandleList)
		agent.GET("/instances/:name", svc.HandleGet)
		agent.POST("/instances", svc.HandleCreate)
		agent.POST("/instances/:name/action", svc.HandleAction)
		agent.DELETE("/instances/:name", svc.HandleDelete)
		agent.GET("/instances/:name/stats", svc.HandleStats)
		agent.POST("/instances/:name/password", svc.HandleChangePassword)
		agent.POST("/instances/:name/ports", svc.HandleAddPort)
		agent.DELETE("/ports/:id", svc.HandleRemovePort)
		agent.GET("/instances/:name/terminal", svc.HandleTerminal)

		// Remote self-check: run scripts/verify-ndp.sh on the node and poll
		// the output (async; the run can take minutes).
		agent.POST("/selfcheck", svc.HandleSelfCheckStart)
		agent.GET("/selfcheck/:id", svc.HandleSelfCheckStatus)

		// Firewall (rfw eBPF) proxy.
		agent.GET("/firewall/status", svc.HandleFWStatus)
		agent.GET("/firewall/rules", svc.HandleFWList)
		agent.POST("/firewall/rules", svc.HandleFWCreate)
		agent.DELETE("/firewall/rules/:id", svc.HandleFWDelete)
	}
	return r
}

// tokenAuth reads the current token from the dynamic source on every request.
func tokenAuth(src *TokenSource) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "unauthorized"})
			return
		}
		given := strings.TrimPrefix(header, "Bearer ")
		if subtle.ConstantTimeCompare([]byte(given), []byte(src.Get())) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "unauthorized"})
			return
		}
		c.Next()
	}
}
