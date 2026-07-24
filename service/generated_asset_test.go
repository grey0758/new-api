package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type fakeGeneratedAssetObject struct {
	body        []byte
	contentType string
	metadata    map[string]string
}

type fakeGeneratedAssetStore struct {
	mu           sync.Mutex
	objects      map[string]fakeGeneratedAssetObject
	putCount     int
	getCount     int
	deleteCount  int
	presignCount int
}

func newFakeGeneratedAssetStore() *fakeGeneratedAssetStore {
	return &fakeGeneratedAssetStore{objects: make(map[string]fakeGeneratedAssetObject)}
}

func (store *fakeGeneratedAssetStore) Put(_ context.Context, objectKey string, contentType string, body []byte, metadata map[string]string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	metadataCopy := make(map[string]string, len(metadata))
	for key, value := range metadata {
		metadataCopy[key] = value
	}
	store.objects[objectKey] = fakeGeneratedAssetObject{
		body:        append([]byte(nil), body...),
		contentType: contentType,
		metadata:    metadataCopy,
	}
	store.putCount++
	return nil
}

func (store *fakeGeneratedAssetStore) Get(_ context.Context, objectKey string) ([]byte, string, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.getCount++
	object, ok := store.objects[objectKey]
	if !ok {
		return nil, "", fmt.Errorf("object %q not found", objectKey)
	}
	return append([]byte(nil), object.body...), object.contentType, nil
}

func (store *fakeGeneratedAssetStore) Delete(_ context.Context, objectKey string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.objects, objectKey)
	store.deleteCount++
	return nil
}

func (store *fakeGeneratedAssetStore) PresignGet(_ context.Context, objectKey string, _ time.Duration) (string, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.presignCount++
	if _, ok := store.objects[objectKey]; !ok {
		return "", fmt.Errorf("object %q not found", objectKey)
	}
	return "https://signed.example.invalid/" + objectKey, nil
}

func setupGeneratedAssetTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)", filepath.Join(t.TempDir(), "generated-assets.db"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.GeneratedImageRequest{},
		&model.GeneratedAsset{},
	))

	previousDB := model.DB
	previousUsingSQLite := common.UsingSQLite
	model.DB = db
	common.UsingSQLite = true
	t.Cleanup(func() {
		model.DB = previousDB
		common.UsingSQLite = previousUsingSQLite
		_ = sqlDB.Close()
	})
	return db
}

func generatedAssetTestRuntime(store generatedAssetObjectStore) *generatedAssetRuntime {
	return &generatedAssetRuntime{
		config: generatedAssetStorageConfig{
			Retention:    30 * 24 * time.Hour,
			PresignTTL:   5 * time.Minute,
			MaxAssetSize: 64 * 1024 * 1024,
		},
		store: store,
	}
}

func generatedAssetTestContext(t *testing.T, body string, idempotencyKey string) *gin.Context {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "https://video.example.invalid/v1/images/generations", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	if idempotencyKey != "" {
		c.Request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	t.Cleanup(func() {
		common.CleanupBodyStorage(c)
	})
	return c
}

func generatedAssetTestPNG(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	source := image.NewNRGBA(image.Rect(0, 0, 2, 3))
	source.SetNRGBA(0, 0, color.NRGBA{R: 0x22, G: 0x66, B: 0xaa, A: 0xff})
	require.NoError(t, png.Encode(&buffer, source))
	return buffer.Bytes()
}

func generatedAssetTestRelayInfo(requestId string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		UserId:          41,
		TokenId:         7,
		RequestId:       requestId,
		OriginModelName: "gpt-image-1",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 6},
	}
}

func TestGeneratedImagePersistenceAndIdempotentReplay(t *testing.T) {
	db := setupGeneratedAssetTestDB(t)
	store := newFakeGeneratedAssetStore()
	restoreRuntime := setGeneratedAssetRuntimeForTest(generatedAssetTestRuntime(store))
	t.Cleanup(restoreRuntime)

	const (
		requestBody    = `{"model":"gpt-image-1","prompt":"blue square","response_format":"b64_json"}`
		idempotencyKey = "generated-image-replay-1"
	)
	request := &dto.ImageRequest{
		Model:          "gpt-image-1",
		Prompt:         "blue square",
		ResponseFormat: "b64_json",
	}
	firstContext := generatedAssetTestContext(t, requestBody, idempotencyKey)
	firstInfo := generatedAssetTestRelayInfo("request-generated-asset-1")
	decision, err := PrepareGeneratedImageRequest(firstContext, firstInfo, request)
	require.NoError(t, err)
	require.Equal(t, GeneratedImageDecisionProceed, decision.Action)

	imageBytes := generatedAssetTestPNG(t)
	response := &dto.ImageResponse{
		Created: 123,
		Data: []dto.ImageData{{
			B64Json: base64.StdEncoding.EncodeToString(imageBytes),
		}},
	}
	require.NoError(t, PersistGeneratedImageResponse(firstContext, firstInfo, request, response))
	require.Equal(t, generatedAssetTaskId(firstInfo.RequestId, 0), response.TaskId)
	require.Equal(t, response.TaskId, response.Data[0].TaskId)
	require.NotEmpty(t, response.Data[0].B64Json)
	require.Empty(t, response.Data[0].Url)
	require.Equal(t, 1, store.putCount)

	assets, err := model.GetGeneratedAssetsByRequestId(firstInfo.RequestId)
	require.NoError(t, err)
	require.Len(t, assets, 1)
	require.Equal(t, response.TaskId, assets[0].TaskId)
	require.Equal(t, "image/png", assets[0].ContentType)
	require.Equal(t, 2, assets[0].Width)
	require.Equal(t, 3, assets[0].Height)
	require.Len(t, assets[0].SHA256, 64)
	require.EqualValues(t, len(imageBytes), assets[0].SizeBytes)
	require.Equal(t, firstInfo.RequestId, store.objects[assets[0].ObjectKey].metadata["request-id"])

	storedRequest, err := model.GetGeneratedImageRequestByRequestId(firstInfo.RequestId)
	require.NoError(t, err)
	require.Equal(t, model.GeneratedImageRequestStatusStored, storedRequest.Status)
	require.NoError(t, MarkGeneratedImageRequestSucceeded(firstInfo.RequestId))

	replayContext := generatedAssetTestContext(t, requestBody, idempotencyKey)
	replayInfo := generatedAssetTestRelayInfo("request-generated-asset-2")
	replayDecision, err := PrepareGeneratedImageRequest(replayContext, replayInfo, request)
	require.NoError(t, err)
	require.Equal(t, GeneratedImageDecisionReplay, replayDecision.Action)
	require.Equal(t, firstInfo.RequestId, replayDecision.RequestId)
	require.Equal(t, http.StatusOK, replayDecision.StatusCode)

	var replayResponse dto.ImageResponse
	require.NoError(t, common.Unmarshal(replayDecision.ResponseBody, &replayResponse))
	require.Equal(t, response.TaskId, replayResponse.TaskId)
	require.Equal(t, response.TaskId, replayResponse.Data[0].TaskId)
	require.NotEmpty(t, replayResponse.Data[0].B64Json)
	require.Equal(t, 1, store.putCount)
	require.Equal(t, 1, store.getCount)

	var requestCount int64
	require.NoError(t, db.Model(&model.GeneratedImageRequest{}).Count(&requestCount).Error)
	require.EqualValues(t, 1, requestCount)
}

func TestGeneratedImageIdempotencyConflictAndExpiryAreFailClosed(t *testing.T) {
	db := setupGeneratedAssetTestDB(t)
	store := newFakeGeneratedAssetStore()
	restoreRuntime := setGeneratedAssetRuntimeForTest(generatedAssetTestRuntime(store))
	t.Cleanup(restoreRuntime)

	const (
		originalBody    = `{"model":"gpt-image-1","prompt":"first"}`
		conflictingBody = `{"model":"gpt-image-1","prompt":"second"}`
		idempotencyKey  = "generated-image-conflict-1"
	)
	request := &dto.ImageRequest{Model: "gpt-image-1", Prompt: "first"}
	firstContext := generatedAssetTestContext(t, originalBody, idempotencyKey)
	firstInfo := generatedAssetTestRelayInfo("request-generated-conflict-1")
	decision, err := PrepareGeneratedImageRequest(firstContext, firstInfo, request)
	require.NoError(t, err)
	require.Equal(t, GeneratedImageDecisionProceed, decision.Action)

	imageBytes := generatedAssetTestPNG(t)
	response := &dto.ImageResponse{Data: []dto.ImageData{{
		B64Json: base64.StdEncoding.EncodeToString(imageBytes),
	}}}
	require.NoError(t, PersistGeneratedImageResponse(firstContext, firstInfo, request, response))
	require.NoError(t, MarkGeneratedImageRequestSucceeded(firstInfo.RequestId))

	conflictContext := generatedAssetTestContext(t, conflictingBody, idempotencyKey)
	conflictDecision, err := PrepareGeneratedImageRequest(
		conflictContext,
		generatedAssetTestRelayInfo("request-generated-conflict-2"),
		&dto.ImageRequest{Model: "gpt-image-1", Prompt: "second"},
	)
	require.NoError(t, err)
	require.Equal(t, GeneratedImageDecisionReject, conflictDecision.Action)
	require.Equal(t, http.StatusConflict, conflictDecision.StatusCode)
	require.Equal(t, "idempotency_key_conflict", conflictDecision.ErrorCode)
	require.Equal(t, 1, store.putCount)

	require.NoError(t, db.Model(&model.GeneratedImageRequest{}).
		Where("request_id = ?", firstInfo.RequestId).
		Update("expires_at", common.GetTimestamp()-1).Error)
	expiredContext := generatedAssetTestContext(t, originalBody, idempotencyKey)
	expiredDecision, err := PrepareGeneratedImageRequest(
		expiredContext,
		generatedAssetTestRelayInfo("request-generated-conflict-3"),
		request,
	)
	require.NoError(t, err)
	require.Equal(t, GeneratedImageDecisionReject, expiredDecision.Action)
	require.Equal(t, http.StatusGone, expiredDecision.StatusCode)
	require.Equal(t, "idempotency_asset_expired", expiredDecision.ErrorCode)
	require.Zero(t, store.getCount)
	require.Equal(t, 1, store.putCount)
}

func TestGeneratedAssetOwnershipExpiryAndHistory(t *testing.T) {
	db := setupGeneratedAssetTestDB(t)
	store := newFakeGeneratedAssetStore()
	restoreRuntime := setGeneratedAssetRuntimeForTest(generatedAssetTestRuntime(store))
	t.Cleanup(restoreRuntime)

	imageBytes := generatedAssetTestPNG(t)
	taskId := generatedAssetTaskId("request-owned-asset", 0)
	objectKey := generatedAssetObjectKey(41, taskId, ".png")
	require.NoError(t, store.Put(context.Background(), objectKey, "image/png", imageBytes, nil))
	asset := model.GeneratedAsset{
		TaskId:      taskId,
		RequestId:   "request-owned-asset",
		UserId:      41,
		TokenId:     7,
		Model:       "gpt-image-1",
		ChannelId:   6,
		ObjectKey:   objectKey,
		ContentType: "image/png",
		SizeBytes:   int64(len(imageBytes)),
		Width:       2,
		Height:      3,
		SHA256:      strings.Repeat("a", 64),
		ExpiresAt:   common.GetTimestamp() + 3600,
	}
	require.NoError(t, db.Create(&asset).Error)

	c := generatedAssetTestContext(t, `{}`, "")
	_, err := GetGeneratedAssetTask(c, 99, taskId)
	require.ErrorIs(t, err, ErrGeneratedAssetNotFound)
	_, err = PresignGeneratedAssetContent(c, 99, taskId)
	require.ErrorIs(t, err, ErrGeneratedAssetNotFound)
	require.Zero(t, store.presignCount)

	task, err := GetGeneratedAssetTask(c, 41, taskId)
	require.NoError(t, err)
	require.Equal(t, "completed", task.Status)
	require.Contains(t, task.ContentURL, taskId)
	signedURL, err := PresignGeneratedAssetContent(c, 41, taskId)
	require.NoError(t, err)
	require.Contains(t, signedURL, objectKey)
	require.Equal(t, 1, store.presignCount)

	history, err := ListGeneratedAssetTasks(c, 41, 1, 20)
	require.NoError(t, err)
	require.EqualValues(t, 1, history.Total)
	require.Len(t, history.Data, 1)
	require.Equal(t, taskId, history.Data[0].TaskId)

	require.NoError(t, db.Model(&model.GeneratedAsset{}).
		Where("task_id = ?", taskId).
		Update("expires_at", common.GetTimestamp()-1).Error)
	_, err = PresignGeneratedAssetContent(c, 41, taskId)
	require.ErrorIs(t, err, ErrGeneratedAssetExpired)
	require.Equal(t, 1, store.presignCount)
}

func TestGeneratedAssetSourceURLRejectsNonPublicTargets(t *testing.T) {
	testCases := []string{
		"http://example.com/image.png",
		"https://localhost/image.png",
		"https://127.0.0.1/image.png",
		"https://10.0.0.1/image.png",
		"https://100.64.0.1/image.png",
		"https://169.254.169.254/latest/meta-data",
		"https://192.0.2.1/image.png",
		"https://198.18.0.1/image.png",
		"https://[::1]/image.png",
		"https://[fc00::1]/image.png",
		"https://user:password@example.com/image.png",
	}
	for _, rawURL := range testCases {
		t.Run(rawURL, func(t *testing.T) {
			_, err := validateGeneratedAssetSourceURL(rawURL)
			require.Error(t, err)
		})
	}
}

func TestGeneratedAssetGatewayStoreContractAndSignedURL(t *testing.T) {
	const (
		secret    = "gateway-test-secret"
		bucket    = "opencodex-video-generated-assets"
		objectKey = "images/41/2026/07/24/img_gateway.png"
	)
	imageBytes := generatedAssetTestPNG(t)
	var stored []byte
	var storedContentType string
	var storedSHA string
	var storedRequestId string
	var storedExpiresAt string

	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/objects/"+objectKey, request.URL.Path)
		if request.Method == http.MethodGet && request.Header.Get("Authorization") == "" {
			expires, err := strconv.ParseInt(request.URL.Query().Get("expires"), 10, 64)
			require.NoError(t, err)
			mac := hmac.New(sha256.New, []byte(secret))
			_, _ = mac.Write([]byte(generatedAssetGatewaySignaturePayload(bucket, objectKey, expires)))
			require.Equal(t, base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), request.URL.Query().Get("sig"))
		} else {
			require.Equal(t, "Bearer "+secret, request.Header.Get("Authorization"))
			require.Equal(t, bucket, request.Header.Get("X-Opencodex-Bucket"))
		}

		switch request.Method {
		case http.MethodPut:
			body, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			stored = body
			storedContentType = request.Header.Get("Content-Type")
			storedSHA = request.Header.Get("X-Opencodex-Meta-SHA256")
			storedRequestId = request.Header.Get("X-Opencodex-Meta-Request-Id")
			storedExpiresAt = request.Header.Get("X-Opencodex-Meta-Expires-At")
			writer.WriteHeader(http.StatusNoContent)
		case http.MethodGet:
			if stored == nil {
				http.NotFound(writer, request)
				return
			}
			writer.Header().Set("Content-Type", storedContentType)
			_, _ = writer.Write(stored)
		case http.MethodDelete:
			stored = nil
			writer.WriteHeader(http.StatusNoContent)
		default:
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	store, err := newGeneratedAssetGatewayStore(generatedAssetStorageConfig{
		GatewayEndpoint: server.URL,
		GatewaySecret:   secret,
		Bucket:          bucket,
		MaxAssetSize:    1024 * 1024,
	})
	require.NoError(t, err)
	store.client = server.Client()

	metadata := map[string]string{
		"sha256":     strings.Repeat("a", 64),
		"request-id": "request-gateway-contract",
		"expires-at": "1784894400",
	}
	require.NoError(t, store.Put(context.Background(), objectKey, "image/png", imageBytes, metadata))
	require.Equal(t, "image/png", storedContentType)
	require.Equal(t, metadata["sha256"], storedSHA)
	require.Equal(t, metadata["request-id"], storedRequestId)
	require.Equal(t, metadata["expires-at"], storedExpiresAt)

	readBack, contentType, err := store.Get(context.Background(), objectKey)
	require.NoError(t, err)
	require.Equal(t, imageBytes, readBack)
	require.Equal(t, "image/png", contentType)

	signedURL, err := store.PresignGet(context.Background(), objectKey, 5*time.Minute)
	require.NoError(t, err)
	signedResponse, err := server.Client().Get(signedURL)
	require.NoError(t, err)
	defer signedResponse.Body.Close()
	require.Equal(t, http.StatusOK, signedResponse.StatusCode)
	signedBody, err := io.ReadAll(signedResponse.Body)
	require.NoError(t, err)
	require.Equal(t, imageBytes, signedBody)

	require.NoError(t, store.Delete(context.Background(), objectKey))
	_, _, err = store.Get(context.Background(), objectKey)
	require.Error(t, err)
}
