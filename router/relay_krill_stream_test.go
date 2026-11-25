package router

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type krillFixtureTransport func(*http.Request) (*http.Response, error)

func (transport krillFixtureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return transport(req)
}

func TestKrillResponsesStreamSettlementAndFailover(t *testing.T) {
	const (
		primaryURL = "https://sub2api.9gpt.net"
		backupURL  = "https://backup.invalid"
		modelName  = "gpt-4o-mini"
		tokenKey   = "krillstreamfixturetoken"
		quota      = 1000000
		partial    = "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"partial output\"}\n\n"
		failed     = "event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"error\":{\"code\":\"upstream_stream_incomplete\",\"message\":\"Upstream response ended before a terminal event\"}}}\n\n"
		completed  = "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":13,\"output_tokens\":5,\"total_tokens\":18}}}\n\n"
	)
	for _, tc := range []struct {
		name         string
		primary      string
		wantBackup   bool
		wantRefund   bool
		wantResponse string
	}{
		{"disconnect after output", partial + failed, false, true, partial + failed},
		{"disconnect before output", failed, true, false, completed},
		{"completed", partial + completed, false, false, partial + completed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupRelayRouterTestDB(t)
			oldRetry, oldErrorLog := common.RetryTimes, constant.ErrorLogEnabled
			common.RetryTimes, constant.ErrorLogEnabled = 2, true
			t.Cleanup(func() { common.RetryTimes, constant.ErrorLogEnabled = oldRetry, oldErrorLog })

			primary := createRelayTestChannel(t, "krill-fixture", primaryURL, 1000, 100, modelName)
			originalChannelID := primary.Id
			require.NoError(t, model.DB.Model(primary).Update("id", 84).Error)
			require.NoError(t, model.DB.Model(&model.Ability{}).Where("channel_id = ?", originalChannelID).Update("channel_id", 84).Error)
			backup := createRelayTestChannel(t, "backup-fixture", backupURL, 100, 100, modelName)
			createRelayTestUserAndToken(t, 1, tokenKey)
			var user model.User
			require.NoError(t, model.DB.First(&user, 1).Error)
			user.SetSetting(dto.UserSetting{BillingPreference: "subscription_only"})
			require.NoError(t, model.DB.Model(&user).Update("setting", user.Setting).Error)
			require.NoError(t, model.DB.Model(&model.Token{}).Where("user_id = ?", user.Id).Update("unlimited_quota", false).Error)
			plan := model.SubscriptionPlan{Title: "Stream Fixture", TotalAmount: quota}
			require.NoError(t, model.DB.Select("Title", "PriceAmount", "TotalAmount").Create(&plan).Error)
			sub := model.UserSubscription{UserId: user.Id, PlanId: plan.Id, AmountTotal: quota, AmountUsed: 7,
				StartTime: time.Now().Add(-time.Hour).Unix(), EndTime: time.Now().Add(time.Hour).Unix(), Status: "active"}
			require.NoError(t, model.DB.Create(&sub).Error)

			var upstreamURLs []string
			client := service.GetHttpClient()
			originalTransport := client.Transport
			t.Cleanup(func() { client.Transport = originalTransport })
			client.Transport = krillFixtureTransport(func(req *http.Request) (*http.Response, error) {
				upstreamURLs = append(upstreamURLs, req.URL.String())
				body := tc.primary
				if req.URL.Host == "backup.invalid" {
					body = completed
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}},
					Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			engine := gin.New()
			engine.Use(middleware.RequestId())
			SetRelayRouter(engine)
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-4o-mini","input":"`+tc.name+`","stream":true}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer sk-"+tokenKey)
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Equal(t, tc.wantResponse, rec.Body.String())
			wantURLs := []string{primaryURL + "/v1/responses"}
			settlementChannel := 84
			if tc.wantBackup {
				wantURLs = append(wantURLs, backupURL+"/v1/responses")
				settlementChannel = backup.Id
			}
			require.Equal(t, wantURLs, upstreamURLs)
			requestID := rec.Header().Get(common.RequestIdKey)
			var consumption []model.Log
			require.NoError(t, model.LOG_DB.Where("request_id = ? AND type = ?", requestID, model.LogTypeConsume).Find(&consumption).Error)
			var precharge model.SubscriptionPreConsumeRecord
			if tc.wantRefund {
				require.Empty(t, consumption)
				require.Eventually(t, func() bool {
					var token model.Token
					return model.DB.Where("request_id = ?", requestID).First(&precharge).Error == nil && precharge.Status == "refunded" &&
						model.DB.Where("user_id = ?", user.Id).First(&token).Error == nil && token.RemainQuota == quota
				}, 2*time.Second, 10*time.Millisecond)
				require.NoError(t, model.DB.First(&sub, sub.Id).Error)
				require.EqualValues(t, 7, sub.AmountUsed)
				var failure []model.Log
				require.NoError(t, model.LOG_DB.Where("request_id = ? AND type = ?", requestID, model.LogTypeError).Find(&failure).Error)
				require.Len(t, failure, 1)
				require.Equal(t, 84, failure[0].ChannelId)
				require.Contains(t, failure[0].Other, `"error_code":"responses_stream_incomplete"`)
				require.Contains(t, failure[0].Other, `"status_code":502`)
			} else {
				require.Len(t, consumption, 1)
				require.Positive(t, consumption[0].Quota)
				require.Equal(t, settlementChannel, consumption[0].ChannelId)
				require.NoError(t, model.DB.Where("request_id = ?", requestID).First(&precharge).Error)
				require.Equal(t, "consumed", precharge.Status)
				require.NoError(t, model.DB.First(&sub, sub.Id).Error)
				require.EqualValues(t, 7+consumption[0].Quota, sub.AmountUsed)
			}
			require.Positive(t, precharge.PreConsumed)
		})
	}
}
