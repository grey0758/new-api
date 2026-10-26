package middleware

import (
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// Result retrieval must still work after the successful request exhausts the
// token's quota. Retain expiry, disabled-token, user-status and IP restrictions.
func ImageJobReadAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		key := strings.TrimSpace(c.GetHeader("Authorization"))
		if strings.HasPrefix(strings.ToLower(key), "bearer ") {
			key = strings.TrimSpace(key[7:])
		}
		key = strings.Split(strings.TrimPrefix(key, "sk-"), "-")[0]
		token, err := model.GetTokenByKey(key, false)
		if err != nil || (token.Status != common.TokenStatusEnabled && token.Status != common.TokenStatusExhausted) || (token.ExpiredTime != -1 && token.ExpiredTime < time.Now().Unix()) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "invalid_api_key", "message": "Invalid or expired API key."}})
			return
		}
		if ips := token.GetIpLimits(); len(ips) > 0 && !common.IsIpInCIDRList(net.ParseIP(c.ClientIP()), ips) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "access_denied", "message": "IP address is not allowed."}})
			return
		}
		user, err := model.GetUserCache(token.UserId)
		if err != nil {
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		if user.Status != common.UserStatusEnabled {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Set("id", token.UserId)
		c.Set("token_id", token.Id)
		c.Next()
	}
}
