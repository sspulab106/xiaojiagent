// Package response defines the unified JSON envelope used by every API call:
//
//	{ "code": 0, "message": "ok", "data": ... }
//
// A zero code means success; non-zero codes mirror the HTTP status.
package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func JSON(c *gin.Context, status int, code int, message string, data any) {
	c.JSON(status, gin.H{"code": code, "message": message, "data": data})
}

func OK(c *gin.Context, data any) {
	JSON(c, http.StatusOK, 0, "ok", data)
}

func Created(c *gin.Context, data any) {
	JSON(c, http.StatusCreated, 0, "ok", data)
}

func Error(c *gin.Context, status int, message string) {
	JSON(c, status, status, message, nil)
}

func BadRequest(c *gin.Context, message string)   { Error(c, http.StatusBadRequest, message) }
func Unauthorized(c *gin.Context, message string) { Error(c, http.StatusUnauthorized, message) }
func Forbidden(c *gin.Context, message string)    { Error(c, http.StatusForbidden, message) }
func NotFound(c *gin.Context, message string)     { Error(c, http.StatusNotFound, message) }
func Internal(c *gin.Context, message string)     { Error(c, http.StatusInternalServerError, message) }
