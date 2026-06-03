package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func TestResponsesAffinityFailureFailsOverToNextChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupRelayRouterTestDB(t)

	prevRetryTimes := common.RetryTimes
	prevCooldownEnabled := common.AutomaticChannelCooldownEnabled
	prevCooldownThreshold := common.ChannelCooldownFailureThreshold
	prevCooldownWindow := common.ChannelCooldownFailureWindowSeconds
	prevCooldownSeconds := common.ChannelCooldownSeconds
	t.Cleanup(func() {
		common.RetryTimes = prevRetryTimes
		common.AutomaticChannelCooldownEnabled = prevCooldownEnabled
		common.ChannelCooldownFailureThreshold = prevCooldownThreshold
		common.ChannelCooldownFailureWindowSeconds = prevCooldownWindow
		common.ChannelCooldownSeconds = prevCooldownSeconds
		_, _ = service.ClearChannelAffinityCacheByRuleName("codex cli trace")
	})
	common.RetryTimes = 1
	common.AutomaticChannelCooldownEnabled = true
	common.ChannelCooldownFailureThreshold = 3
	common.ChannelCooldownFailureWindowSeconds = 120
	common.ChannelCooldownSeconds = 300

	const (
		userID    = 1
		modelName = "gpt-4o-mini"
		tokenKey  = "failovertoken1234567890"
		cacheKey  = "affinity-failover-real-path"
	)

	var (
		primaryMu       sync.Mutex
		primaryShouldOK = true
		primaryRequests int
		backupRequests  int
	)

	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryMu.Lock()
		primaryRequests++
		shouldOK := primaryShouldOK
		primaryMu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if !shouldOK {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"message":"quota exhausted on primary","type":"insufficient_quota","code":"insufficient_quota"}}`))
			return
		}
		writeResponsesPayload(t, w, "resp_primary", modelName, "primary ok")
	}))
	defer primary.Close()

	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backupRequests++
		w.Header().Set("Content-Type", "application/json")
		writeResponsesPayload(t, w, "resp_backup", modelName, "backup ok")
	}))
	defer backup.Close()

	primaryChannel := createRelayTestChannel(t, "affinity-primary", primary.URL, 1000, 100, modelName)
	backupChannel := createRelayTestChannel(t, "affinity-backup", backup.URL, 100, 100, modelName)
	createRelayTestUserAndToken(t, userID, tokenKey)

	engine := gin.New()
	SetRelayRouter(engine)

	firstRec := postResponsesForFailoverTest(engine, tokenKey, modelName, cacheKey)
	if firstRec.Code != http.StatusOK {
		t.Fatalf("expected first request to seed affinity on primary, got %d: %s", firstRec.Code, firstRec.Body.String())
	}

	var firstResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(firstRec.Body.Bytes(), &firstResp); err != nil {
		t.Fatalf("failed to decode first response: %v", err)
	}
	if firstResp.ID != "resp_primary" {
		t.Fatalf("expected first response from primary channel %d, got %q", primaryChannel.Id, firstResp.ID)
	}

	primaryMu.Lock()
	primaryShouldOK = false
	primaryMu.Unlock()

	secondRec := postResponsesForFailoverTest(engine, tokenKey, modelName, cacheKey)
	if secondRec.Code != http.StatusOK {
		t.Fatalf("expected second request to fail over to backup channel %d, got %d: %s", backupChannel.Id, secondRec.Code, secondRec.Body.String())
	}
	var secondResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(secondRec.Body.Bytes(), &secondResp); err != nil {
		t.Fatalf("failed to decode second response: %v", err)
	}
	if secondResp.ID != "resp_backup" {
		t.Fatalf("expected second response from backup channel %d, got %q", backupChannel.Id, secondResp.ID)
	}

	thirdRec := postResponsesForFailoverTest(engine, tokenKey, modelName, cacheKey)
	if thirdRec.Code != http.StatusOK {
		t.Fatalf("expected third request to keep using backup affinity channel %d, got %d: %s", backupChannel.Id, thirdRec.Code, thirdRec.Body.String())
	}
	var thirdResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(thirdRec.Body.Bytes(), &thirdResp); err != nil {
		t.Fatalf("failed to decode third response: %v", err)
	}
	if thirdResp.ID != "resp_backup" {
		t.Fatalf("expected third response from backup channel %d, got %q", backupChannel.Id, thirdResp.ID)
	}

	primaryMu.Lock()
	gotPrimaryRequests := primaryRequests
	primaryMu.Unlock()
	if gotPrimaryRequests != 2 {
		t.Fatalf("expected primary to be called once for success and once before failover, got %d", gotPrimaryRequests)
	}
	if backupRequests != 2 {
		t.Fatalf("expected backup to be called for failover and refreshed affinity, got %d", backupRequests)
	}
}

func createRelayTestChannel(t *testing.T, name string, baseURL string, priority int64, weight uint, modelName string) *model.Channel {
	t.Helper()
	channel := &model.Channel{
		Name:        name,
		Type:        constant.ChannelTypeOpenAI,
		Key:         "test-upstream-key",
		Status:      common.ChannelStatusEnabled,
		CreatedTime: time.Now().Unix(),
		Group:       "default",
		Models:      modelName,
		BaseURL:     stringPtr(baseURL),
		Priority:    int64Ptr(priority),
		Weight:      uintPtr(weight),
		AutoBan:     intPtr(0),
	}
	if err := model.DB.Create(channel).Error; err != nil {
		t.Fatalf("failed to create channel %s: %v", name, err)
	}
	if err := channel.AddAbilities(nil); err != nil {
		t.Fatalf("failed to create abilities for channel %s: %v", name, err)
	}
	return channel
}

func createRelayTestUserAndToken(t *testing.T, userID int, tokenKey string) {
	t.Helper()
	user := &model.User{
		Id:       userID,
		Username: "failover-admin",
		Password: "password-hash",
		Role:     common.RoleAdminUser,
		Status:   common.UserStatusEnabled,
		Quota:    1000000,
		Group:    "default",
	}
	if err := model.DB.Create(user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	token := &model.Token{
		UserId:         userID,
		Name:           "failover-token",
		Key:            tokenKey,
		Status:         common.TokenStatusEnabled,
		CreatedTime:    time.Now().Unix(),
		AccessedTime:   time.Now().Unix(),
		ExpiredTime:    -1,
		RemainQuota:    1000000,
		UnlimitedQuota: true,
		Group:          "",
	}
	if err := model.DB.Create(token).Error; err != nil {
		t.Fatalf("failed to create token: %v", err)
	}
}

func postResponsesForFailoverTest(engine *gin.Engine, tokenKey string, modelName string, promptCacheKey string) *httptest.ResponseRecorder {
	body := fmt.Sprintf(`{"model":%q,"input":"say hi","prompt_cache_key":%q}`, modelName, promptCacheKey)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer sk-"+tokenKey)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func writeResponsesPayload(t *testing.T, w http.ResponseWriter, id string, modelName string, text string) {
	t.Helper()
	payload := map[string]any{
		"id":         id,
		"object":     "response",
		"created_at": 1713513600,
		"status":     "completed",
		"model":      modelName,
		"output": []map[string]any{
			{
				"type":   "message",
				"id":     "msg_" + id,
				"status": "completed",
				"role":   "assistant",
				"content": []map[string]any{
					{
						"type":        "output_text",
						"text":        text,
						"annotations": []any{},
					},
				},
			},
		},
		"parallel_tool_calls": false,
		"store":               true,
		"temperature":         1,
		"top_p":               1,
		"tools":               []any{},
		"usage": map[string]any{
			"input_tokens":  5,
			"output_tokens": 3,
			"total_tokens":  8,
		},
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("failed to encode upstream payload: %v", err)
	}
}

func int64Ptr(v int64) *int64 {
	return &v
}

func uintPtr(v uint) *uint {
	return &v
}

func intPtr(v int) *int {
	return &v
}
