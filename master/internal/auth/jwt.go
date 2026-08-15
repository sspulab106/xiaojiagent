// Package auth provides JWT signing/parsing and Gin middleware for
// authentication and RBAC. Roles are "admin" and "user".
package auth

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims is the JWT payload. Kept minimal: user id, username, role.
type Claims struct {
	UserID   uint   `json:"uid"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// Sign issues a new HS256 token.
func Sign(secret string, ttl time.Duration, uid uint, username, role string) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:   uid,
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

// Parse validates a token and returns its claims.
func Parse(secret, token string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	return claims, nil
}
