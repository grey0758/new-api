package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

func UpdateInvitationPolicy(c *gin.Context) {
	var policy operation_setting.InvitationSetting
	if err := common.DecodeJson(c.Request.Body, &policy); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.UpdateInvitationPolicy(policy); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, model.GetInvitationPolicy())
}
