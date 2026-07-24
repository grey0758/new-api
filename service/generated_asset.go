package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	_ "golang.org/x/image/webp"
	"gorm.io/gorm"
)

var (
	ErrGeneratedAssetNotFound = errors.New("generated asset not found")
	ErrGeneratedAssetExpired  = errors.New("generated asset expired")
)

const (
	GeneratedImageDecisionProceed = "proceed"
	GeneratedImageDecisionReplay  = "replay"
	GeneratedImageDecisionReject  = "reject"
)

type GeneratedImageRequestDecision struct {
	Action       string
	RequestId    string
	StatusCode   int
	ErrorCode    string
	ErrorMessage string
	ResponseBody []byte
}

type GeneratedAssetTask struct {
	Object      string `json:"object"`
	TaskId      string `json:"task_id"`
	Status      string `json:"status"`
	Model       string `json:"model"`
	RequestId   string `json:"request_id"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	SHA256      string `json:"sha256"`
	ContentURL  string `json:"content_url"`
	CreatedAt   int64  `json:"created_at"`
	ExpiresAt   int64  `json:"expires_at"`
}

type GeneratedAssetTaskList struct {
	Object   string               `json:"object"`
	Data     []GeneratedAssetTask `json:"data"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
	Total    int64                `json:"total"`
}

func PrepareGeneratedImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ImageRequest) (*GeneratedImageRequestDecision, error) {
	if c == nil || info == nil || request == nil {
		return nil, errors.New("invalid generated image request context")
	}
	runtime, err := getGeneratedAssetRuntime()
	if err != nil {
		return nil, err
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	requestBody, err := storage.Bytes()
	if err != nil {
		return nil, err
	}
	requestHash := generatedImageRequestHash(c, requestBody)

	idempotencyKey, external, err := generatedImageIdempotencyKey(c, info.RequestId)
	if err != nil {
		return &GeneratedImageRequestDecision{
			Action:       GeneratedImageDecisionReject,
			StatusCode:   http.StatusBadRequest,
			ErrorCode:    "invalid_idempotency_key",
			ErrorMessage: err.Error(),
		}, nil
	}
	idempotencyDigest := sha256.Sum256([]byte(idempotencyKey))
	responseFormat := strings.ToLower(strings.TrimSpace(request.ResponseFormat))
	if responseFormat == "" {
		responseFormat = "url"
	}
	expiresAt := common.GetTimestamp() + int64(runtime.config.Retention.Seconds())
	record, created, err := model.BeginGeneratedImageRequest(&model.GeneratedImageRequest{
		UserId:               info.UserId,
		TokenId:              info.TokenId,
		RequestId:            info.RequestId,
		IdempotencyKeyDigest: hex.EncodeToString(idempotencyDigest[:]),
		RequestHash:          hex.EncodeToString(requestHash[:]),
		Model:                info.OriginModelName,
		ResponseFormat:       responseFormat,
		Status:               model.GeneratedImageRequestStatusProcessing,
		ExpiresAt:            expiresAt,
	})
	if err != nil {
		return nil, err
	}
	if created {
		if external {
			c.Header("Idempotency-Key-Accepted", "true")
		}
		return &GeneratedImageRequestDecision{
			Action:    GeneratedImageDecisionProceed,
			RequestId: record.RequestId,
		}, nil
	}

	if record.RequestHash != hex.EncodeToString(requestHash[:]) {
		return &GeneratedImageRequestDecision{
			Action:       GeneratedImageDecisionReject,
			RequestId:    record.RequestId,
			StatusCode:   http.StatusConflict,
			ErrorCode:    "idempotency_key_conflict",
			ErrorMessage: "the idempotency key was already used with a different image request",
		}, nil
	}

	switch record.Status {
	case model.GeneratedImageRequestStatusStored, model.GeneratedImageRequestStatusSucceeded:
		if record.ExpiresAt > 0 && record.ExpiresAt <= common.GetTimestamp() {
			return &GeneratedImageRequestDecision{
				Action:       GeneratedImageDecisionReject,
				RequestId:    record.RequestId,
				StatusCode:   http.StatusGone,
				ErrorCode:    "idempotency_asset_expired",
				ErrorMessage: "the original generated image has expired and this idempotency key will not call the provider again",
			}, nil
		}
		body, err := buildGeneratedImageReplayResponse(c, record, runtime)
		if err != nil {
			return nil, err
		}
		return &GeneratedImageRequestDecision{
			Action:       GeneratedImageDecisionReplay,
			RequestId:    record.RequestId,
			StatusCode:   http.StatusOK,
			ResponseBody: body,
		}, nil
	case model.GeneratedImageRequestStatusProcessing:
		return &GeneratedImageRequestDecision{
			Action:       GeneratedImageDecisionReject,
			RequestId:    record.RequestId,
			StatusCode:   http.StatusConflict,
			ErrorCode:    "idempotency_request_in_progress",
			ErrorMessage: "the original image request is still processing or has an unknown upstream result",
		}, nil
	case model.GeneratedImageRequestStatusFailed, model.GeneratedImageRequestStatusStorageFailed:
		statusCode := record.ErrorStatus
		if statusCode < 400 || statusCode > 599 {
			statusCode = http.StatusConflict
		}
		errorCode := record.ErrorCode
		if errorCode == "" {
			errorCode = "idempotency_request_failed"
		}
		return &GeneratedImageRequestDecision{
			Action:       GeneratedImageDecisionReject,
			RequestId:    record.RequestId,
			StatusCode:   statusCode,
			ErrorCode:    errorCode,
			ErrorMessage: "the original image request failed; reuse of this idempotency key will not call the provider again",
		}, nil
	default:
		return &GeneratedImageRequestDecision{
			Action:       GeneratedImageDecisionReject,
			RequestId:    record.RequestId,
			StatusCode:   http.StatusConflict,
			ErrorCode:    "idempotency_request_unknown",
			ErrorMessage: "the original image request is in an unknown state and will not be replayed",
		}, nil
	}
}

func generatedImageIdempotencyKey(c *gin.Context, internalRequestId string) (string, bool, error) {
	for _, header := range []string{"Idempotency-Key", "X-Request-Id", "X-Oneapi-Request-Id"} {
		value := strings.TrimSpace(c.GetHeader(header))
		if value == "" {
			continue
		}
		if len(value) > 256 {
			return "", false, errors.New("idempotency key exceeds 256 characters")
		}
		for _, character := range value {
			if character < 0x21 || character == 0x7f {
				return "", false, errors.New("idempotency key contains invalid characters")
			}
		}
		return header + ":" + value, true, nil
	}
	return "internal-request:" + internalRequestId, false, nil
}

func generatedImageRequestHash(c *gin.Context, requestBody []byte) [sha256.Size]byte {
	method := ""
	requestPath := ""
	if c != nil && c.Request != nil {
		method = strings.ToUpper(strings.TrimSpace(c.Request.Method))
		if c.Request.URL != nil {
			requestPath = c.Request.URL.EscapedPath()
		}
	}
	payload := make([]byte, 0, len(method)+len(requestPath)+len(requestBody)+2)
	payload = append(payload, method...)
	payload = append(payload, '\n')
	payload = append(payload, requestPath...)
	payload = append(payload, '\n')
	payload = append(payload, requestBody...)
	return sha256.Sum256(payload)
}

func PersistGeneratedImageResponse(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ImageRequest, response *dto.ImageResponse) error {
	if c == nil || info == nil || request == nil || response == nil {
		return errors.New("invalid generated image persistence input")
	}
	runtime, err := getGeneratedAssetRuntime()
	if err != nil {
		return err
	}
	if len(response.Data) == 0 {
		return markGeneratedImageStorageFailure(info.RequestId, errors.New("image response contains no data"))
	}

	expiresAt := common.GetTimestamp() + int64(runtime.config.Retention.Seconds())
	uploadedKeys := make([]string, 0, len(response.Data))
	assets := make([]model.GeneratedAsset, 0, len(response.Data))
	persistedData := make([]dto.ImageData, 0, len(response.Data))
	wantsBase64 := strings.EqualFold(strings.TrimSpace(request.ResponseFormat), "b64_json")

	for index, imageData := range response.Data {
		payload, sourceContentType, err := generatedImagePayload(c.Request.Context(), imageData, runtime.config.MaxAssetSize)
		if err != nil {
			cleanupGeneratedAssetObjects(runtime.store, uploadedKeys)
			return markGeneratedImageStorageFailure(info.RequestId, err)
		}
		contentType, extension, width, height, err := inspectGeneratedImagePayload(payload, sourceContentType)
		if err != nil {
			cleanupGeneratedAssetObjects(runtime.store, uploadedKeys)
			return markGeneratedImageStorageFailure(info.RequestId, err)
		}
		digest := sha256.Sum256(payload)
		digestHex := hex.EncodeToString(digest[:])

		taskId := generatedAssetTaskId(info.RequestId, index)
		objectKey := generatedAssetObjectKey(info.UserId, taskId, extension)
		metadata := map[string]string{
			"sha256":     digestHex,
			"request-id": info.RequestId,
			"expires-at": strconv.FormatInt(expiresAt, 10),
		}
		if err := runtime.store.Put(c.Request.Context(), objectKey, contentType, payload, metadata); err != nil {
			cleanupGeneratedAssetObjects(runtime.store, uploadedKeys)
			return markGeneratedImageStorageFailure(info.RequestId, fmt.Errorf("store generated image %d: %w", index, err))
		}
		uploadedKeys = append(uploadedKeys, objectKey)
		assets = append(assets, model.GeneratedAsset{
			TaskId:      taskId,
			RequestId:   info.RequestId,
			UserId:      info.UserId,
			TokenId:     info.TokenId,
			Model:       info.OriginModelName,
			ChannelId:   info.ChannelId,
			ObjectKey:   objectKey,
			ContentType: contentType,
			SizeBytes:   int64(len(payload)),
			Width:       width,
			Height:      height,
			SHA256:      digestHex,
			ExpiresAt:   expiresAt,
		})

		persisted := dto.ImageData{
			RevisedPrompt: imageData.RevisedPrompt,
			TaskId:        taskId,
		}
		if wantsBase64 {
			persisted.B64Json = base64.StdEncoding.EncodeToString(payload)
		} else {
			persisted.Url = generatedAssetContentURL(c, taskId)
		}
		persistedData = append(persistedData, persisted)
	}

	if err := model.CreateGeneratedAssetsAndMarkStored(info.RequestId, assets); err != nil {
		cleanupGeneratedAssetObjects(runtime.store, uploadedKeys)
		return markGeneratedImageStorageFailure(info.RequestId, fmt.Errorf("record generated images: %w", err))
	}
	response.Data = persistedData
	response.TaskId = persistedData[0].TaskId
	return nil
}

func generatedImagePayload(ctx context.Context, data dto.ImageData, maxBytes int64) ([]byte, string, error) {
	if strings.TrimSpace(data.B64Json) != "" {
		decoded, err := decodeGeneratedImageBase64(data.B64Json, maxBytes)
		return decoded, "", err
	}
	if strings.TrimSpace(data.Url) == "" {
		return nil, "", errors.New("image response contains neither url nor b64_json")
	}
	return downloadGeneratedAsset(ctx, data.Url, maxBytes)
}

func decodeGeneratedImageBase64(value string, maxBytes int64) ([]byte, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(strings.ToLower(value), "data:") {
		comma := strings.IndexByte(value, ',')
		if comma < 0 {
			return nil, errors.New("invalid generated image data URL")
		}
		value = value[comma+1:]
	}
	if int64(len(value)) > ((maxBytes+2)/3)*4+16 {
		return nil, errors.New("generated image base64 payload is too large")
	}
	for _, encoding := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		decoded, err := encoding.DecodeString(value)
		if err == nil {
			if int64(len(decoded)) > maxBytes {
				return nil, errors.New("generated image payload is too large")
			}
			return decoded, nil
		}
	}
	return nil, errors.New("invalid generated image base64 payload")
}

func inspectGeneratedImagePayload(data []byte, hintedContentType string) (string, string, int, int, error) {
	if len(data) == 0 {
		return "", "", 0, 0, errors.New("generated image payload is empty")
	}
	detectedContentType := strings.TrimSpace(strings.Split(http.DetectContentType(data), ";")[0])
	switch detectedContentType {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return "", "", 0, 0, fmt.Errorf("unsupported generated image content type %q", detectedContentType)
	}
	if hintedContentType != "" && strings.HasPrefix(strings.ToLower(hintedContentType), "image/") &&
		!strings.EqualFold(hintedContentType, detectedContentType) {
		return "", "", 0, 0, errors.New("generated image content type does not match payload")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", "", 0, 0, fmt.Errorf("decode generated image dimensions: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > 32768 || config.Height > 32768 {
		return "", "", 0, 0, errors.New("generated image dimensions are invalid")
	}
	extensions := map[string]string{
		"image/png":  ".png",
		"image/jpeg": ".jpg",
		"image/gif":  ".gif",
		"image/webp": ".webp",
	}
	return detectedContentType, extensions[detectedContentType], config.Width, config.Height, nil
}

func generatedAssetObjectKey(userId int, taskId string, extension string) string {
	now := time.Now().UTC()
	return path.Join(
		"images",
		strconv.Itoa(userId),
		now.Format("2006"),
		now.Format("01"),
		now.Format("02"),
		taskId+extension,
	)
}

func generatedAssetTaskId(requestId string, index int) string {
	digest := sha256.Sum256([]byte(requestId + ":" + strconv.Itoa(index)))
	return "img_" + hex.EncodeToString(digest[:16])
}

func cleanupGeneratedAssetObjects(store generatedAssetObjectStore, objectKeys []string) {
	if store == nil {
		return
	}
	for _, objectKey := range objectKeys {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		_ = store.Delete(ctx, objectKey)
		cancel()
	}
}

func markGeneratedImageStorageFailure(requestId string, err error) error {
	_ = MarkGeneratedImageRequestStorageFailed(requestId, http.StatusServiceUnavailable, "generated_asset_persistence_failed")
	return err
}

func buildGeneratedImageReplayResponse(c *gin.Context, request *model.GeneratedImageRequest, runtime *generatedAssetRuntime) ([]byte, error) {
	assets, err := model.GetGeneratedAssetsByRequestId(request.RequestId)
	if err != nil {
		return nil, err
	}
	if len(assets) == 0 {
		return nil, errors.New("stored generated image request has no assets")
	}
	response := dto.ImageResponse{
		Created: assets[0].CreatedAt,
		TaskId:  assets[0].TaskId,
		Data:    make([]dto.ImageData, 0, len(assets)),
	}
	wantsBase64 := strings.EqualFold(request.ResponseFormat, "b64_json")
	for _, asset := range assets {
		item := dto.ImageData{TaskId: asset.TaskId}
		if wantsBase64 {
			data, _, err := runtime.store.Get(c.Request.Context(), asset.ObjectKey)
			if err != nil {
				return nil, err
			}
			item.B64Json = base64.StdEncoding.EncodeToString(data)
		} else {
			item.Url = generatedAssetContentURL(c, asset.TaskId)
		}
		response.Data = append(response.Data, item)
	}
	return common.Marshal(response)
}

func MarkGeneratedImageRequestSucceeded(requestId string) error {
	return model.MarkGeneratedImageRequestSucceeded(requestId)
}

func MarkGeneratedImageRequestFailed(requestId string, statusCode int, errorCode string) error {
	return model.MarkGeneratedImageRequestFailed(
		requestId,
		model.GeneratedImageRequestStatusFailed,
		statusCode,
		errorCode,
	)
}

func MarkGeneratedImageRequestStorageFailed(requestId string, statusCode int, errorCode string) error {
	return model.MarkGeneratedImageRequestFailed(
		requestId,
		model.GeneratedImageRequestStatusStorageFailed,
		statusCode,
		errorCode,
	)
}

func GetGeneratedAssetTask(c *gin.Context, userId int, taskId string) (*GeneratedAssetTask, error) {
	asset, err := model.GetGeneratedAssetByTaskIdAndUser(taskId, userId)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrGeneratedAssetNotFound
	}
	if err != nil {
		return nil, err
	}
	task := generatedAssetTaskFromModel(c, *asset)
	return &task, nil
}

func ListGeneratedAssetTasks(c *gin.Context, userId int, page int, pageSize int) (*GeneratedAssetTaskList, error) {
	assets, total, err := model.ListGeneratedAssetsByUser(userId, page, pageSize)
	if err != nil {
		return nil, err
	}
	tasks := make([]GeneratedAssetTask, 0, len(assets))
	for _, asset := range assets {
		tasks = append(tasks, generatedAssetTaskFromModel(c, asset))
	}
	return &GeneratedAssetTaskList{
		Object:   "list",
		Data:     tasks,
		Page:     page,
		PageSize: pageSize,
		Total:    total,
	}, nil
}

func PresignGeneratedAssetContent(c *gin.Context, userId int, taskId string) (string, error) {
	asset, err := model.GetGeneratedAssetByTaskIdAndUser(taskId, userId)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", ErrGeneratedAssetNotFound
	}
	if err != nil {
		return "", err
	}
	if asset.ExpiresAt > 0 && asset.ExpiresAt <= common.GetTimestamp() {
		return "", ErrGeneratedAssetExpired
	}
	runtime, err := getGeneratedAssetRuntime()
	if err != nil {
		return "", err
	}
	return runtime.store.PresignGet(c.Request.Context(), asset.ObjectKey, runtime.config.PresignTTL)
}

func generatedAssetTaskFromModel(c *gin.Context, asset model.GeneratedAsset) GeneratedAssetTask {
	status := "completed"
	if asset.ExpiresAt > 0 && asset.ExpiresAt <= common.GetTimestamp() {
		status = "expired"
	}
	return GeneratedAssetTask{
		Object:      "image.task",
		TaskId:      asset.TaskId,
		Status:      status,
		Model:       asset.Model,
		RequestId:   asset.RequestId,
		ContentType: asset.ContentType,
		SizeBytes:   asset.SizeBytes,
		Width:       asset.Width,
		Height:      asset.Height,
		SHA256:      asset.SHA256,
		ContentURL:  generatedAssetContentURL(c, asset.TaskId),
		CreatedAt:   asset.CreatedAt,
		ExpiresAt:   asset.ExpiresAt,
	}
}

func generatedAssetContentURL(c *gin.Context, taskId string) string {
	baseURL := strings.TrimRight(strings.TrimSpace(system_setting.ServerAddress), "/")
	if baseURL == "" && c != nil && c.Request != nil {
		scheme := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto"))
		if scheme == "" {
			scheme = "http"
			if c.Request.TLS != nil {
				scheme = "https"
			}
		}
		host := strings.TrimSpace(c.Request.Host)
		if parsed, err := url.Parse(scheme + "://" + host); err == nil && parsed.Host != "" {
			baseURL = parsed.Scheme + "://" + parsed.Host
		}
	}
	contentPath := "/v1/images/tasks/" + url.PathEscape(taskId) + "/content"
	if c != nil && c.Request != nil && strings.HasPrefix(c.Request.URL.Path, "/api/") {
		contentPath = "/api/image/tasks/" + url.PathEscape(taskId) + "/content"
	}
	return baseURL + contentPath
}
