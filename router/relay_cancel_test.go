package router

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Exercise routing, billing precharge/refund and transport together. Canceling
// an interactive turn must not penalize its provider or start a fallback turn.
func TestOuterToolsCancelBeforeHeadersRefundsWithoutRetry(t *testing.T) {
	setupRelayRouterTestDB(t)
	oldRetry := common.RetryTimes
	common.RetryTimes = 2
	t.Cleanup(func() { common.RetryTimes = oldRetry })
	entered, gone, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var primaryCalls, backupCalls atomic.Int32
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryCalls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		close(entered)
		select {
		case <-r.Context().Done():
			close(gone)
		case <-release:
		}
	}))
	defer primary.Close()
	defer close(release)
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backupCalls.Add(1)
		http.Error(w, "unexpected retry", 500)
	}))
	defer backup.Close()
	channel := createRelayTestChannel(t, "cancel-primary", primary.URL+"/outer-tools", 1000, 100, "gpt-4o-mini")
	createRelayTestChannel(t, "cancel-backup", backup.URL+"/outer-tools", 100, 100, "gpt-4o-mini")
	createRelayTestUserAndToken(t, 1, "canceltesttoken1234567890")
	engine := gin.New()
	SetRelayRouter(engine)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-4o-mini","input":"hello","stream":true}`)).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer sk-canceltesttoken1234567890")
	done := make(chan struct{})
	go func() { engine.ServeHTTP(httptest.NewRecorder(), request); close(done) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream not entered")
	}
	cancel()
	select {
	case <-gone:
	case <-time.After(time.Second):
		t.Fatal("upstream did not receive cancellation")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("gateway did not release request")
	}
	require.EqualValues(t, 1, primaryCalls.Load())
	require.Zero(t, backupCalls.Load())
	require.False(t, service.IsChannelCoolingDown(channel.Id))
	var logs int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("type = ?", model.LogTypeError).Count(&logs).Error)
	require.Zero(t, logs)
	require.Eventually(t, func() bool {
		var user model.User
		return model.DB.First(&user, 1).Error == nil && user.Quota == 1000000
	}, 2*time.Second, 10*time.Millisecond, "precharge must be refunded")
}
