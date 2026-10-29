package service

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// Explicitly scoped to this instance's Flow channel, never inferred from a
// generic OpenAI model name. Existing synchronous routes remain unchanged.
func IsFlowImageJobChannel(id int) bool {
	configured, err := strconv.Atoi(os.Getenv("IMAGE_JOBS_FLOW_CHANNEL_ID"))
	return err == nil && configured > 0 && id == configured
}

func BuildFlowImageJobRequest(c *gin.Context, upstreamModel string) ([]byte, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	body, err := storage.Bytes()
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if common.Unmarshal(body, &fields) != nil {
		return nil, errors.New("invalid Flow image request")
	}
	var prompt, format string
	var n *uint
	if common.Unmarshal(fields["prompt"], &prompt) != nil || strings.TrimSpace(prompt) == "" {
		return nil, errors.New("prompt is required")
	}
	if raw, ok := fields["n"]; ok && common.Unmarshal(raw, &n) != nil {
		return nil, errors.New("invalid image count")
	}
	if n != nil && *n != 1 {
		return nil, errors.New("asynchronous Flow images require n=1")
	}
	_ = common.Unmarshal(fields["response_format"], &format)
	if format != "" && format != "url" {
		return nil, errors.New("asynchronous Flow supports url responses with inline fallback")
	}
	if _, ok := fields["messages"]; ok {
		return nil, errors.New("use prompt and image_urls for image jobs")
	}
	var images []string
	if raw, ok := fields["image_urls"]; ok {
		if common.Unmarshal(raw, &images) != nil {
			return nil, errors.New("image_urls must be an ordered string array")
		}
	}
	if raw, ok := fields["image"]; ok {
		if len(images) > 0 {
			return nil, errors.New("specify image or image_urls, not both")
		}
		var single string
		if common.Unmarshal(raw, &single) == nil {
			images = []string{single}
		} else if common.Unmarshal(raw, &images) != nil {
			return nil, errors.New("image must be a URI or ordered URI array")
		}
	}
	if len(images) > 32 {
		return nil, errors.New("too many reference images")
	}
	content := []any{map[string]any{"type": "text", "text": prompt}}
	for _, image := range images {
		parsed, err := url.Parse(image)
		if err != nil || image == "" || (parsed.Scheme != "https" && parsed.Scheme != "http" && !strings.HasPrefix(image, "data:image/")) {
			return nil, errors.New("invalid reference image URI")
		}
		content = append(content, map[string]any{"type": "image_url", "image_url": map[string]string{"url": image}})
	}
	// Preserve original extra parameters and explicit zero values. Only reshape
	// the public Images envelope into Flow's supported non-streaming Chat shape.
	delete(fields, "prompt")
	delete(fields, "image_urls")
	delete(fields, "image")
	delete(fields, "response_format")
	delete(fields, "n")
	fields["model"], _ = common.Marshal(upstreamModel)
	fields["stream"] = json.RawMessage("false")
	fields["messages"], err = common.Marshal([]any{map[string]any{"role": "user", "content": content}})
	if err != nil {
		return nil, err
	}
	return common.Marshal(fields)
}

func ConvertFlowImageJobResponse(body []byte) ([]byte, error) {
	var response struct {
		Created int64 `json:"created"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if common.Unmarshal(body, &response) != nil || len(response.Choices) != 1 {
		return nil, errors.New("invalid Flow image response")
	}
	content := strings.TrimSpace(response.Choices[0].Message.Content)
	const prefix = "![Generated Image]("
	if !strings.HasPrefix(content, prefix) || !strings.HasSuffix(content, ")") {
		return nil, errors.New("Flow returned no generated image")
	}
	image := strings.TrimSuffix(strings.TrimPrefix(content, prefix), ")")
	data := dto.ImageData{}
	if strings.HasPrefix(image, "data:image/") {
		index := strings.Index(image, ";base64,")
		if index < 0 {
			return nil, errors.New("invalid Flow inline image")
		}
		data.B64Json = image[index+8:]
	} else {
		parsed, err := url.Parse(image)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return nil, errors.New("invalid Flow result URI")
		}
		data.Url = image
	}
	if response.Created <= 0 {
		response.Created = time.Now().Unix()
	}
	return common.Marshal(dto.ImageResponse{Created: response.Created, Data: []dto.ImageData{data}})
}

// FlowImageJobRejectionCode accepts only the complete provider-owned rejection
// message emitted by Flow's validated Google RPC error slot. Generic HTTP500,
// partial responses, request text and other provider reasons remain unclassified.
func FlowImageJobRejectionCode(err *types.NewAPIError) string {
	if err != nil && err.StatusCode == http.StatusInternalServerError &&
		err.GetErrorCode() == types.ErrorCodeBadResponseStatusCode &&
		err.Error() == "Google RPC rejection: PUBLIC_ERROR_HIGH_TRAFFIC" {
		return "flow_provider_high_traffic"
	}
	return ""
}
