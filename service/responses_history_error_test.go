package service

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestResponsesHistoryIDRejection(t *testing.T) {
	old := common.AutomaticChannelCooldownEnabled
	common.AutomaticChannelCooldownEnabled = true
	t.Cleanup(func() { common.AutomaticChannelCooldownEnabled = old })
	for _, upstream := range []*types.NewAPIError{
		types.WithOpenAIError(types.OpenAIError{Code: "invalid_id_prefix", Type: "server_error", Message: "provider diagnostic"}, 503),
		types.NewOpenAIError(errors.New("[ApiIdParam] [input[17].id] [invalid_id_prefix] Invalid 'input[17].id': 'fc_synthetic'. Expected an ID that begins with 'ctc'."), types.ErrorCode("unknown_error"), 503),
	} {
		require.True(t, IsClientRequestValidationError(upstream))
		require.True(t, IsRequestScopedUpstreamRejectionError(upstream))
		require.False(t, shouldRecordChannelFailureForCooldown(*types.NewChannelError(47, 1, "test", false, "", true), upstream))
		err := NormalizeResponsesHistoryIDError(upstream)
		require.Equal(t, http.StatusBadRequest, err.StatusCode)
		require.Equal(t, ResponsesHistoryIDErrorCode, err.GetErrorCode())
		require.True(t, types.IsSkipRetryError(err))
		require.True(t, types.ShouldPreserveUserError(err))
		require.Contains(t, err.ToOpenAIError().Message, "original compatible route")
		require.NotContains(t, err.ToOpenAIError().Message, "fc_synthetic")
		require.False(t, ShouldDisableChannel(err))
	}
}

func TestResponsesHistoryIDDoesNotClassifyGenericProviderFailures(t *testing.T) {
	for _, message := range []string{"server overloaded", "invalid_id_prefix mentioned in documentation", "[ApiIdParam] [model] [invalid_id_prefix]", "Expected an ID that begins with 'ctc'"} {
		err := types.NewOpenAIError(errors.New(message), types.ErrorCode("unknown_error"), 503)
		require.False(t, IsResponsesHistoryIDError(err))
		require.Same(t, err, NormalizeResponsesHistoryIDError(err))
	}
	require.Nil(t, NormalizeResponsesHistoryIDError(nil))
}
