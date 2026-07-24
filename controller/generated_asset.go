package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetGeneratedImageTask(c *gin.Context) {
	taskId, ok := generatedImageTaskId(c)
	if !ok {
		return
	}
	task, err := service.GetGeneratedAssetTask(c, generatedAssetUserId(c), taskId)
	if err != nil {
		respondGeneratedAssetError(c, err)
		return
	}
	c.JSON(http.StatusOK, task)
}

func GetGeneratedImageTaskContent(c *gin.Context) {
	taskId, ok := generatedImageTaskId(c)
	if !ok {
		return
	}
	signedURL, err := service.PresignGeneratedAssetContent(c, generatedAssetUserId(c), taskId)
	if err != nil {
		respondGeneratedAssetError(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Redirect(http.StatusFound, signedURL)
}

func ListGeneratedImageTasks(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	result, err := service.ListGeneratedAssetTasks(c, generatedAssetUserId(c), page, pageSize)
	if err != nil {
		respondGeneratedAssetError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func generatedAssetUserId(c *gin.Context) int {
	userId := common.GetContextKeyInt(c, constant.ContextKeyUserId)
	if userId <= 0 {
		userId = c.GetInt("id")
	}
	return userId
}

func generatedImageTaskId(c *gin.Context) (string, bool) {
	taskId := strings.TrimSpace(c.Param("task_id"))
	if !strings.HasPrefix(taskId, "img_") || len(taskId) > 64 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "invalid image task id",
				"type":    "invalid_request_error",
				"code":    "invalid_image_task_id",
			},
		})
		return "", false
	}
	return taskId, true
}

func respondGeneratedAssetError(c *gin.Context, err error) {
	statusCode := http.StatusInternalServerError
	errorCode := "generated_asset_error"
	message := "generated image task is unavailable"
	switch {
	case errors.Is(err, service.ErrGeneratedAssetNotFound):
		statusCode = http.StatusNotFound
		errorCode = "image_task_not_found"
		message = "image task not found"
	case errors.Is(err, service.ErrGeneratedAssetExpired):
		statusCode = http.StatusGone
		errorCode = "image_task_expired"
		message = "image task content has expired"
	}
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"message": message,
			"type":    "generated_asset_error",
			"code":    errorCode,
		},
	})
}
