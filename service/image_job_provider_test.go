package service

import (
	"errors"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestFlowImageJobProviderBusyRequiresExactAuthoritativeRejection(t *testing.T) {
	for _, tc := range []struct {
		message string
		status  int
		code    types.ErrorCode
		want    string
	}{
		{"Google RPC rejection: PUBLIC_ERROR_HIGH_TRAFFIC", 500, types.ErrorCodeBadResponseStatusCode, "flow_provider_high_traffic"},
		{"Google RPC rejection: PUBLIC_ERROR_HIGH_TRAFFIC", 503, types.ErrorCodeBadResponseStatusCode, ""},
		{"Google RPC rejection: PUBLIC_ERROR_HIGH_TRAFFIC", 500, types.ErrorCodeDoRequestFailed, ""},
		{"Google RPC rejection: PUBLIC_ERROR_HIGH_TRAFFIC trailing", 500, types.ErrorCodeBadResponseStatusCode, ""},
		{"PUBLIC_ERROR_HIGH_TRAFFIC", 500, types.ErrorCodeBadResponseStatusCode, ""},
		{"Google RPC rejection: PUBLIC_ERROR_UNUSUAL_ACTIVITY", 500, types.ErrorCodeBadResponseStatusCode, ""},
		{"Google RPC rejection: PUBLIC_ERROR_HIGH_TRAFFIC\nPUBLIC_ERROR_UNUSUAL_ACTIVITY", 500, types.ErrorCodeBadResponseStatusCode, ""},
		{"bad response status code 500", 500, types.ErrorCodeBadResponseStatusCode, ""},
	} {
		err := types.NewOpenAIError(errors.New(tc.message), tc.code, tc.status)
		require.Equal(t, tc.want, FlowImageJobRejectionCode(err), tc.message)
	}
	require.Empty(t, FlowImageJobRejectionCode(nil))
}
