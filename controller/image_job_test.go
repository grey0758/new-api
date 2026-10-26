package controller

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func imageJobFixture(t *testing.T, upstream http.Handler) (*gin.Engine, *model.User, *model.Token) {
	t.Helper()
	t.Setenv("IMAGE_JOBS_ENABLED", "true")
	t.Setenv("OPENCODEX_GENERATED_ASSETS_ENABLED", "false")
	gin.SetMode(gin.TestMode)
	oldDB, oldLog := model.DB, model.LOG_DB
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:job_controller_%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	conn, err := db.DB()
	require.NoError(t, err)
	conn.SetMaxOpenConns(1)
	model.DB = db
	model.LOG_DB = db
	t.Cleanup(func() { model.DB = oldDB; model.LOG_DB = oldLog; _ = conn.Close() })
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true
	common.UsingSQLite = true
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Log{}, &model.UserSubscription{}, &model.SubscriptionPlan{}, &model.SubscriptionPreConsumeRecord{}))
	require.NoError(t, model.InitImageJobs())
	user := &model.User{Id: 90001, Username: "fixture", Quota: 1000000, Group: "default", Status: common.UserStatusEnabled, Setting: `{"billing_preference":"wallet_only"}`}
	token := &model.Token{Id: 90001, UserId: user.Id, Key: strings.Repeat("a", 48), RemainQuota: 1000000, Status: common.TokenStatusEnabled, ExpiredTime: -1}
	require.NoError(t, db.Create(user).Error)
	require.NoError(t, db.Create(token).Error)
	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)
	settings := `{"pass_through_body_enabled":true}`
	channel := &model.Channel{Id: 90001, Type: constant.ChannelTypeOpenAI, Name: "local-fixture", Key: "fixture", BaseURL: &server.URL, Status: common.ChannelStatusEnabled, Setting: &settings, Models: "fixture-4k"}
	require.NoError(t, db.Create(channel).Error)
	previousPrices := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() { _ = ratio_setting.UpdateModelPriceByJSONString(previousPrices) })
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"fixture-4k":0.01}`))
	service.InitHttpClient()
	router := gin.New()
	router.Use(middleware.BodyStorageCleanup(), middleware.RequestId())
	router.POST("/v1/images/generations", middleware.TokenAuth(), ImageJobLookup, func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
		require.Nil(t, middleware.SetupContextForSelectedChannel(c, channel, "fixture-4k"))
		c.Next()
	}, ImageGeneration)
	queries := router.Group("/v1/images/jobs", middleware.ImageJobReadAuth())
	queries.GET("/:job_id", FetchImageJob)
	queries.GET("/:job_id/result", FetchImageJob)
	queries.DELETE("/:job_id", FetchImageJob)
	return router, user, token
}

func jobRequest(router http.Handler, token *model.Token, method, path, body, key string, async bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = "192.0.2.9:12345"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer sk-"+token.Key)
	if async {
		req.Header.Set("Prefer", "respond-async")
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestImageJobAsyncRealRelayBillingDisconnectAndReplay(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	seenBody := make(chan []byte, 1)
	router, user, token := imageJobFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		seenBody <- body
		close(started)
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"created":123,"data":[{"url":"https://fixture.invalid/image4096.png"}],"fixture_extra":"preserve"}`)
	}))
	body := `{"model":"fixture-4k","prompt":"fixture","n":1,"seed":0,"image_size":"4K","image_urls":["https://fixture.invalid/first","https://fixture.invalid/second"]}`
	before := time.Now()
	submitted := jobRequest(router, token, "POST", "/v1/images/generations", body, "unique", true)
	require.Equal(t, 202, submitted.Code, submitted.Body.String())
	require.Less(t, time.Since(before), time.Second)
	require.Equal(t, int32(0), calls.Load())
	var receipt struct {
		ID string `json:"id"`
	}
	require.NoError(t, common.Unmarshal(submitted.Body.Bytes(), &receipt))
	require.NotEmpty(t, receipt.ID)
	job, err := model.ClaimImageJob("owner", "4k", time.Now().Unix())
	require.NoError(t, err)
	done := make(chan struct{})
	go func() { executeImageJob(router, job); close(done) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream never started")
	}
	// The submit HTTP response is already gone. Background work remains live.
	repeated := jobRequest(router, token, "POST", "/v1/images/generations", body, "unique", true)
	require.Equal(t, 202, repeated.Code)
	require.Equal(t, "true", repeated.Header().Get("X-Idempotent-Replay"))
	conflict := jobRequest(router, token, "POST", "/v1/images/generations", strings.Replace(body, "fixture\"", "changed\"", 1), "unique", true)
	require.Equal(t, 409, conflict.Code)
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("job did not finish")
	}
	result := jobRequest(router, token, "GET", "/v1/images/jobs/"+receipt.ID+"/result", "", "", false)
	require.Equal(t, 200, result.Code, result.Body.String())
	require.Contains(t, result.Body.String(), "fixture_extra")
	current, err := model.FindImageJob(user.Id, receipt.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", current.Status)
	require.Equal(t, "settled", current.BillingStatus)
	require.Empty(t, current.RequestBody)
	var afterUser model.User
	var afterToken model.Token
	require.NoError(t, model.DB.First(&afterUser, user.Id).Error)
	require.NoError(t, model.DB.First(&afterToken, token.Id).Error)
	require.Equal(t, 995000, afterUser.Quota)
	require.Equal(t, 995000, afterToken.RemainQuota)
	require.Equal(t, int32(1), calls.Load())
	require.True(t, bytes.Equal([]byte(body), <-seenBody))
	_ = jobRequest(router, token, "GET", "/v1/images/jobs/"+receipt.ID+"/result", "", "", false)
	require.NoError(t, model.DB.First(&afterUser, user.Id).Error)
	require.Equal(t, 995000, afterUser.Quota)
	require.Equal(t, int32(1), calls.Load())
	// Polls work with exhausted quota, but revoked or wrong-user tokens cannot read.
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", token.Id).Updates(map[string]any{"status": common.TokenStatusExhausted, "remain_quota": 0}).Error)
	require.Equal(t, 200, jobRequest(router, token, "GET", "/v1/images/jobs/"+receipt.ID, "", "", false).Code)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", token.Id).Update("status", common.TokenStatusDisabled).Error)
	require.Equal(t, 401, jobRequest(router, token, "GET", "/v1/images/jobs/"+receipt.ID, "", "", false).Code)
}

func TestImageJobUnknownAndExplicitFailureNeverReplayed(t *testing.T) {
	for _, unknown := range []bool{true, false} {
		t.Run(fmt.Sprint(unknown), func(t *testing.T) {
			var calls atomic.Int32
			router, user, token := imageJobFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if unknown {
					h := w.(http.Hijacker)
					conn, _, _ := h.Hijack()
					_ = conn.Close()
					return
				}
				w.WriteHeader(422)
				_, _ = io.WriteString(w, `{"error":{"code":"content_policy_violation","message":"fixture rejection"}}`)
			}))
			w := jobRequest(router, token, "POST", "/v1/images/generations", `{"model":"fixture-4k","prompt":"fixture"}`, "unique", true)
			require.Equal(t, 202, w.Code, w.Body.String())
			job, err := model.ClaimImageJob("owner", "4k", time.Now().Unix())
			require.NoError(t, err)
			executeImageJob(router, job)
			current, err := model.FindImageJob(user.Id, job.ID)
			require.NoError(t, err)
			expected := "failed"
			if unknown {
				expected = "reconciliation_required"
			}
			require.Equal(t, expected, current.Status)
			require.Equal(t, int32(1), calls.Load())
			require.NoError(t, model.MaintainImageJobs("new", true, time.Now().Unix()))
			_, err = model.ClaimImageJob("new", "4k", time.Now().Unix())
			require.ErrorIs(t, err, gorm.ErrRecordNotFound)
			var after model.User
			require.NoError(t, model.DB.First(&after, user.Id).Error)
			require.Equal(t, 1000000, after.Quota)
		})
	}
}

func TestImageJobInputGuardsSyncAndCrossTokenIsolation(t *testing.T) {
	var calls atomic.Int32
	router, user, token := imageJobFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"created":123,"data":[{"url":"https://fixture.invalid/4096.png"}]}`)
	}))
	body := `{"model":"fixture-4k","prompt":"fixture"}`
	for _, tc := range []struct {
		body, key string
		status    int
	}{{body, "", 400}, {`{"model":"fixture-4k","stream":true}`, "key", 400}, {"[]", "key", 400}, {strings.Repeat("x", service.ImageJobMaxRequestBytes+1), "key", 413}} {
		w := jobRequest(router, token, "POST", "/v1/images/generations", tc.body, tc.key, true)
		require.Equal(t, tc.status, w.Code)
	}
	w := jobRequest(router, token, "POST", "/v1/images/generations", body, "", false)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, int32(1), calls.Load())
	_ = jobRequest(router, token, "POST", "/v1/images/generations", body, "key", true)
	job, err := model.FindImageJobByKey(user.Id, service.ImageJobDigest([]byte("key")))
	require.NoError(t, err)
	other := *token
	other.Id++
	other.Key = strings.Repeat("b", 48)
	require.NoError(t, model.DB.Create(&other).Error)
	require.Equal(t, 404, jobRequest(router, &other, "GET", "/v1/images/jobs/"+job.ID, "", "", false).Code)
	w = jobRequest(router, token, "DELETE", "/v1/images/jobs/"+job.ID, "", "", false)
	require.Equal(t, 200, w.Code)
	_, err = model.ClaimImageJob("owner", "4k", time.Now().Unix())
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	require.Equal(t, int32(1), calls.Load())
}

func TestImageJobSavedResultSurvivesBillingCheckpointFailure(t *testing.T) {
	var calls atomic.Int32
	router, user, token := imageJobFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"url":"https://fixture.invalid/image.png"}]}`)
	}))
	require.NoError(t, model.DB.Exec(`CREATE TRIGGER reject_settled BEFORE UPDATE OF billing_status ON image_jobs WHEN NEW.billing_status = 'settled' BEGIN SELECT RAISE(ABORT, 'fixture checkpoint fault'); END`).Error)
	w := jobRequest(router, token, "POST", "/v1/images/generations", `{"model":"fixture-4k","prompt":"fixture"}`, "unique", true)
	require.Equal(t, 202, w.Code)
	job, err := model.ClaimImageJob("owner", "4k", time.Now().Unix())
	require.NoError(t, err)
	executeImageJob(router, job)
	current, err := model.FindImageJob(user.Id, job.ID)
	require.NoError(t, err)
	require.Equal(t, "reconciliation_required", current.Status)
	require.NotEmpty(t, current.ResultBody)
	result := jobRequest(router, token, "GET", "/v1/images/jobs/"+job.ID+"/result", "", "", false)
	require.Equal(t, 200, result.Code)
	var after model.User
	require.NoError(t, model.DB.First(&after, user.Id).Error)
	require.Equal(t, 995000, after.Quota)
	require.NoError(t, model.MaintainImageJobs("new", true, time.Now().Unix()))
	_, err = model.ClaimImageJob("new", "4k", time.Now().Unix())
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	require.Equal(t, int32(1), calls.Load())
}

func TestImageJobRevokedAuthorizationFailsBeforeProvider(t *testing.T) {
	var calls atomic.Int32
	router, user, token := imageJobFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	w := jobRequest(router, token, "POST", "/v1/images/generations", `{"model":"fixture-4k","prompt":"fixture"}`, "unique", true)
	require.Equal(t, 202, w.Code)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", token.Id).Update("status", common.TokenStatusDisabled).Error)
	job, err := model.ClaimImageJob("owner", "4k", time.Now().Unix())
	require.NoError(t, err)
	executeImageJob(router, job)
	current, err := model.FindImageJob(user.Id, job.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", current.Status)
	require.False(t, current.ProviderStarted)
	require.Equal(t, int32(0), calls.Load())
	var after model.User
	require.NoError(t, model.DB.First(&after, user.Id).Error)
	require.Equal(t, 1000000, after.Quota)
}
