package handler

import (
	"github.com/gin-gonic/gin"

	"example.com/codetest/master/internal/auth"
	"example.com/codetest/master/internal/response"
)

// Profile returns the authenticated user's own record.
func (h *Handler) Profile(c *gin.Context) {
	user, err := h.userByID(c, auth.UID(c))
	if err != nil {
		response.NotFound(c, "用户不存在")
		return
	}
	response.OK(c, user)
}
