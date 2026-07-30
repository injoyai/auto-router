package server

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto-router/internal/jwt"
)

// GatewayAuth middleware checks the single gateway bearer token.
// I9: the token is compared in constant time to avoid timing side channels.
// The error envelope is protocol-aware: /v1/messages returns the Claude
// error shape, other gateway routes return the OpenAI error shape.
//
// The token is read via tokenGetter on every request so that the gateway
// token can be rotated at runtime without restarting the server.
func GatewayAuth(tokenGetter func() string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := tokenGetter()
		h := c.GetHeader("Authorization")
		ok := strings.HasPrefix(h, "Bearer ") && subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(h, "Bearer ")), []byte(token)) == 1
		if !ok {
			writeAuthError(c)
			return
		}
		c.Next()
	}
}

// writeAuthError aborts with a 401 in the client's protocol format, inferred
// from the request path (/v1/messages → Claude, else OpenAI).
func writeAuthError(c *gin.Context) {
	if strings.HasPrefix(c.Request.URL.Path, "/v1/messages") {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"type": "error", "error": gin.H{"type": "authentication_error", "message": "invalid token"}})
		return
	}
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "invalid token", "type": "auth_error"}})
}

// AdminAuth middleware checks the admin JWT.
func AdminAuth(mgr *jwt.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		_, err := mgr.Verify(strings.TrimPrefix(h, "Bearer "))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}
		c.Next()
	}
}
