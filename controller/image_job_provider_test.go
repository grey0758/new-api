package controller

import (
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/require"
)

func TestImageJobProviderBusyPersistsOnlyForRefundedExplicitFlowRejection(t *testing.T) {
	for _, tc := range []struct {
		name, detail, code string
		flow               bool
		status             int
	}{
		{"exact_flow_busy", "Google RPC rejection: PUBLIC_ERROR_HIGH_TRAFFIC", "flow_provider_high_traffic", true, 500},
		{"wrapped_flow_busy", "生成失败: Google RPC rejection: PUBLIC_ERROR_HIGH_TRAFFIC", "flow_provider_high_traffic", true, 500},
		{"generic_500", "upstream server unavailable", "bad_response_status_code", true, 500},
		{"mixed_reason", "Google RPC rejection: PUBLIC_ERROR_HIGH_TRAFFIC trailing", "bad_response_status_code", true, 500},
		{"other_provider", "Google RPC rejection: PUBLIC_ERROR_HIGH_TRAFFIC", "bad_response_status_code", false, 500},
		{"different_status", "Google RPC rejection: PUBLIC_ERROR_HIGH_TRAFFIC", "bad_response_status_code", true, 503},
		{"wrapped_other_provider", "生成失败: Google RPC rejection: PUBLIC_ERROR_HIGH_TRAFFIC", "bad_response_status_code", false, 500},
		{"wrapped_other_reason", "生成失败: Google RPC rejection: PUBLIC_ERROR_UNUSUAL_ACTIVITY", "bad_response_status_code", true, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			router, user, token := imageJobFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				b, _ := common.Marshal(map[string]string{"detail": tc.detail})
				_, _ = w.Write(b)
			}))
			if tc.flow {
				t.Setenv("IMAGE_JOBS_FLOW_CHANNEL_ID", "90001")
			}
			body := `{"model":"fixture-4k","prompt":"fixture","n":1}`
			w := jobRequest(router, token, "POST", "/v1/images/generations", body, "unique", true)
			require.Equal(t, 202, w.Code, w.Body.String())
			job, err := model.ClaimImageJob("owner", "4k", time.Now().Unix())
			require.NoError(t, err)
			executeImageJob(router, job)
			current, err := model.FindImageJob(user.Id, job.ID)
			require.NoError(t, err)
			require.Equal(t, "failed", current.Status)
			require.Equal(t, "refunded", current.BillingStatus)
			require.Equal(t, tc.code, current.ErrorCode)
			require.Empty(t, current.ResultHash)
			require.Zero(t, current.ResultBytes)
			status := jobRequest(router, token, "GET", "/v1/images/jobs/"+job.ID, "", "", false)
			require.Equal(t, 200, status.Code)
			require.Contains(t, status.Body.String(), fmt.Sprintf(`"error_code":%q`, tc.code))
			var after model.User
			require.NoError(t, model.DB.First(&after, user.Id).Error)
			require.Equal(t, user.Quota, after.Quota)
			// An idempotent submission observes the same terminal receipt without work.
			require.Equal(t, 202, jobRequest(router, token, "POST", "/v1/images/generations", body, "unique", true).Code)
			require.Equal(t, int32(1), calls.Load())
			result := jobRequest(router, token, "GET", "/v1/images/jobs/"+job.ID+"/result", "", "", false)
			require.NotEqual(t, http.StatusOK, result.Code)
			_, _ = io.ReadAll(result.Result().Body)
		})
	}
}

func TestImageJobBusyNeverDowngradesUnknownOrBillingEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, billing, wantStatus, wantCode string
		rejected, checkpoint                bool
	}{
		{"refund", "refunded", "failed", "flow_provider_high_traffic", true, false},
		{"not_refunded", "reserved", "reconciliation_required", "billing_reconciliation_required", true, false},
		{"not_started", "not_started", "failed", "image_generation_failed", true, false},
		{"checkpoint_failed", "refunded", "reconciliation_required", "provider_result_unconfirmed", true, true},
		{"outcome_unknown", "refunded", "reconciliation_required", "provider_result_unconfirmed", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router, user, token := imageJobFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("no upstream expected") }))
			w := jobRequest(router, token, "POST", "/v1/images/generations", `{"model":"fixture-4k","prompt":"fixture","n":1}`, "unique", true)
			require.Equal(t, 202, w.Code)
			job, err := model.ClaimImageJob("owner", "4k", time.Now().Unix())
			require.NoError(t, err)
			executeImageJob(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				e := service.ImageJobExecutionFrom(r.Context())
				e.ProviderRejected = tc.rejected
				e.CheckpointFailed = tc.checkpoint
				e.ProviderRejectionCode = "flow_provider_high_traffic"
				require.NoError(t, model.UpdateOwnedImageJob(job.ID, job.Owner, map[string]any{"provider_started": true, "billing_status": tc.billing}))
				w.WriteHeader(500)
			}), job)
			current, err := model.FindImageJob(user.Id, job.ID)
			require.NoError(t, err)
			require.Equal(t, tc.wantStatus, current.Status)
			require.Equal(t, tc.wantCode, current.ErrorCode)
		})
	}
}

func TestImageJobProviderBusyFlowOpenAIEnvelope(t *testing.T) {
	for _, message := range []string{
		"Google RPC rejection: PUBLIC_ERROR_HIGH_TRAFFIC",
		"生成失败: Google RPC rejection: PUBLIC_ERROR_HIGH_TRAFFIC",
	} {
		t.Run(message, func(t *testing.T) {
			testImageJobProviderBusyFlowOpenAIEnvelope(t, message)
		})
	}
}

func testImageJobProviderBusyFlowOpenAIEnvelope(t *testing.T, message string) {
	var calls atomic.Int32
	router, user, token := imageJobFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		body, err := common.Marshal(map[string]any{"error": map[string]any{
			"message": message, "type": "server_error", "code": "generation_failed", "status_code": 500,
		}})
		require.NoError(t, err)
		_, _ = w.Write(body)
	}))
	t.Setenv("IMAGE_JOBS_FLOW_CHANNEL_ID", "90001")
	w := jobRequest(router, token, "POST", "/v1/images/generations", `{"model":"fixture-4k","prompt":"fixture","n":1}`, "unique", true)
	require.Equal(t, 202, w.Code)
	job, err := model.ClaimImageJob("owner", "4k", time.Now().Unix())
	require.NoError(t, err)
	executeImageJob(router, job)
	current, err := model.FindImageJob(user.Id, job.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", current.Status)
	require.Equal(t, "refunded", current.BillingStatus)
	require.Equal(t, "flow_provider_high_traffic", current.ErrorCode)
	require.Empty(t, current.ResultHash)
	require.Equal(t, int32(1), calls.Load())
}
