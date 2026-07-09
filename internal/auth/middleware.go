package auth

import (
	"crypto/subtle"
	"strings"

	"github.com/gin-gonic/gin"
)

// Middleware 校验单 Token。Token 可来自两种位置：
//  1. Authorization: Bearer <token>
//  2. X-API-Token: <token>
func Middleware(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		got := extractToken(c)
		if token != "" && subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1 {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(401, gin.H{"error": "unauthorized", "message": "需要有效的 API Token"})
	}
}

func extractToken(c *gin.Context) string {
	if h := c.GetHeader("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	if t := c.GetHeader("X-API-Token"); t != "" {
		return t
	}
	return ""
}
