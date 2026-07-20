package sora

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

const grokVideoAPIStyle = "grok"

var grokVideoDurations = map[int]bool{5: true, 10: true, 15: true}

type grokVideoReference struct {
	URL string `json:"url"`
}

type grokVideoTask struct {
	RequestID  string `json:"request_id"`
	Model      string `json:"model,omitempty"`
	Status     string `json:"status,omitempty"`
	Progress   any    `json:"progress,omitempty"`
	Duration   int    `json:"duration,omitempty"`
	Resolution string `json:"resolution,omitempty"`
	Video      *struct {
		URL string `json:"url"`
	} `json:"video,omitempty"`
	Error *struct {
		Message string `json:"message"`
		Code    string `json:"code,omitempty"`
	} `json:"error,omitempty"`
}

func (a *TaskAdaptor) isGrokVideoAPI() bool {
	return a.videoStyle == grokVideoAPIStyle
}

func fixedGrokVideoDuration(modelName string) (int, bool) {
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	for _, duration := range []int{5, 10, 15} {
		if strings.HasSuffix(modelName, fmt.Sprintf("-%ds", duration)) {
			return duration, true
		}
	}
	return 0, false
}

func requestedGrokVideoDuration(req relaycommon.TaskSubmitReq) (int, error) {
	duration := req.Duration
	if strings.TrimSpace(req.Seconds) != "" {
		seconds, err := strconv.Atoi(strings.TrimSpace(req.Seconds))
		if err != nil {
			return 0, fmt.Errorf("seconds must be an integer")
		}
		if duration != 0 && duration != seconds {
			return 0, fmt.Errorf("duration and seconds must match")
		}
		duration = seconds
	}
	if fixedDuration, ok := fixedGrokVideoDuration(req.Model); ok {
		if duration != 0 && duration != fixedDuration {
			return 0, fmt.Errorf("model %s has a fixed duration of %d seconds", req.Model, fixedDuration)
		}
		duration = fixedDuration
	}
	if !grokVideoDurations[duration] {
		return 0, fmt.Errorf("duration must be one of 5, 10, or 15 seconds")
	}
	return duration, nil
}

func aspectRatioFromSize(size string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(size)) {
	case "1280x720", "1792x1024":
		return "16:9", true
	case "720x1280", "1024x1792":
		return "9:16", true
	case "720x720", "1024x1024":
		return "1:1", true
	default:
		return "", false
	}
}

func normalizedGrokAspectRatio(req relaycommon.TaskSubmitReq) (string, error) {
	aspectRatio := strings.TrimSpace(req.AspectRatio)
	if aspectRatio == "" {
		aspectRatio = "16:9"
	}
	if req.Size != "" {
		sizeRatio, ok := aspectRatioFromSize(req.Size)
		if !ok {
			return "", fmt.Errorf("size must map to 16:9, 9:16, or 1:1 at 720p")
		}
		if req.AspectRatio != "" && sizeRatio != aspectRatio {
			return "", fmt.Errorf("size and aspect_ratio must describe the same ratio")
		}
		aspectRatio = sizeRatio
	}
	switch aspectRatio {
	case "16:9", "9:16", "1:1":
		return aspectRatio, nil
	default:
		return "", fmt.Errorf("aspect_ratio must be one of 16:9, 9:16, or 1:1")
	}
}

func grokReferenceURLs(req relaycommon.TaskSubmitReq) []string {
	candidates := make([]string, 0, len(req.Images)+2)
	candidates = append(candidates, req.Image)
	candidates = append(candidates, req.Images...)
	candidates = append(candidates, req.InputReference)
	seen := make(map[string]bool, len(candidates))
	urls := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		urls = append(urls, candidate)
	}
	return urls
}

func validateGrokReferenceURLs(urls []string) error {
	for _, imageURL := range urls {
		if !strings.HasPrefix(strings.ToLower(imageURL), "https://") {
			return fmt.Errorf("reference images must use public HTTPS URLs")
		}
	}
	return nil
}

func grokRequestError(err error) *dto.TaskError {
	return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
}

func (a *TaskAdaptor) validateGrokVideoRequest(c *gin.Context) *dto.TaskError {
	if strings.Contains(strings.ToLower(c.GetHeader("Content-Type")), "multipart/form-data") {
		form, err := c.MultipartForm()
		if err != nil {
			return grokRequestError(fmt.Errorf("invalid multipart form: %w", err))
		}
		if form != nil && len(form.File) > 0 {
			return grokRequestError(fmt.Errorf("this video channel requires reference images as public HTTPS URLs in JSON"))
		}
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return grokRequestError(err)
	}
	duration, err := requestedGrokVideoDuration(req)
	if err != nil {
		return grokRequestError(err)
	}
	if req.Resolution != "" && !strings.EqualFold(strings.TrimSpace(req.Resolution), "720p") {
		return grokRequestError(fmt.Errorf("resolution must be 720p"))
	}
	if _, err = normalizedGrokAspectRatio(req); err != nil {
		return grokRequestError(err)
	}
	if err = validateGrokReferenceURLs(grokReferenceURLs(req)); err != nil {
		return grokRequestError(err)
	}
	req.Duration = duration
	req.Seconds = strconv.Itoa(duration)
	req.Resolution = "720p"
	c.Set("task_request", req)
	return nil
}

func (a *TaskAdaptor) buildGrokVideoRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}
	duration, err := requestedGrokVideoDuration(req)
	if err != nil {
		return nil, err
	}
	aspectRatio, err := normalizedGrokAspectRatio(req)
	if err != nil {
		return nil, err
	}
	urls := grokReferenceURLs(req)
	if err = validateGrokReferenceURLs(urls); err != nil {
		return nil, err
	}
	modelName := strings.TrimSpace(info.UpstreamModelName)
	if modelName == "" {
		modelName = strings.TrimSpace(req.Model)
	}
	payload := map[string]any{
		"model":        modelName,
		"prompt":       req.Prompt,
		"duration":     duration,
		"resolution":   "720p",
		"aspect_ratio": aspectRatio,
	}
	if len(urls) == 1 {
		payload["image"] = grokVideoReference{URL: urls[0]}
	} else if len(urls) > 1 {
		references := make([]grokVideoReference, 0, len(urls))
		for _, imageURL := range urls {
			references = append(references, grokVideoReference{URL: imageURL})
		}
		payload["images"] = references
	}
	body, err := common.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(body), nil
}

func (a *TaskAdaptor) doGrokVideoResponse(c *gin.Context, responseBody []byte, info *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
	var upstream grokVideoTask
	if err := common.Unmarshal(responseBody, &upstream); err != nil {
		return "", nil, service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
	}
	if strings.TrimSpace(upstream.RequestID) == "" {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("request_id is empty"), "invalid_response", http.StatusInternalServerError)
	}
	public := responseTask{
		ID:        info.PublicTaskID,
		TaskID:    info.PublicTaskID,
		Object:    "video",
		Model:     info.OriginModelName,
		Status:    "queued",
		Progress:  0,
		CreatedAt: time.Now().Unix(),
	}
	c.JSON(http.StatusOK, public)
	return upstream.RequestID, responseBody, nil
}

func parseGrokProgress(progress any) string {
	switch value := progress.(type) {
	case float64:
		if value >= 0 && value <= 1 {
			value *= 100
		}
		return fmt.Sprintf("%.0f%%", value)
	case string:
		value = strings.TrimSpace(value)
		if value == "" {
			return ""
		}
		if strings.HasSuffix(value, "%") {
			return value
		}
		if number, err := strconv.ParseFloat(value, 64); err == nil {
			return parseGrokProgress(number)
		}
	}
	return ""
}

func parseGrokVideoTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var upstream grokVideoTask
	if err := common.Unmarshal(respBody, &upstream); err != nil {
		return nil, errors.Wrap(err, "unmarshal grok video task result failed")
	}
	result := &relaycommon.TaskInfo{Code: 0, Progress: parseGrokProgress(upstream.Progress)}
	switch strings.ToLower(strings.TrimSpace(upstream.Status)) {
	case "pending", "queued":
		result.Status = model.TaskStatusQueued
	case "processing", "in_progress":
		result.Status = model.TaskStatusInProgress
	case "done", "completed", "success", "succeeded":
		result.Progress = "100%"
		if upstream.Video != nil {
			result.Url = strings.TrimSpace(upstream.Video.URL)
		}
		if result.Url == "" {
			result.Status = model.TaskStatusFailure
			result.Reason = "completed task did not include video.url"
		} else {
			result.Status = model.TaskStatusSuccess
		}
	case "failed", "failure", "error", "cancelled", "canceled":
		result.Status = model.TaskStatusFailure
		result.Progress = "100%"
		if upstream.Error != nil && strings.TrimSpace(upstream.Error.Message) != "" {
			result.Reason = strings.TrimSpace(upstream.Error.Message)
		} else {
			result.Reason = "task failed"
		}
	case "":
		if upstream.Error != nil && strings.TrimSpace(upstream.Error.Message) != "" {
			result.Status = model.TaskStatusFailure
			result.Progress = "100%"
			result.Reason = strings.TrimSpace(upstream.Error.Message)
		}
	}
	return result, nil
}

func looksLikeGrokVideoTask(task grokVideoTask) bool {
	return task.RequestID != "" || task.Video != nil || strings.EqualFold(task.Status, "done")
}

func convertGrokTaskToOpenAIVideo(task *model.Task) ([]byte, bool, error) {
	var upstream grokVideoTask
	if err := common.Unmarshal(task.Data, &upstream); err != nil || !looksLikeGrokVideoTask(upstream) {
		return nil, false, nil
	}
	public := responseTask{
		ID:          task.TaskID,
		TaskID:      task.TaskID,
		Object:      "video",
		Model:       task.Properties.OriginModelName,
		Status:      task.Status.ToVideoStatus(),
		CreatedAt:   task.CreatedAt,
		CompletedAt: task.FinishTime,
		VideoURL:    task.GetResultURL(),
	}
	if public.Status == dto.VideoStatusCompleted {
		public.Progress = 100
	} else if progress := parseGrokProgress(upstream.Progress); progress != "" {
		progress = strings.TrimSuffix(progress, "%")
		public.Progress, _ = strconv.Atoi(progress)
	}
	if public.Status == dto.VideoStatusFailed {
		public.Error = &struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		}{Message: task.FailReason, Code: "video_generation_failed"}
	}
	body, err := common.Marshal(public)
	if err != nil {
		return nil, true, err
	}
	return body, true, nil
}
