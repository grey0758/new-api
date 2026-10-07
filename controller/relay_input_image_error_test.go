package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/relay/channel/openai"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestLocalResponsesImageFailureRemains400AfterChannelErrorHandling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("relay_channel_error_seen", true)
	err := openai.NewResponsesInputImageError(errors.New("could not download the input image"))
	visible := sanitizeRelayErrorForUser(c, err)
	require.Equal(t, http.StatusBadRequest, visible.StatusCode)
	require.Equal(t, types.ErrorCodeConvertRequestFailed, visible.ToOpenAIError().Code)
	require.Equal(t, "could not download the input image", visible.ToOpenAIError().Message)
	require.False(t, shouldRetry(c, visible, 3, 47))
	require.False(t, types.IsRecordErrorLog(visible))
}
