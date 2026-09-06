package helper

import (
	"context"
	"io"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestStreamScannerCancellationClosesSilentBodyBeforeWaiting(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close()
	defer reader.Close()
	c, resp, info := setupStreamTest(t, reader)
	resp.Body = reader
	info.DisablePing = true
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	c.Request = c.Request.WithContext(ctx)
	first := make(chan struct{})
	done := make(chan struct{})
	go func() {
		StreamScannerHandler(c, resp, info, func(_ string, _ *StreamResult) { close(first) })
		close(done)
	}()
	_, err := io.WriteString(writer, "data: first\n\n")
	require.NoError(t, err)
	<-first
	cancel()
	select {
	case <-done:
		require.Equal(t, relaycommon.StreamEndReasonClientGone, info.StreamStatus.EndReason)
	case <-time.After(time.Second):
		t.Error("scanner waited for a silent read before closing its body")
		_ = reader.Close()
		<-done
	}
}
