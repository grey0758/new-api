package router

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

func TestCLIProxyResponsesRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupRelayRouterTestDB(t)

	const (
		userID      = 1
		modelName   = "gpt-4o-mini"
		tokenKey    = "admintoken1234567890"
		upstreamKey = "cliproxy-upstream-key"
		responseID  = "resp_cli_123"
	)

	var (
		upstreamMu              sync.Mutex
		upstreamResponses       = make(map[string]map[string]any)
		lastAuthorizationHeader string
		lastRequestMethod       string
		lastRequestPath         string
		lastPostedModel         string
		getRequests             int
	)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamMu.Lock()
		lastAuthorizationHeader = r.Header.Get("Authorization")
		lastRequestMethod = r.Method
		lastRequestPath = r.URL.Path
		upstreamMu.Unlock()

		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/responses":
			var req struct {
				Model string `json:"model"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			upstreamMu.Lock()
			lastPostedModel = req.Model
			payload := map[string]any{
				"id":         responseID,
				"object":     "response",
				"created_at": 1713513600,
				"status":     "completed",
				"model":      req.Model,
				"output": []map[string]any{
					{
						"type":   "message",
						"id":     "msg_cli_123",
						"status": "completed",
						"role":   "assistant",
						"content": []map[string]any{
							{
								"type":        "output_text",
								"text":        "hello from CLIProxy",
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
					"input_tokens":  11,
					"output_tokens": 7,
					"total_tokens":  18,
				},
			}
			upstreamResponses[responseID] = payload
			upstreamMu.Unlock()
			_ = json.NewEncoder(w).Encode(payload)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/responses/"+responseID:
			upstreamMu.Lock()
			getRequests++
			payload, ok := upstreamResponses[responseID]
			upstreamMu.Unlock()
			if !ok {
				http.Error(w, `{"error":{"message":"not found","type":"invalid_request_error"}}`, http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(payload)
		default:
			http.Error(w, fmt.Sprintf(`{"error":{"message":"unexpected upstream route %s %s","type":"invalid_request_error"}}`, r.Method, r.URL.Path), http.StatusNotFound)
		}
	}))
	defer upstream.Close()

	channel := &model.Channel{
		Name:        "cliproxy-test",
		Type:        constant.ChannelTypeCLIProxy,
		Key:         upstreamKey,
		Status:      common.ChannelStatusEnabled,
		CreatedTime: time.Now().Unix(),
		Group:       "default",
		Models:      modelName,
		BaseURL:     stringPtr(upstream.URL),
	}
	if err := model.DB.Create(channel).Error; err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}

	user := &model.User{
		Id:       userID,
		Username: "admin",
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
		Name:           "relay-token",
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

	engine := gin.New()
	SetRelayRouter(engine)

	postBody := `{"model":"gpt-4o-mini","input":"say hi"}`
	postReq := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(postBody))
	postReq.Header.Set("Content-Type", "application/json")
	postReq.Header.Set("Authorization", fmt.Sprintf("Bearer sk-%s-%d", tokenKey, channel.Id))
	postRec := httptest.NewRecorder()
	engine.ServeHTTP(postRec, postReq)

	if postRec.Code != http.StatusOK {
		t.Fatalf("expected POST /v1/responses to succeed, got %d: %s", postRec.Code, postRec.Body.String())
	}

	var postResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(postRec.Body.Bytes(), &postResp); err != nil {
		t.Fatalf("failed to decode POST response: %v", err)
	}
	if postResp.ID != responseID {
		t.Fatalf("expected response id %q, got %q", responseID, postResp.ID)
	}

	ref, err := model.GetRelayResponseRefByResponseID(responseID)
	if err != nil {
		t.Fatalf("expected relay response ref to be persisted: %v", err)
	}
	if ref.ChannelID != channel.Id {
		t.Fatalf("expected stored channel id %d, got %d", channel.Id, ref.ChannelID)
	}
	if ref.ModelName != modelName {
		t.Fatalf("expected stored model %q, got %q", modelName, ref.ModelName)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/v1/responses/"+responseID, nil)
	getReq.Header.Set("Authorization", fmt.Sprintf("Bearer sk-%s-%d", tokenKey, channel.Id))
	getRec := httptest.NewRecorder()
	engine.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("expected GET /v1/responses/:id to succeed, got %d: %s", getRec.Code, getRec.Body.String())
	}

	if !bytes.Equal(bytes.TrimSpace(getRec.Body.Bytes()), bytes.TrimSpace(postRec.Body.Bytes())) {
		t.Fatalf("expected GET response body to match stored upstream response\nPOST: %s\nGET: %s", postRec.Body.String(), getRec.Body.String())
	}

	upstreamMu.Lock()
	defer upstreamMu.Unlock()
	if lastAuthorizationHeader != "Bearer "+upstreamKey {
		t.Fatalf("expected upstream authorization header %q, got %q", "Bearer "+upstreamKey, lastAuthorizationHeader)
	}
	if lastPostedModel != modelName {
		t.Fatalf("expected upstream model %q, got %q", modelName, lastPostedModel)
	}
	if getRequests != 1 {
		t.Fatalf("expected exactly one upstream GET /v1/responses/:id call, got %d", getRequests)
	}
	if lastRequestMethod != http.MethodGet || lastRequestPath != "/v1/responses/"+responseID {
		t.Fatalf("expected last upstream request to be GET /v1/responses/%s, got %s %s", responseID, lastRequestMethod, lastRequestPath)
	}
}

func setupRelayRouterTestDB(t *testing.T) {
	t.Helper()

	prevSQLitePath := common.SQLitePath
	prevUsingSQLite := common.UsingSQLite
	prevUsingMySQL := common.UsingMySQL
	prevUsingPostgreSQL := common.UsingPostgreSQL
	prevRedisEnabled := common.RedisEnabled
	prevMemoryCacheEnabled := common.MemoryCacheEnabled
	prevIsMasterNode := common.IsMasterNode
	prevOptionMap := common.OptionMap
	prevSelfUseModeEnabled := operation_setting.SelfUseModeEnabled
	prevSQLDSN, hadSQLDSN := os.LookupEnv("SQL_DSN")

	dbPath := filepath.Join(t.TempDir(), "relay-router-test.db") + "?_busy_timeout=30000"
	common.SQLitePath = dbPath
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false
	common.IsMasterNode = true
	common.OptionMap = map[string]string{}
	operation_setting.SelfUseModeEnabled = true
	_ = os.Setenv("SQL_DSN", "local")

	if err := model.InitDB(); err != nil {
		t.Fatalf("failed to init test db: %v", err)
	}
	model.LOG_DB = model.DB
	service.InitHttpClient()
	service.ResetProxyClientCache()

	t.Cleanup(func() {
		if sqlDB, err := model.DB.DB(); err == nil {
			_ = sqlDB.Close()
		}
		common.SQLitePath = prevSQLitePath
		common.UsingSQLite = prevUsingSQLite
		common.UsingMySQL = prevUsingMySQL
		common.UsingPostgreSQL = prevUsingPostgreSQL
		common.RedisEnabled = prevRedisEnabled
		common.MemoryCacheEnabled = prevMemoryCacheEnabled
		common.IsMasterNode = prevIsMasterNode
		common.OptionMap = prevOptionMap
		operation_setting.SelfUseModeEnabled = prevSelfUseModeEnabled
		if hadSQLDSN {
			_ = os.Setenv("SQL_DSN", prevSQLDSN)
		} else {
			_ = os.Unsetenv("SQL_DSN")
		}
	})
}

func stringPtr(v string) *string {
	return &v
}
