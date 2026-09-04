package router

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
	"github.com/QuantumNous/new-api/i18n"
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
		tokenKey    = "ordinarytoken1234567890"
		upstreamKey = "cliproxy-upstream-key"
		responseID  = "resp_cli_123"
	)

	var (
		upstreamMu              sync.Mutex
		upstreamResponses       = make(map[string]map[string]any)
		lastAuthorizationHeader string
		lastRequestMethod       string
		lastRequestPath         string
		lastRequestQuery        string
		lastPostedModel         string
		getRequests             int
		resourceRequests        []string
		cancelRequestBody       string
	)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamMu.Lock()
		lastAuthorizationHeader = r.Header.Get("Authorization")
		lastRequestMethod = r.Method
		lastRequestPath = r.URL.Path
		lastRequestQuery = r.URL.RawQuery
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
		case r.Method == http.MethodGet && r.URL.Path == "/v1/responses/"+responseID+"/input_items":
			upstreamMu.Lock()
			resourceRequests = append(resourceRequests, r.Method+" "+r.URL.RequestURI())
			upstreamMu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"object":   "list",
				"data":     []map[string]any{{"id": "item_cli_123", "type": "message", "role": "user"}},
				"has_more": false,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/responses/"+responseID+"/events":
			upstreamMu.Lock()
			resourceRequests = append(resourceRequests, r.Method+" "+r.URL.RequestURI())
			upstreamMu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"object":   "list",
				"data":     []map[string]any{{"type": "response.completed", "sequence_number": 3}},
				"has_more": false,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/responses/"+responseID+"/cancel":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			upstreamMu.Lock()
			resourceRequests = append(resourceRequests, r.Method+" "+r.URL.RequestURI())
			cancelRequestBody = string(body)
			payload := upstreamResponses[responseID]
			upstreamMu.Unlock()
			if payload == nil {
				http.Error(w, `{"error":{"message":"not found","type":"invalid_request_error"}}`, http.StatusNotFound)
				return
			}
			cancelled := make(map[string]any, len(payload))
			for key, value := range payload {
				cancelled[key] = value
			}
			cancelled["status"] = "cancelled"
			_ = json.NewEncoder(w).Encode(cancelled)
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/responses/"+responseID:
			upstreamMu.Lock()
			resourceRequests = append(resourceRequests, r.Method+" "+r.URL.RequestURI())
			_, ok := upstreamResponses[responseID]
			if ok {
				delete(upstreamResponses, responseID)
			}
			upstreamMu.Unlock()
			if !ok {
				http.Error(w, `{"error":{"message":"not found","type":"invalid_request_error"}}`, http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": responseID, "object": "response.deleted", "deleted": true,
			})
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
	priority := int64(100)
	if err := model.DB.Create(&model.Ability{
		Group:     "default",
		Model:     modelName,
		ChannelId: channel.Id,
		Enabled:   true,
		Priority:  &priority,
	}).Error; err != nil {
		t.Fatalf("failed to create channel ability: %v", err)
	}

	user := &model.User{
		Id:       userID,
		Username: "admin",
		Password: "password-hash",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Quota:    1000000,
		Group:    "default",
		AffCode:  "owner-aff",
	}
	if err := model.DB.Create(user).Error; err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	token := &model.Token{
		UserId:             userID,
		Name:               "relay-token",
		Key:                tokenKey,
		Status:             common.TokenStatusEnabled,
		CreatedTime:        time.Now().Unix(),
		AccessedTime:       time.Now().Unix(),
		ExpiredTime:        -1,
		RemainQuota:        1000000,
		UnlimitedQuota:     true,
		ModelLimitsEnabled: true,
		ModelLimits:        modelName,
		Group:              "",
	}
	if err := model.DB.Create(token).Error; err != nil {
		t.Fatalf("failed to create token: %v", err)
	}
	otherUser := &model.User{
		Id:       userID + 1,
		Username: "other-user",
		Password: "password-hash",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Quota:    1000000,
		Group:    "default",
		AffCode:  "other-aff",
	}
	if err := model.DB.Create(otherUser).Error; err != nil {
		t.Fatalf("failed to create other user: %v", err)
	}
	otherTokenKey := "otherordinarytoken1234567890"
	if err := model.DB.Create(&model.Token{
		UserId:             otherUser.Id,
		Name:               "other-relay-token",
		Key:                otherTokenKey,
		Status:             common.TokenStatusEnabled,
		CreatedTime:        time.Now().Unix(),
		AccessedTime:       time.Now().Unix(),
		ExpiredTime:        -1,
		RemainQuota:        1000000,
		UnlimitedQuota:     true,
		ModelLimitsEnabled: true,
		ModelLimits:        modelName,
	}).Error; err != nil {
		t.Fatalf("failed to create other token: %v", err)
	}

	engine := gin.New()
	SetRelayRouter(engine)

	postBody := `{"model":"gpt-4o-mini","input":"say hi"}`
	postReq := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(postBody))
	postReq.Header.Set("Content-Type", "application/json")
	postReq.Header.Set("Authorization", fmt.Sprintf("Bearer sk-%s", tokenKey))
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
	getReq.Header.Set("Authorization", fmt.Sprintf("Bearer sk-%s", tokenKey))
	getRec := httptest.NewRecorder()
	engine.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("expected GET /v1/responses/:id to succeed, got %d: %s", getRec.Code, getRec.Body.String())
	}

	if !bytes.Equal(bytes.TrimSpace(getRec.Body.Bytes()), bytes.TrimSpace(postRec.Body.Bytes())) {
		t.Fatalf("expected GET response body to match stored upstream response\nPOST: %s\nGET: %s", postRec.Body.String(), getRec.Body.String())
	}

	getWithSuffix := httptest.NewRequest(http.MethodGet, "/v1/responses/"+responseID, nil)
	getWithSuffix.Header.Set("Authorization", fmt.Sprintf("Bearer sk-%s-%d", tokenKey, channel.Id))
	getWithSuffixRec := httptest.NewRecorder()
	engine.ServeHTTP(getWithSuffixRec, getWithSuffix)
	if getWithSuffixRec.Code != http.StatusOK {
		t.Fatalf("expected channel-suffixed GET /v1/responses/:id to succeed, got %d: %s", getWithSuffixRec.Code, getWithSuffixRec.Body.String())
	}

	// A channel-suffixed token remains forbidden for ordinary POST requests.
	postWithSuffix := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(postBody))
	postWithSuffix.Header.Set("Content-Type", "application/json")
	postWithSuffix.Header.Set("Authorization", fmt.Sprintf("Bearer sk-%s-%d", tokenKey, channel.Id))
	postWithSuffixRec := httptest.NewRecorder()
	engine.ServeHTTP(postWithSuffixRec, postWithSuffix)
	if postWithSuffixRec.Code != http.StatusForbidden {
		t.Fatalf("expected ordinary channel-suffixed POST to remain forbidden, got %d: %s", postWithSuffixRec.Code, postWithSuffixRec.Body.String())
	}

	// A missing response reference cannot fall back to the suffix-selected channel.
	missingGet := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_missing", nil)
	missingGet.Header.Set("Authorization", fmt.Sprintf("Bearer sk-%s-%d", tokenKey, channel.Id))
	missingGetRec := httptest.NewRecorder()
	engine.ServeHTTP(missingGetRec, missingGet)
	if missingGetRec.Code != http.StatusNotFound {
		t.Fatalf("expected unreferenced suffixed GET to be not found, got %d: %s", missingGetRec.Code, missingGetRec.Body.String())
	}

	otherUserGet := httptest.NewRequest(http.MethodGet, "/v1/responses/"+responseID, nil)
	otherUserGet.Header.Set("Authorization", fmt.Sprintf("Bearer sk-%s", otherTokenKey))
	otherUserGetRec := httptest.NewRecorder()
	engine.ServeHTTP(otherUserGetRec, otherUserGet)
	if otherUserGetRec.Code != http.StatusNotFound {
		t.Fatalf("expected another user to receive 404 for an owned response, got %d: %s", otherUserGetRec.Code, otherUserGetRec.Body.String())
	}

	resourceCases := []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/v1/responses/" + responseID + "/input_items?limit=2&order=asc"},
		{method: http.MethodGet, path: "/v1/responses/" + responseID + "/events?starting_after=1"},
		{method: http.MethodPost, path: "/v1/responses/" + responseID + "/cancel"},
		{method: http.MethodDelete, path: "/v1/responses/" + responseID},
	}
	for _, testCase := range resourceCases {
		otherReq := httptest.NewRequest(testCase.method, testCase.path, nil)
		otherReq.Header.Set("Authorization", fmt.Sprintf("Bearer sk-%s", otherTokenKey))
		otherRec := httptest.NewRecorder()
		engine.ServeHTTP(otherRec, otherReq)
		if otherRec.Code != http.StatusNotFound {
			t.Fatalf("expected another user to receive 404 for %s %s, got %d: %s", testCase.method, testCase.path, otherRec.Code, otherRec.Body.String())
		}

		missingPath := strings.Replace(testCase.path, responseID, "resp_missing_resource", 1)
		missingReq := httptest.NewRequest(testCase.method, missingPath, nil)
		missingReq.Header.Set("Authorization", fmt.Sprintf("Bearer sk-%s", tokenKey))
		missingRec := httptest.NewRecorder()
		engine.ServeHTTP(missingRec, missingReq)
		if missingRec.Code != http.StatusNotFound {
			t.Fatalf("expected a missing response to receive 404 for %s %s, got %d: %s", testCase.method, missingPath, missingRec.Code, missingRec.Body.String())
		}
	}

	inputItemsReq := httptest.NewRequest(
		http.MethodGet,
		"/v1/responses/"+responseID+"/input_items?limit=2&order=asc",
		nil,
	)
	inputItemsReq.Header.Set("Authorization", fmt.Sprintf("Bearer sk-%s", tokenKey))
	inputItemsRec := httptest.NewRecorder()
	engine.ServeHTTP(inputItemsRec, inputItemsReq)
	if inputItemsRec.Code != http.StatusOK || !strings.Contains(inputItemsRec.Body.String(), "item_cli_123") {
		t.Fatalf("expected input_items to pass through, got %d: %s", inputItemsRec.Code, inputItemsRec.Body.String())
	}

	eventsReq := httptest.NewRequest(
		http.MethodGet,
		"/v1/responses/"+responseID+"/events?starting_after=1",
		nil,
	)
	eventsReq.Header.Set("Authorization", fmt.Sprintf("Bearer sk-%s", tokenKey))
	eventsRec := httptest.NewRecorder()
	engine.ServeHTTP(eventsRec, eventsReq)
	if eventsRec.Code != http.StatusOK || !strings.Contains(eventsRec.Body.String(), "response.completed") {
		t.Fatalf("expected events to pass through, got %d: %s", eventsRec.Code, eventsRec.Body.String())
	}

	cancelBody := `{"reason":"caller_requested"}`
	cancelReq := httptest.NewRequest(
		http.MethodPost,
		"/v1/responses/"+responseID+"/cancel",
		strings.NewReader(cancelBody),
	)
	cancelReq.Header.Set("Content-Type", "application/json")
	cancelReq.Header.Set("Authorization", fmt.Sprintf("Bearer sk-%s", tokenKey))
	cancelRec := httptest.NewRecorder()
	engine.ServeHTTP(cancelRec, cancelReq)
	if cancelRec.Code != http.StatusOK || !strings.Contains(cancelRec.Body.String(), `"status":"cancelled"`) {
		t.Fatalf("expected cancel to pass through, got %d: %s", cancelRec.Code, cancelRec.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/v1/responses/"+responseID, nil)
	deleteReq.Header.Set("Authorization", fmt.Sprintf("Bearer sk-%s", tokenKey))
	deleteRec := httptest.NewRecorder()
	engine.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK || !strings.Contains(deleteRec.Body.String(), `"deleted":true`) {
		t.Fatalf("expected DELETE response to pass through, got %d: %s", deleteRec.Code, deleteRec.Body.String())
	}
	var remainingRefs int64
	if err := model.DB.Model(&model.RelayResponseRef{}).Where("response_id = ?", responseID).Count(&remainingRefs).Error; err != nil {
		t.Fatalf("failed to count response refs after delete: %v", err)
	}
	if remainingRefs != 0 {
		t.Fatalf("expected successful DELETE to remove the response ref, got %d", remainingRefs)
	}

	upstreamMu.Lock()
	defer upstreamMu.Unlock()
	if lastAuthorizationHeader != "Bearer "+upstreamKey {
		t.Fatalf("expected upstream authorization header %q, got %q", "Bearer "+upstreamKey, lastAuthorizationHeader)
	}
	if lastPostedModel != modelName {
		t.Fatalf("expected upstream model %q, got %q", modelName, lastPostedModel)
	}
	if getRequests != 2 {
		t.Fatalf("expected exactly two upstream GET /v1/responses/:id calls, got %d", getRequests)
	}
	if lastRequestMethod != http.MethodDelete || lastRequestPath != "/v1/responses/"+responseID || lastRequestQuery != "" {
		t.Fatalf("expected last upstream request to be DELETE /v1/responses/%s, got %s %s?%s", responseID, lastRequestMethod, lastRequestPath, lastRequestQuery)
	}
	wantResourceRequests := []string{
		"GET /v1/responses/" + responseID + "/input_items?limit=2&order=asc",
		"GET /v1/responses/" + responseID + "/events?starting_after=1",
		"POST /v1/responses/" + responseID + "/cancel",
		"DELETE /v1/responses/" + responseID,
	}
	if fmt.Sprint(resourceRequests) != fmt.Sprint(wantResourceRequests) {
		t.Fatalf("unexpected upstream resource requests: got %v want %v", resourceRequests, wantResourceRequests)
	}
	if cancelRequestBody != cancelBody {
		t.Fatalf("expected cancel body %q, got %q", cancelBody, cancelRequestBody)
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
	if err := i18n.Init(); err != nil {
		t.Fatalf("failed to init i18n: %v", err)
	}

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
