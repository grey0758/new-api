package router

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type krillReselectTransport func(*http.Request) (*http.Response, error)

func (transport krillReselectTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestKrillTextReselectPreservesUpstreamStatus(t *testing.T) {
	const (
		centralURL    = "http://sub2api-krill:8080"
		backupURL     = "http://backup.invalid"
		modelName     = "gpt-4o-mini"
		tokenKey      = "krillreselectfixture"
		responsesPath = "/v1/responses"
		chatPath      = "/v1/chat/completions"
		responsesDone = "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":13,\"output_tokens\":5,\"total_tokens\":18}}}\n\n"
		chatDone      = `{"id":"chatcmpl-fixture","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"fixture answer"},"finish_reason":"stop"}],"usage":{"prompt_tokens":13,"completion_tokens":5,"total_tokens":18}}`
	)
	type route struct {
		channelID int
		baseURL   string
		status    int
		code      string
		message   string
		cooling   bool
	}
	badGateway84 := route{channelID: 84, baseURL: centralURL, status: 502}
	unavailable84 := route{channelID: 84, baseURL: centralURL, status: 503}
	unavailable85 := route{channelID: 85, baseURL: centralURL, status: 503}
	noRoute85 := route{channelID: 85, baseURL: centralURL, status: 404, message: "no route available for the requested model"}
	success47 := route{channelID: 47, baseURL: backupURL, status: 200}
	busy47 := route{channelID: 47, baseURL: backupURL, status: 503, code: "server_busy"}
	noRoute47 := route{channelID: 47, baseURL: backupURL, status: 404, message: "no route available for the requested model"}
	unknown404At47 := route{channelID: 47, baseURL: backupURL, status: 404, message: "fixture endpoint missing"}
	cooling85 := route{channelID: 85, baseURL: centralURL, cooling: true}
	other503 := route{channelID: 86, baseURL: centralURL, status: 503}
	wrongOrigin85 := route{channelID: 85, baseURL: "http://other.invalid", status: 503}
	for _, tc := range []struct {
		name         string
		path         string
		routes       []route
		cache        bool
		wantAttempts []int
		want         int
	}{
		{"relay failure without a database candidate", responsesPath, []route{badGateway84}, false, []int{84}, 502},
		{"busy with all cached candidates excluded", responsesPath, []route{unavailable84}, true, []int{84}, 503},
		{"enabled alternative in cooldown stays excluded", responsesPath, []route{cooling85, unavailable84}, false, []int{84}, 503},
		{"available alternative settles once", responsesPath, []route{unavailable84, success47}, false, []int{84, 47}, 200},
		{"busy backup with exhausted database candidates", responsesPath, []route{unavailable84, busy47}, false, []int{84, 47}, 503},
		{"busy backup with exhausted cached candidates", responsesPath, []route{unavailable84, busy47}, true, []int{84, 47}, 503},
		{"backup busy outside Krill keeps existing behavior", responsesPath, []route{other503, busy47}, true, []int{86, 47}, 500},
		{"other origin keeps existing behavior", responsesPath, []route{wrongOrigin85, noRoute47}, false, []int{85, 47}, 500},
		{"central 85 alone with exhausted channels", responsesPath, []route{unavailable85}, true, []int{85}, 503},
		{"three attempts with database selection", responsesPath, []route{noRoute85, unavailable84, noRoute47}, false, []int{85, 84, 47}, 503},
		{"three attempts with cached selection", responsesPath, []route{noRoute85, unavailable84, noRoute47}, true, []int{85, 84, 47}, 503},
		{"both central channels unavailable before backup route failure", responsesPath, []route{unavailable85, unavailable84, noRoute47}, false, []int{85, 84, 47}, 503},
		{"central 85 routing hold and unavailable backup route", responsesPath, []route{unavailable85, noRoute47}, true, []int{85, 47}, 503},
		{"backup route failure preserves central bad gateway", responsesPath, []route{badGateway84, noRoute47}, false, []int{84, 47}, 502},
		{"central and legacy model 404 without 5xx", responsesPath, []route{noRoute85, noRoute47}, true, []int{85, 47}, 500},
		{"unrelated fallback 404 keeps existing behavior", responsesPath, []route{unavailable85, unknown404At47}, true, []int{85, 47}, 500},
		{"central 85 still settles only successful backup", responsesPath, []route{unavailable85, success47}, true, []int{85, 47}, 200},
		{"chat model route failure and exhausted backup", chatPath, []route{{channelID: 85, baseURL: centralURL, status: 502}, unavailable84, noRoute47}, false, []int{85, 84, 47}, 503},
		{"chat model cooldown and exhausted backup", chatPath, []route{unavailable85, unavailable84, noRoute47}, true, []int{85, 84, 47}, 503},
		{"chat backup route failure preserves central bad gateway", chatPath, []route{badGateway84, noRoute47}, false, []int{84, 47}, 502},
		{"chat unrelated fallback 404 keeps existing behavior", chatPath, []route{unavailable85, unknown404At47}, true, []int{85, 47}, 500},
		{"chat available backup settles once", chatPath, []route{unavailable85, unavailable84, success47}, true, []int{85, 84, 47}, 200},
		{"legacy completions keeps existing behavior", "/v1/completions", []route{unavailable84}, false, []int{84}, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupRelayRouterTestDB(t)
			retryTimes := common.RetryTimes
			common.RetryTimes = 3
			t.Cleanup(func() { common.RetryTimes = retryTimes })

			upstreams := make(map[string]route, len(tc.routes))
			for i, upstream := range tc.routes {
				t.Cleanup(func() { service.ClearChannelCooldown(upstream.channelID) })
				key := fmt.Sprintf("fixture-upstream-%d", upstream.channelID)
				channel := createRelayTestChannel(t, key, upstream.baseURL, int64(100-i), 100, modelName)
				moveRelayReselectFixtureChannel(t, channel, upstream.channelID)
				updates := map[string]any{"key": key}
				if upstream.baseURL == centralURL {
					updates["setting"] = `{"pass_through_body_enabled":true}`
				}
				require.NoError(t, model.DB.Model(channel).Updates(updates).Error)
				if upstream.cooling {
					coolDownRelayReselectFixtureChannel(t, channel, modelName)
				}
				upstreams["Bearer "+key] = upstream
			}
			createRelayTestUserAndToken(t, 1, tokenKey)
			common.MemoryCacheEnabled = tc.cache
			model.InitChannelCache()

			var attempts []int
			requestBody := `{"model":"gpt-4o-mini","input":"fixture","stream":true}`
			completed, contentType, successMarker := responsesDone, "text/event-stream", "response.completed"
			switch tc.path {
			case chatPath:
				requestBody = `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"fixture"}],"stream":false}`
				completed, contentType, successMarker = chatDone, "application/json", "fixture answer"
			case "/v1/completions":
				requestBody = `{"model":"gpt-4o-mini","prompt":"fixture"}`
			}
			client := service.GetHttpClient()
			priorTransport := client.Transport
			t.Cleanup(func() { client.Transport = priorTransport })
			client.Transport = krillReselectTransport(func(req *http.Request) (*http.Response, error) {
				upstream, ok := upstreams[req.Header.Get("Authorization")]
				if !ok || req.URL.String() != upstream.baseURL+tc.path {
					return nil, errors.New("unexpected fixture route")
				}
				attempts = append(attempts, upstream.channelID)
				if upstream.status == http.StatusOK {
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {contentType}},
						Body: io.NopCloser(strings.NewReader(completed))}, nil
				}
				code := upstream.code
				if code == "" {
					code = "unknown_error"
				}
				body := fmt.Sprintf(`{"error":{"type":"upstream_error","code":%q,"message":%q}}`, code, upstream.message+" fixture private message")
				return &http.Response{StatusCode: upstream.status, Header: http.Header{"Content-Type": {"application/json"}},
					Body: io.NopCloser(strings.NewReader(body))}, nil
			})

			engine := gin.New()
			engine.Use(middleware.RequestId())
			SetRelayRouter(engine)
			request := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(requestBody))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer sk-"+tokenKey)
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)

			if tc.want != http.StatusOK {
				// Refunds run asynchronously and must finish before closing the fixture database.
				require.Eventually(t, func() bool {
					var user model.User
					var token model.Token
					return model.DB.First(&user, 1).Error == nil && user.Quota == 1000000 &&
						model.DB.Where("user_id = ?", user.Id).First(&token).Error == nil && token.RemainQuota == 1000000
				}, 3*time.Second, 10*time.Millisecond)
			}
			require.Equal(t, tc.wantAttempts, attempts)
			require.Equal(t, tc.want, response.Code, response.Body.String())
			require.NotContains(t, response.Body.String(), "fixture private message")

			requestID := response.Header().Get(common.RequestIdKey)
			require.NotEmpty(t, requestID)
			var charges []model.Log
			require.NoError(t, model.LOG_DB.Where("request_id = ? AND type = ?", requestID, model.LogTypeConsume).Find(&charges).Error)
			if tc.want == http.StatusOK {
				require.Contains(t, response.Body.String(), successMarker)
				require.Len(t, charges, 1)
				require.Equal(t, 47, charges[0].ChannelId)
				require.Positive(t, charges[0].Quota)
			} else {
				require.Empty(t, charges)
				var finalEvent model.Log
				require.NoError(t, model.LOG_DB.Where("request_id = ? AND type = ? AND other LIKE ?",
					requestID, model.LogTypeSystem, `%"event_type":"final_error"%`).Take(&finalEvent).Error)
				require.Contains(t, finalEvent.Other, `"error_code":"get_channel_failed"`)
				require.Contains(t, finalEvent.Other, `"status_code":500`)
			}
		})
	}
}

func moveRelayReselectFixtureChannel(t *testing.T, channel *model.Channel, id int) {
	t.Helper()
	originalID := channel.Id
	require.NoError(t, model.DB.Model(channel).Update("id", id).Error)
	require.NoError(t, model.DB.Model(&model.Ability{}).
		Where("channel_id = ?", originalID).Update("channel_id", id).Error)
	require.Equal(t, id, channel.Id)
}

func coolDownRelayReselectFixtureChannel(t *testing.T, channel *model.Channel, modelName string) {
	t.Helper()
	enabled := common.AutomaticChannelCooldownEnabled
	threshold := common.ChannelCooldownFailureThreshold
	window := common.ChannelCooldownFailureWindowSeconds
	duration := common.ChannelCooldownSeconds
	t.Cleanup(func() {
		service.ClearChannelCooldown(channel.Id)
		common.AutomaticChannelCooldownEnabled = enabled
		common.ChannelCooldownFailureThreshold = threshold
		common.ChannelCooldownFailureWindowSeconds = window
		common.ChannelCooldownSeconds = duration
	})
	common.AutomaticChannelCooldownEnabled = true
	common.ChannelCooldownFailureThreshold = 1
	common.ChannelCooldownFailureWindowSeconds = 60
	common.ChannelCooldownSeconds = 300
	channelError := types.NewChannelError(channel.Id, channel.Type, channel.Name, false, "", false)
	upstreamError := types.NewErrorWithStatusCode(errors.New("fixture unavailable"), "unknown_error", http.StatusServiceUnavailable)
	service.RecordChannelFailureForCooldownWithModel(*channelError, upstreamError, modelName)
	require.True(t, service.IsChannelCoolingDown(channel.Id))
}
