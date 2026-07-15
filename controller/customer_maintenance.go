package controller

import (
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type UpdateCustomerContactRequest struct {
	WeChatAccount  string `json:"wechat_account"`
	WeChatUsername string `json:"wechat_username"`
	PushEnabled    *bool  `json:"push_enabled"`
	Notes          string `json:"notes"`
}

func GetCustomerMaintenanceCustomers(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	items, total, err := model.ListCustomerMaintenanceCustomers(
		c.Query("keyword"),
		pageInfo.GetStartIdx(),
		pageInfo.GetPageSize(),
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func UpdateCustomerMaintenanceContact(c *gin.Context) {
	userId, err := strconv.Atoi(c.Param("id"))
	if err != nil || userId <= 0 {
		common.ApiErrorMsg(c, "无效的用户ID")
		return
	}
	var request UpdateCustomerContactRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	request.WeChatAccount = strings.TrimSpace(request.WeChatAccount)
	request.WeChatUsername = strings.TrimSpace(request.WeChatUsername)
	request.Notes = strings.TrimSpace(request.Notes)
	if utf8.RuneCountInString(request.WeChatAccount) > 128 {
		common.ApiErrorMsg(c, "微信号不能超过128个字符")
		return
	}
	if utf8.RuneCountInString(request.WeChatUsername) > 128 {
		common.ApiErrorMsg(c, "微信用户名不能超过128个字符")
		return
	}
	if utf8.RuneCountInString(request.Notes) > 512 {
		common.ApiErrorMsg(c, "备注不能超过512个字符")
		return
	}
	pushEnabled := true
	if request.PushEnabled != nil {
		pushEnabled = *request.PushEnabled
	}
	contact, err := model.UpsertCustomerContact(
		userId,
		request.WeChatAccount,
		request.WeChatUsername,
		request.Notes,
		pushEnabled,
	)
	if err != nil {
		if errors.Is(err, model.ErrCustomerMaintenanceUserNotFound) {
			common.ApiErrorMsg(c, "用户不存在或不是普通客户")
			return
		}
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, contact)
}

func GetCustomerMaintenanceNotifications(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	items, total, err := model.ListCustomerMaintenanceNotifications(
		c.Query("keyword"),
		c.Query("status"),
		c.Query("push_status"),
		pageInfo.GetStartIdx(),
		pageInfo.GetPageSize(),
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func BackfillCustomerMaintenanceNotifications(c *gin.Context) {
	months, err := strconv.Atoi(c.DefaultQuery("months", "2"))
	if err != nil || months < 1 || months > 12 {
		common.ApiErrorMsg(c, "月份范围必须在1到12之间")
		return
	}
	result, err := service.BackfillExpiredMonthlySubscriptionNotifications(months, time.Now())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func AcknowledgeCustomerMaintenanceNotification(c *gin.Context) {
	notificationId, err := strconv.Atoi(c.Param("id"))
	if err != nil || notificationId <= 0 {
		common.ApiErrorMsg(c, "无效的通知ID")
		return
	}
	if err := model.AcknowledgeCustomerNotification(notificationId); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.ApiErrorMsg(c, "通知不存在")
			return
		}
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
