package controller

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type registrationChallengeRequest struct {
	Target string `json:"target"`
}

func CreateRegistrationChallenge(c *gin.Context) {
	if !common.RegisterEnabled {
		common.ApiErrorI18n(c, i18n.MsgUserRegisterDisabled)
		return
	}
	if !common.PasswordRegisterEnabled {
		common.ApiErrorI18n(c, i18n.MsgUserPasswordRegisterDisabled)
		return
	}

	var request registrationChallengeRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	target := strings.TrimSpace(request.Target)
	if target == "" || utf8.RuneCountInString(target) > model.UserNameMaxLength {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	challenge, err := service.IssueRegistrationChallenge(target)
	if err != nil {
		if !errors.Is(err, service.ErrRegistrationChallengeInvalid) {
			common.SysLog("failed to issue registration challenge: " + err.Error())
		}
		common.ApiErrorI18n(c, i18n.MsgUserChallengeUnavailable)
		return
	}
	common.ApiSuccess(c, challenge)
}
