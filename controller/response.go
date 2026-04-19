package controller

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func RetrieveResponse(c *gin.Context) {
	responseID := strings.TrimSpace(c.Param("response_id"))
	if responseID == "" {
		writeRetrieveResponseError(c, http.StatusBadRequest, "response_id is required")
		return
	}

	channel, modelName, err := resolveRetrieveResponseChannel(c, responseID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeRetrieveResponseError(c, http.StatusNotFound, "response not found")
			return
		}
		writeRetrieveResponseError(c, http.StatusBadRequest, err.Error())
		return
	}
	if channel == nil {
		writeRetrieveResponseError(c, http.StatusNotFound, "response not found")
		return
	}
	if channel.Status != common.ChannelStatusEnabled {
		writeRetrieveResponseError(c, http.StatusForbidden, "channel is disabled")
		return
	}

	if newAPIError := middleware.SetupContextForSelectedChannel(c, channel, modelName); newAPIError != nil {
		c.JSON(newAPIError.StatusCode, gin.H{"error": newAPIError.ToOpenAIError()})
		return
	}

	info := relaycommon.GenRelayInfoOpenAI(c, &dto.BaseRequest{})
	info.RelayMode = relayconstant.RelayModeResponses
	info.RelayFormat = types.RelayFormatOpenAIResponses
	info.InitRequestConversionChain()
	info.InitChannelMeta(c)

	adaptor := relay.GetAdaptor(info.ApiType)
	if adaptor == nil {
		writeRetrieveResponseError(c, http.StatusBadGateway, fmt.Sprintf("invalid api type: %d", info.ApiType))
		return
	}
	adaptor.Init(info)

	respAny, err := adaptor.DoRequest(c, info, nil)
	if err != nil {
		writeRetrieveResponseError(c, http.StatusBadGateway, err.Error())
		return
	}
	resp, ok := respAny.(*http.Response)
	if !ok {
		writeRetrieveResponseError(c, http.StatusBadGateway, "invalid upstream response type")
		return
	}
	if resp == nil {
		writeRetrieveResponseError(c, http.StatusBadGateway, "empty upstream response")
		return
	}
	defer service.CloseResponseBodyGracefully(resp)

	if resp.StatusCode >= http.StatusBadRequest {
		newAPIError := service.RelayErrorHandler(c.Request.Context(), resp, false)
		c.JSON(newAPIError.StatusCode, gin.H{"error": newAPIError.ToOpenAIError()})
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		writeRetrieveResponseError(c, http.StatusBadGateway, err.Error())
		return
	}
	service.IOCopyBytesGracefully(c, resp, body)
}

func resolveRetrieveResponseChannel(c *gin.Context, responseID string) (*model.Channel, string, error) {
	userID := c.GetInt("id")
	if userID <= 0 {
		return nil, "", fmt.Errorf("invalid user")
	}

	ref, err := model.GetRelayResponseRefByResponseID(responseID)
	if err == nil && ref != nil {
		if ref.UserID != 0 && ref.UserID != userID {
			return nil, "", gorm.ErrRecordNotFound
		}
		ch, chErr := model.GetChannelById(ref.ChannelID, true)
		if chErr == nil {
			return ch, strings.TrimSpace(ref.ModelName), nil
		}
		if !errors.Is(chErr, gorm.ErrRecordNotFound) {
			return nil, "", chErr
		}
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, "", err
	}

	channelIDAny, ok := common.GetContextKey(c, constant.ContextKeyTokenSpecificChannelId)
	if !ok {
		return nil, "", gorm.ErrRecordNotFound
	}
	channelID, convErr := strconv.Atoi(fmt.Sprint(channelIDAny))
	if convErr != nil {
		return nil, "", convErr
	}
	ch, chErr := model.GetChannelById(channelID, true)
	if chErr != nil {
		return nil, "", chErr
	}
	return ch, "", nil
}

func writeRetrieveResponseError(c *gin.Context, statusCode int, message string) {
	err := types.NewErrorWithStatusCode(errors.New(message), types.ErrorCodeInvalidRequest, statusCode, types.ErrOptionWithSkipRetry())
	c.JSON(statusCode, gin.H{"error": err.ToOpenAIError()})
}
