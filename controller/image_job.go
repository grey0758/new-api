package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const imageJobInputKey = "image_job_input"

// Initialized at startup, before serving requests. Tests may restore it while
// no executor is running; live configuration changes require a drained restart.
var imageJobCapacity = service.ImageJobCapacity{Limits: model.ImageJobLimits{Pending: 48, PerUser: 8, PendingBytes: 128 << 20, StoredBytes: 1 << 30, Retained: 10000}, StandardWorkers: 4, FourKWorkers: 1}

type imageJobInput struct {
	Body                  []byte
	KeyHash, Hash, Family string
}

func wantsAsyncImage(c *gin.Context) bool {
	for _, preference := range strings.Split(c.GetHeader("Prefer"), ",") {
		if strings.EqualFold(strings.TrimSpace(strings.SplitN(preference, ";", 2)[0]), "respond-async") {
			return true
		}
	}
	return false
}
func imageJobError(c *gin.Context, status int, code string) {
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"code": code, "message": strings.ReplaceAll(code, "_", " "), "type": "image_job_error"}})
}

// Called before distribution: a repeat must return its existing job even when
// all channels have become busy since the initial request was admitted.
func ImageJobLookup(c *gin.Context) {
	if !wantsAsyncImage(c) || service.IsImageJob(c) {
		c.Next()
		return
	}
	if !service.ImageJobsEnabled() {
		imageJobError(c, 503, "image_jobs_disabled")
		return
	}
	media, _, err := mime.ParseMediaType(c.ContentType())
	if err != nil || media != "application/json" {
		imageJobError(c, 415, "image_jobs_require_json")
		return
	}
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if key == "" || len(key) > 200 || strings.IndexFunc(key, func(r rune) bool { return r < 33 || r > 126 }) >= 0 {
		imageJobError(c, 400, "invalid_idempotency_key")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, service.ImageJobMaxRequestBytes)
	storage, err := common.GetBodyStorage(c)
	if err != nil || storage.Size() > service.ImageJobMaxRequestBytes {
		imageJobError(c, 413, "image_job_request_too_large")
		return
	}
	body, err := storage.Bytes()
	if err != nil {
		imageJobError(c, 400, "invalid_image_job_request")
		return
	}
	var payload map[string]json.RawMessage
	if common.Unmarshal(body, &payload) != nil || payload == nil {
		imageJobError(c, 400, "invalid_image_job_request")
		return
	}
	if stream, ok := payload["stream"]; ok && !bytes.Equal(bytes.TrimSpace(stream), []byte("false")) {
		imageJobError(c, 400, "image_jobs_do_not_stream")
		return
	}
	// Canonical JSON for comparison only. Original bytes/order/explicit zeros
	// are retained for execution; references are never reordered or downloaded.
	canonical, err := common.Marshal(payload)
	if err != nil {
		imageJobError(c, 400, "invalid_image_job_request")
		return
	}
	input := &imageJobInput{Body: body, KeyHash: service.ImageJobDigest([]byte(key)), Hash: service.ImageJobDigest(canonical), Family: "standard"}
	var name, size string
	_ = common.Unmarshal(payload["model"], &name)
	_ = common.Unmarshal(payload["image_size"], &size)
	var quality string
	_ = common.Unmarshal(payload["quality"], &quality)
	var config struct {
		ImageConfig struct {
			ImageSize string `json:"imageSize"`
		} `json:"imageConfig"`
	}
	_ = common.Unmarshal(payload["generationConfig"], &config)
	if strings.Contains(strings.ToLower(name), "4k") || strings.EqualFold(size, "4K") || strings.EqualFold(config.ImageConfig.ImageSize, "4K") || strings.EqualFold(quality, "high") || strings.EqualFold(quality, "hd") || strings.EqualFold(quality, "ultra") {
		input.Family = "4k"
	}
	existing, lookupErr := model.FindImageJobByKey(c.GetInt("id"), input.KeyHash)
	err = lookupErr
	if err == nil {
		if existing.RequestHash != input.Hash || existing.TokenID != c.GetInt("token_id") {
			imageJobError(c, 409, "idempotency_key_conflict")
			return
		}
		c.Header("X-Idempotent-Replay", "true")
		writeImageJobReceipt(c, existing, 202)
		c.Abort()
		return
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		imageJobError(c, 503, "image_job_storage_unavailable")
		return
	}
	c.Set(imageJobInputKey, input)
	c.Next()
}

func ImageGeneration(c *gin.Context) {
	value, async := c.Get(imageJobInputKey)
	if !async {
		Relay(c, types.RelayFormatOpenAIImage)
		return
	}
	input := value.(*imageJobInput)
	if service.IsFlowImageJobChannel(c.GetInt("channel_id")) {
		if _, err := service.BuildFlowImageJobRequest(c, c.GetString("original_model")); err != nil {
			imageJobError(c, 400, "unsupported_flow_image_request")
			return
		}
	}
	request, err := helper.GetAndValidateRequest(c, types.RelayFormatOpenAIImage)
	if err != nil {
		imageJobError(c, 400, "invalid_image_job_request")
		return
	}
	info, err := relaycommon.GenRelayInfo(c, types.RelayFormatOpenAIImage, request, nil)
	if err != nil {
		imageJobError(c, 400, "invalid_image_job_request")
		return
	}
	meta := request.GetTokenCountMeta()
	tokens, err := service.EstimateRequestToken(c, meta, info)
	if err != nil {
		imageJobError(c, 400, "image_job_quote_failed")
		return
	}
	info.SetEstimatePromptTokens(tokens)
	price, err := helper.ModelPriceHelper(c, info, tokens, meta)
	if err != nil {
		imageJobError(c, 400, "image_job_quote_failed")
		return
	}
	// Admission checks use existing quote and funding preference. Full current
	// auth/quote/reservation is repeated by the normal Relay immediately before
	// dispatch. No wallet mutation or CAPTCHA occurs while queued.
	if !price.FreeModel {
		if !c.GetBool("token_unlimited_quota") && c.GetInt("token_quota") < price.QuotaToPreConsume {
			imageJobError(c, 403, "insufficient_token_quota")
			return
		}
		quota, qerr := model.GetUserQuota(info.UserId, false)
		hasSub, serr := model.HasActiveUserSubscription(info.UserId)
		if qerr != nil || serr != nil {
			imageJobError(c, 503, "image_job_quote_unavailable")
			return
		}
		pref := common.NormalizeBillingPreference(info.UserSetting.BillingPreference)
		if (pref == "wallet_only" && quota < price.QuotaToPreConsume) || (pref == "subscription_only" && !hasSub) || (!hasSub && quota < price.QuotaToPreConsume) {
			imageJobError(c, 403, "insufficient_user_quota")
			return
		}
	}
	now := time.Now().Unix()
	job := &model.ImageJob{ID: model.NewImageJobID(), UserID: info.UserId, TokenID: info.TokenId, KeyHash: input.KeyHash, RequestHash: input.Hash, RequestID: c.GetString(common.RequestIdKey), Model: info.OriginModelName, Family: input.Family, Status: "queued", ClientIP: c.ClientIP(), RequestBody: input.Body, RequestBytes: int64(len(input.Body)), BillingStatus: "not_started", CreatedAt: now, QueueExpiresAt: now + 900, ExpiresAt: now + 7*86400}
	if specific := c.GetString("specific_channel_id"); specific != "" {
		job.TokenSuffix = specific
	}
	limits := imageJobCapacity.Limits
	job, created, err := model.AdmitImageJob(job, limits)
	if errors.Is(err, model.ErrImageJobConflict) {
		imageJobError(c, 409, "idempotency_key_conflict")
		return
	}
	if errors.Is(err, model.ErrImageJobFull) {
		var capacity *model.ImageJobCapacityError
		if errors.As(err, &capacity) {
			c.Header("X-Image-Queue-Limit", capacity.Reason)
			common.SysLog(fmt.Sprintf("image_job_capacity_rejected: reason=%s current=%d limit=%d family=%s", capacity.Reason, capacity.Current, capacity.Limit, input.Family))
		}
		c.Header("Retry-After", "5")
		imageJobError(c, 429, "image_job_queue_full")
		return
	}
	if err != nil {
		imageJobError(c, 503, "image_job_storage_unavailable")
		return
	}
	if !created {
		c.Header("X-Idempotent-Replay", "true")
	}
	writeImageJobReceipt(c, job, 202)
}

func writeImageJobReceipt(c *gin.Context, job *model.ImageJob, status int) {
	c.Header("Location", "/v1/images/jobs/"+job.ID)
	c.Header("Preference-Applied", "respond-async")
	c.Header("Retry-After", "2")
	c.Header("Cache-Control", "no-store")
	c.Header("X-Image-Job-Id", job.ID)
	c.JSON(status, gin.H{"id": job.ID, "object": "image.job", "status": job.Status, "model": job.Model, "request_id": job.RequestID, "created_at": job.CreatedAt, "started_at": job.StartedAt, "finished_at": job.FinishedAt, "queue_expires_at": job.QueueExpiresAt, "expires_at": job.ExpiresAt, "result_expires_at": job.ResultExpiresAt, "result_url": "/v1/images/jobs/" + job.ID + "/result", "result_available": job.ResultBytes > 0 && job.ResultExpiresAt > time.Now().Unix(), "billing_status": job.BillingStatus, "error_code": job.ErrorCode})
}

func FetchImageJob(c *gin.Context) {
	if !service.ImageJobsEnabled() {
		imageJobError(c, 503, "image_jobs_disabled")
		return
	}
	lookup := model.FindImageJobMetadata
	if strings.HasSuffix(c.Request.URL.Path, "/result") {
		lookup = model.FindImageJob
	}
	job, err := lookup(c.GetInt("id"), c.Param("job_id"))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		imageJobError(c, 404, "image_job_not_found")
		return
	}
	if err != nil {
		imageJobError(c, 503, "image_job_storage_unavailable")
		return
	}
	// Token scope prevents a weaker key for the same user from reading images
	// created with another key/model permission set.
	if job.TokenID != c.GetInt("token_id") {
		imageJobError(c, 404, "image_job_not_found")
		return
	}
	if c.Request.Method == http.MethodDelete {
		ok, err := model.CancelImageJob(job.UserID, job.ID)
		if err != nil {
			imageJobError(c, 503, "image_job_storage_unavailable")
			return
		}
		if !ok {
			imageJobError(c, 409, "image_job_already_started")
			return
		}
		job.Status = "cancelled"
		writeImageJobReceipt(c, job, 200)
		return
	}
	if strings.HasSuffix(c.Request.URL.Path, "/result") {
		if job.ResultExpiresAt > 0 && job.ResultExpiresAt <= time.Now().Unix() {
			imageJobError(c, 410, "image_job_result_expired")
			return
		}
		if len(job.ResultBody) > 0 {
			c.Header("Cache-Control", "no-store")
			c.Header("X-Image-Job-Id", job.ID)
			c.Header("X-Billing-Status", job.BillingStatus)
			c.Data(200, "application/json", job.ResultBody)
			return
		}
		if job.Status == "queued" || job.Status == "running" {
			writeImageJobReceipt(c, job, 202)
			return
		}
		imageJobError(c, 409, "image_job_has_no_result")
		return
	}
	writeImageJobReceipt(c, job, 200)
}

// Small bounded worker pool outside HTTP lifecycle. The existing DB-backed
// background leader owns claims; CAS is a second line of duplicate protection.
func StartImageJobExecutor(handler http.Handler) error {
	if !service.ImageJobsEnabled() {
		return nil
	}
	capacity, err := service.LoadImageJobCapacity()
	if err != nil {
		return err
	}
	imageJobCapacity = capacity
	if err := model.InitImageJobs(); err != nil {
		return err
	}
	owner := common.GetUUID()
	go func() {
		wasLeader := false
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for range ticker.C {
			leader := model.IsBackgroundTaskLeader()
			if leader {
				if err := model.MaintainImageJobs(owner, !wasLeader, time.Now().Unix()); err != nil {
					common.SysError("image_job_maintenance_failed")
					continue
				}
			}
			wasLeader = leader
		}
	}()
	startImageJobWorkers(context.Background(), imageJobCapacity, time.Second, model.IsBackgroundTaskLeader, func(family string) {
		job, err := model.ClaimImageJob(owner, family, time.Now().Unix())
		if err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				common.SysError("image_job_claim_failed")
			}
			return
		}
		executeImageJob(handler, job)
	})
	common.SysLog(fmt.Sprintf("image_jobs_enabled: workers_standard=%d workers_4k=%d pending=%d per_user=%d request_max=%d queue_bytes=%d result_bytes=%d retained=%d", capacity.StandardWorkers, capacity.FourKWorkers, capacity.Limits.Pending, capacity.Limits.PerUser, service.ImageJobMaxRequestBytes, capacity.Limits.PendingBytes, capacity.Limits.StoredBytes, capacity.Limits.Retained))
	return nil
}

// Each synchronous run owns its slot until execution and billing finish. Empty
// queues poll at a bounded rate and cancellation stops further claims.
func startImageJobWorkers(ctx context.Context, capacity service.ImageJobCapacity, interval time.Duration, isLeader func() bool, run func(string)) {
	for _, pool := range []struct {
		family string
		count  int
	}{{"standard", capacity.StandardWorkers}, {"4k", capacity.FourKWorkers}} {
		for i := 0; i < pool.count; i++ {
			go func(family string) {
				ticker := time.NewTicker(interval)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						if ctx.Err() != nil {
							return
						}
						if isLeader() {
							run(family)
						}
					}
				}
			}(pool.family)
		}
	}
}

type imageJobResponseWriter struct {
	header http.Header
	status int
}

func (w *imageJobResponseWriter) Header() http.Header { return w.header }
func (w *imageJobResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *imageJobResponseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	return len(body), nil
}
func (w *imageJobResponseWriter) Flush() {}

func executeImageJob(handler http.Handler, job *model.ImageJob) {
	// A panic or process kill keeps a durable, unreplayed reconciliation record.
	defer func() {
		if recover() != nil {
			_ = model.UpdateOwnedImageJob(job.ID, job.Owner, map[string]any{"status": "reconciliation_required", "error_code": "executor_panic", "finished_at": time.Now().Unix(), "request_body": nil, "request_bytes": 0})
		}
	}()
	token, err := model.GetTokenById(job.TokenID)
	if err != nil || token.UserId != job.UserID {
		_ = model.UpdateOwnedImageJob(job.ID, job.Owner, map[string]any{"status": "failed", "error_code": "execution_auth_revoked", "finished_at": time.Now().Unix(), "request_body": nil, "request_bytes": 0})
		return
	}
	execution := &service.ImageJobExecution{ID: job.ID, Owner: job.Owner, RequestID: job.RequestID}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	req, _ := http.NewRequestWithContext(service.WithImageJob(ctx, execution), http.MethodPost, "/v1/images/generations", bytes.NewReader(job.RequestBody))
	req.Host = "image-job.internal"
	req.RemoteAddr = net.JoinHostPort(job.ClientIP, "0")
	key := "sk-" + token.Key
	if job.TokenSuffix != "" {
		key += "-" + job.TokenSuffix
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	// Stable namespace avoids collisions with ordinary synchronous archive keys.
	req.Header.Set("Idempotency-Key", job.ID)
	w := &imageJobResponseWriter{header: make(http.Header)}
	handler.ServeHTTP(w, req)
	current, err := model.FindImageJobMetadata(job.UserID, job.ID)
	if err != nil {
		return
	}
	status, code := "failed", "image_generation_failed"
	if execution.FinalCode != "" {
		code = execution.FinalCode
	}
	if current.ResultHash != "" && current.ResultBytes > 0 {
		status, code = "completed", ""
		if current.BillingStatus != "settled" || execution.CheckpointFailed {
			status, code = "reconciliation_required", "billing_reconciliation_required"
		}
	} else if (current.ProviderStarted && !execution.ProviderRejected) || execution.CheckpointFailed {
		status, code = "reconciliation_required", "provider_result_unconfirmed"
	} else if current.BillingStatus != "not_started" && current.BillingStatus != "refunded" && current.BillingStatus != "settled" {
		status, code = "reconciliation_required", "billing_reconciliation_required"
	}
	if err := model.UpdateOwnedImageJob(job.ID, job.Owner, map[string]any{"status": status, "error_code": code, "http_status": w.status, "finished_at": time.Now().Unix(), "request_body": nil, "request_bytes": 0}); err != nil {
		common.SysError(fmt.Sprintf("image_job_finish_failed: %s", job.ID))
	}
}

var _ io.Writer = (*imageJobResponseWriter)(nil)
