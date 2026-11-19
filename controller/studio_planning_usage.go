package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type studioPlanningCharge struct {
	ID               int64  `json:"id"`
	TokenID          int    `json:"tokenId"`
	CreatedAt        int64  `json:"createdAt"`
	Type             int    `json:"type"`
	ModelName        string `json:"model"`
	Quota            int64  `json:"quota"`
	PromptTokens     int    `json:"promptTokens"`
	CompletionTokens int    `json:"completionTokens"`
	RequestID        string `json:"requestId"`
	UseTime          int    `json:"durationSeconds"`
}

// GetStudioPlanningUsage exposes the service account's settled ledger with
// stable row IDs and excludes prompts, channel details and token secrets.
func GetStudioPlanningUsage(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	userID := c.GetInt("id")
	user, err := model.GetUserById(userID, false)
	if err != nil || user.Status != common.UserStatusEnabled || user.Group != "studio-planning" || user.Role != common.RoleCommonUser || !c.GetBool("use_access_token") {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "Studio planning service account required"})
		return
	}
	after, err := strconv.ParseInt(c.DefaultQuery("after_id", "0"), 10, 64)
	if err != nil || after < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid after_id"})
		return
	}
	const pageSize = 500
	rows := make([]studioPlanningCharge, 0, pageSize+1)
	err = model.LOG_DB.WithContext(c.Request.Context()).Table("logs").
		Select("id, token_id, created_at, type, model_name, quota, prompt_tokens, completion_tokens, request_id, use_time").
		Where("user_id = ? AND id > ? AND type IN ?", userID, after, []int{model.LogTypeConsume, model.LogTypeRefund}).
		Order("id ASC").Limit(pageSize + 1).Scan(&rows).Error
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "Studio planning ledger unavailable"})
		return
	}
	hasMore := len(rows) > pageSize
	if hasMore {
		rows = rows[:pageSize]
	}
	common.ApiSuccess(c, gin.H{
		"userId": userID, "quotaPerUnit": common.QuotaPerUnit, "balanceQuota": user.Quota,
		"usedQuota": user.UsedQuota, "hasMore": hasMore, "items": rows,
	})
}
