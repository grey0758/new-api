package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestProbeProviderAdmissionDoesNotPromoteAccountCooldown(t *testing.T) {
	oldEnabled, oldRedis := common.AutomaticChannelCooldownEnabled, common.RedisEnabled
	common.AutomaticChannelCooldownEnabled, common.RedisEnabled = true, false
	t.Cleanup(func() {
		common.AutomaticChannelCooldownEnabled, common.RedisEnabled = oldEnabled, oldRedis
		channelCooldownMu.Lock()
		delete(channelCooldownLocal, 98761)
		channelCooldownMu.Unlock()
	})
	for _, tc := range []struct {
		name, body string
		status     int
		cool       bool
	}{
		{"pool short cooldown", `{"error":{"code":"relay_pool_unavailable","message":"private-canary"}}`, 503, false},
		{"pool full", `{"error":{"code":"relay_pool_queue_full"}}`, 429, false},
		{"ordinary unavailable", `{"error":{"code":"service_unavailable"}}`, 503, true},
		{"unstructured unavailable", `upstream failure`, 503, true},
		{"auth must not be bypassed", `{"error":{"code":"relay_pool_unavailable"}}`, 401, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			base := server.URL
			channel := &model.Channel{Id: 98761, Type: 1, Status: common.ChannelStatusEnabled, Key: "test", BaseURL: &base}
			require.NoError(t, model.DB.Save(channel).Error)
			defer model.DB.Delete(channel)
			err := probeOpenAIResponsesStream(channel, "gpt-5.6-sol", time.Second)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "private-canary")
			channelCooldownMu.Lock()
			channelCooldownLocal[98761] = channelCooldownEntry{probing: true}
			channelCooldownMu.Unlock()
			probeAndRecoverChannelCooldown(98761, "test-probe", "gpt-5.6-sol", channelProbeModeContinuous)
			require.Equal(t, tc.cool, IsChannelCoolingDown(98761))
			require.False(t, GetChannelCooldownStatuses([]int{98761})[98761].Probing)
			if !tc.cool {
				// An admission error during recovery cannot erase an older outage.
				channelCooldownMu.Lock()
				r := channelCooldownLocal[98761]
				r.probeRequired, r.coolUntil, r.probing = true, time.Now().Add(time.Minute), true
				channelCooldownLocal[98761] = r
				channelCooldownMu.Unlock()
				probeAndRecoverChannelCooldown(98761, "test-probe", "gpt-5.6-sol", channelProbeModeRecovery)
				require.True(t, IsChannelCoolingDown(98761))
			}
		})
	}
}
