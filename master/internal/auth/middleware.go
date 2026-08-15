package auth

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	ctxKeyUID      = "auth.uid"
	ctxKeyRole     = "auth.role"
	ctxKeyUsername = "auth.username"
)

// Middleware validates JWTs and injects the caller identity into the context.
type Middleware struct {
	secret string
	ttl    time.Duration
}

func New(secret string, ttlHours int) *Middleware {
	return &Middleware{secret: secret, ttl: time.Duration(ttlHours) * time.Hour}
}

// IssueToken mints a token for a user; used by login/register handlers.
func (m *Middleware) IssueToken(uid uint, username, role string) (string, error) {
	return Sign(m.secret, m.ttl, uid, username, role)
}

// Middleware returns the authentication guard. It aborts with 401 unless the
// Authorization: Bearer <token> header contains a valid token.
func (m *Middleware) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := m.authenticate(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "unauthorized"})
			return
		}
		c.Set(ctxKeyUID, claims.UserID)
		c.Set(ctxKeyRole, claims.Role)
		c.Set(ctxKeyUsername, claims.Username)
		c.Next()
	}
}

// RequireAdmin blocks non-admin callers.
func (m *Middleware) RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetString(ctxKeyRole) != "admin" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 403, "message": "forbidden"})
			return
		}
		c.Next()
	}
}

func (m *Middleware) authenticate(c *gin.Context) (*Claims, bool) {
	header := c.GetHeader("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return nil, false
	}
	claims, err := Parse(m.secret, strings.TrimPrefix(header, "Bearer "))
	if err != nil {
		return nil, false
	}
	return claims, true
}

// ValidateToken parses an externally supplied JWT, e.g. a ?token= query
// parameter on a WebSocket URL. Returns its claims on success.
func (m *Middleware) ValidateToken(token string) (*Claims, error) {
	return Parse(m.secret, token)
}

// SetUser injects identity from claims into the request context, so that
// downstream helpers like auth.UID(c) work even on routes registered outside
// the JWT middleware (e.g. the WebSocket terminal endpoint).
func (m *Middleware) SetUser(c *gin.Context, claims *Claims) {
	c.Set(ctxKeyUID, claims.UserID)
	c.Set(ctxKeyRole, claims.Role)
	c.Set(ctxKeyUsername, claims.Username)
}

// UID returns the authenticated user id from the request context.
func UID(c *gin.Context) uint { return c.GetUint(ctxKeyUID) }

// Role returns the authenticated user's role.
func Role(c *gin.Context) string { return c.GetString(ctxKeyRole) }

// Username returns the authenticated user's username.
func Username(c *gin.Context) string { return c.GetString(ctxKeyUsername) }
