package sora

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newGrokVideoAdaptor() *TaskAdaptor {
	adaptor := &TaskAdaptor{}
	adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ChannelBaseUrl: "https://video.example.com/",
		ApiKey:         "test-key",
		ChannelOtherSettings: dto.ChannelOtherSettings{
			VideoAPIStyle: grokVideoAPIStyle,
		},
	}})
	return adaptor
}

func TestGrokVideoFixedDurationAndAspectRatio(t *testing.T) {
	duration, err := requestedGrokVideoDuration(relaycommon.TaskSubmitReq{
		Model:   "seedance-2.0-fast-10s",
		Seconds: "10",
	})
	require.NoError(t, err)
	require.Equal(t, 10, duration)

	_, err = requestedGrokVideoDuration(relaycommon.TaskSubmitReq{
		Model:    "seedance-2.0-fast-10s",
		Duration: 5,
	})
	require.ErrorContains(t, err, "fixed duration of 10 seconds")

	aspectRatio, err := normalizedGrokAspectRatio(relaycommon.TaskSubmitReq{Size: "720x1280"})
	require.NoError(t, err)
	require.Equal(t, "9:16", aspectRatio)
}

func TestBuildGrokVideoRequestBodyNormalizesCurrentVideoShape(t *testing.T) {
	adaptor := newGrokVideoAdaptor()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("task_request", relaycommon.TaskSubmitReq{
		Model:    "seedance-2.0-fast-5s",
		Prompt:   "a tiny robot chef",
		Seconds:  "5",
		Size:     "1280x720",
		Image:    "https://cdn.example.com/robot.jpg",
		Images:   []string{"https://cdn.example.com/robot.jpg"},
		Duration: 5,
	})
	info := &relaycommon.RelayInfo{
		OriginModelName: "seedance-2.0-fast-5s",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "video-v1",
		},
	}

	bodyReader, err := adaptor.BuildRequestBody(ctx, info)
	require.NoError(t, err)
	body, err := io.ReadAll(bodyReader)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(body, &payload))
	require.Equal(t, "video-v1", payload["model"])
	require.Equal(t, float64(5), payload["duration"])
	require.Equal(t, "720p", payload["resolution"])
	require.Equal(t, "16:9", payload["aspect_ratio"])
	require.Equal(t, map[string]any{"url": "https://cdn.example.com/robot.jpg"}, payload["image"])
	require.NotContains(t, payload, "seconds")
	require.NotContains(t, payload, "size")
	require.Nil(t, adaptor.EstimateBilling(ctx, info))

	url, err := adaptor.BuildRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://video.example.com/v1/videos/generations", url)
}

func TestGrokVideoSubmitAndPollResponses(t *testing.T) {
	adaptor := newGrokVideoAdaptor()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	info := &relaycommon.RelayInfo{
		OriginModelName: "seedance-2.0-fast-5s",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
	}

	upstreamID, raw, taskErr := adaptor.doGrokVideoResponse(ctx, []byte(`{"request_id":"request_upstream"}`), info)
	require.Nil(t, taskErr)
	require.Equal(t, "request_upstream", upstreamID)
	require.JSONEq(t, `{"request_id":"request_upstream"}`, string(raw))
	require.Equal(t, 200, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"id":"task_public"`)
	require.Contains(t, recorder.Body.String(), `"task_id":"task_public"`)
	require.NotContains(t, recorder.Body.String(), "request_upstream")

	result, err := adaptor.ParseTaskResult([]byte(`{"request_id":"request_upstream","status":"done","video":{"url":"https://cdn.example.com/output.mp4"}}`))
	require.NoError(t, err)
	require.Equal(t, "SUCCESS", result.Status)
	require.Equal(t, "100%", result.Progress)
	require.Equal(t, "https://cdn.example.com/output.mp4", result.Url)
}

func TestGrokVideoFetchUsesTaskEndpoint(t *testing.T) {
	service.InitHttpClient()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/videos/request_upstream", r.URL.Path)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"request_id":"request_upstream","status":"pending"}`))
	}))
	defer server.Close()

	adaptor := newGrokVideoAdaptor()
	response, err := adaptor.FetchTask(server.URL+"/", "test-key", map[string]any{"task_id": "request_upstream"}, "")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.NoError(t, response.Body.Close())
}

func TestConvertGrokTaskToOpenAIVideoUsesPublicIdentity(t *testing.T) {
	task := &model.Task{
		TaskID:     "task_public",
		CreatedAt:  10,
		FinishTime: 20,
		Status:     model.TaskStatusSuccess,
		Properties: model.Properties{OriginModelName: "seedance-2.0-fast-5s"},
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "request_upstream",
			ResultURL:      "https://cdn.example.com/output.mp4",
		},
		Data: []byte(`{"request_id":"request_upstream","status":"done","video":{"url":"https://cdn.example.com/output.mp4"}}`),
	}

	body, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)
	var response map[string]any
	require.NoError(t, json.Unmarshal(body, &response))
	require.Equal(t, "task_public", response["id"])
	require.Equal(t, "task_public", response["task_id"])
	require.Equal(t, dto.VideoStatusCompleted, response["status"])
	require.Equal(t, "https://cdn.example.com/output.mp4", response["video_url"])
	require.NotContains(t, string(body), "request_upstream")
}
