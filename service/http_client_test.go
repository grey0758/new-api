package service

import (
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestRelayResponseHeaderTimeout(t *testing.T) {
	original := common.RelayResponseHeaderTimeout
	t.Cleanup(func() { common.RelayResponseHeaderTimeout = original })

	tests := []struct {
		name    string
		seconds int
		want    time.Duration
	}{
		{name: "disabled", seconds: 0, want: 0},
		{name: "configured", seconds: 7, want: 7 * time.Second},
		{name: "negative disabled", seconds: -1, want: 0},
		{name: "overflow clamped", seconds: maxTimeoutSeconds + 1, want: time.Duration(maxTimeoutSeconds) * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			common.RelayResponseHeaderTimeout = tt.seconds
			transport := &http.Transport{}
			applyRelayResponseHeaderTimeout(transport)
			require.Equal(t, tt.want, transport.ResponseHeaderTimeout)
		})
	}
}
