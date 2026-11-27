package router

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
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

type cancelOnKrillEventRecorder struct {
	*httptest.ResponseRecorder
	event  string
	cancel context.CancelFunc
}

func (r *cancelOnKrillEventRecorder) Flush() {
	r.ResponseRecorder.Flush()
	if strings.Contains(r.Body.String(), "event: "+r.event+"\n") {
		r.cancel()
	}
}

func TestKrillResponsesStreamSettlementAndFailover(t *testing.T) {
	const (
		publicURL  = "https://sub2api.9gpt.net"
		privateURL = "http://sub2api-krill:8080"
		directURL  = "https://api.kbsfdxc.net/codex"
		backupURL  = "https://backup.invalid"
		modelName  = "gpt-4o-mini"
		tokenKey   = "krillstreamfixturetoken"
		quota      = 1000000
		partial    = "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"partial output\"}\n\n"
		failed     = "event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"error\":{\"code\":\"upstream_stream_incomplete\",\"message\":\"Upstream response ended before a terminal event\"}}}\n\n"
		completed  = "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":13,\"output_tokens\":5,\"total_tokens\":18}}}\n\n"
		incomplete = "event: response.incomplete\ndata: {\"type\":\"response.incomplete\",\"response\":{\"status\":\"incomplete\",\"incomplete_details\":{\"reason\":\"max_output_tokens\"}}}\n\n"
	)
	for _, tc := range []struct {
		name         string
		primaryID    int
		primaryURL   string
		primary      string
		wantBackup   bool
		wantRefund   bool
		wantResponse string
		readError    bool
		cancelOn     string
	}{
		{"84 legacy endpoint failure", 84, publicURL, partial + failed, false, true, partial + failed, false, ""},
		{"84 private endpoint failure", 84, privateURL, partial + failed, false, true, partial + failed, false, ""},
		{"85 private endpoint failure", 85, privateURL, partial + failed, false, true, partial + failed, false, ""},
		{"47 client closes on failure", 47, directURL, partial + failed, false, true, partial + failed, false, "response.failed"},
		{"84 client closes on failure", 84, privateURL, partial + failed, false, true, partial + failed, false, "response.failed"},
		{"85 client closes on failure", 85, privateURL, partial + failed, false, true, partial + failed, false, "response.failed"},
		{"85 client cancels without failure", 85, privateURL, partial, false, false, partial, false, "response.output_text.delta"},
		{"47 disconnect after output", 47, directURL, partial, false, true, "", false, ""},
		{"47 read failure after output", 47, directURL, partial, false, true, "", true, ""},
		{"85 disconnect before output", 85, privateURL, failed, true, false, completed, false, ""},
		{"47 disconnect before output", 47, directURL, "", true, false, completed, false, ""},
		{"85 completed", 85, privateURL, partial + completed, false, false, partial + completed, false, ""},
		{"47 legitimate incomplete", 47, directURL, partial + incomplete, false, false, partial + incomplete, false, ""},
		{"85 with another provider", 85, "https://other.invalid", partial + failed, false, false, partial + failed, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupRelayRouterTestDB(t)
			oldRetry, oldErrorLog := common.RetryTimes, constant.ErrorLogEnabled
			common.RetryTimes, constant.ErrorLogEnabled = 2, true
			t.Cleanup(func() { common.RetryTimes, constant.ErrorLogEnabled = oldRetry, oldErrorLog })

			primary := createRelayTestChannel(t, "krill-fixture", tc.primaryURL, 1000, 100, modelName)
			originalChannelID := primary.Id
			require.NoError(t, model.DB.Model(primary).Update("id", tc.primaryID).Error)
			require.NoError(t, model.DB.Model(&model.Ability{}).Where("channel_id = ?", originalChannelID).Update("channel_id", tc.primaryID).Error)
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
				var reader io.Reader = strings.NewReader(body)
				if tc.readError && req.URL.Host != "backup.invalid" {
					reader = io.MultiReader(reader, iotest.ErrReader(io.ErrUnexpectedEOF))
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}},
					Body: io.NopCloser(reader)}, nil
			})
			engine := gin.New()
			engine.Use(middleware.RequestId())
			SetRelayRouter(engine)
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-4o-mini","input":"`+tc.name+`","stream":true}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer sk-"+tokenKey)
			rec := httptest.NewRecorder()
			if tc.cancelOn != "" {
				ctx, cancel := context.WithCancel(req.Context())
				defer cancel()
				engine.ServeHTTP(&cancelOnKrillEventRecorder{ResponseRecorder: rec, event: tc.cancelOn, cancel: cancel}, req.WithContext(ctx))
				require.ErrorIs(t, ctx.Err(), context.Canceled)
			} else {
				engine.ServeHTTP(rec, req)
			}
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			if tc.wantResponse != "" {
				require.Equal(t, tc.wantResponse, rec.Body.String())
			} else {
				body := rec.Body.String()
				require.True(t, strings.HasPrefix(body, partial+"event: response.failed\ndata: "))
				data := strings.TrimPrefix(body, partial+"event: response.failed\ndata: ")
				require.True(t, strings.HasSuffix(data, "\n\n"))
				var event dto.ResponsesStreamResponse
				require.NoError(t, common.UnmarshalJsonStr(data, &event))
				require.Equal(t, "response.failed", event.Type)
				require.NotNil(t, event.Response)
				require.NotNil(t, event.Response.GetOpenAIError())
				require.Equal(t, "upstream_stream_incomplete", event.Response.GetOpenAIError().Code)
			}
			wantURLs := []string{tc.primaryURL + "/v1/responses"}
			settlementChannel := tc.primaryID
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
				require.Equal(t, tc.primaryID, failure[0].ChannelId)
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
