package openai

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/ai360"
	"github.com/QuantumNous/new-api/relay/channel/lingyiwanwu"

	//"github.com/QuantumNous/new-api/relay/channel/minimax"
	"github.com/QuantumNous/new-api/relay/channel/openrouter"
	"github.com/QuantumNous/new-api/relay/channel/xinference"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/common_handler"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
)

type Adaptor struct {
	ChannelType    int
	ResponseFormat string
}

func isGrsaiImageCompat(info *relaycommon.RelayInfo) bool {
	if info == nil {
		return false
	}
	if info.RelayMode != relayconstant.RelayModeImagesGenerations &&
		info.RelayMode != relayconstant.RelayModeImagesEdits {
		return false
	}
	if !isGrsaiImageBaseURL(info) {
		return false
	}
	return isGrsaiNativeImageModel(info.UpstreamModelName) ||
		isGrsaiNativeImageModel(info.OriginModelName)
}

func isGrsaiImageBaseURL(info *relaycommon.RelayInfo) bool {
	if info == nil {
		return false
	}
	baseURL := strings.ToLower(strings.TrimSpace(info.ChannelBaseUrl))
	return strings.Contains(baseURL, "grsai.dakka.com.cn") ||
		strings.Contains(baseURL, "grsaiapi.com") ||
		strings.Contains(baseURL, "host.docker.internal:39001")
}

func shouldNormalizeResponsesRequestArguments(info *relaycommon.RelayInfo) bool {
	if info == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(info.ChannelOtherSettings.ResponsesArgumentsMode)) {
	case "object", "objects", "json_object", "json-object":
		return true
	case "string", "strings", "legacy":
		return false
	}
	baseURL := strings.ToLower(strings.TrimSpace(info.ChannelBaseUrl))
	if strings.Contains(baseURL, "cliproxyplus") {
		return true
	}
	if info.ChannelId == 1 && strings.TrimRight(baseURL, "/") == "http://cliproxy:8317" {
		return true
	}
	if strings.Contains(baseURL, "cliproxy") ||
		strings.Contains(baseURL, "codex2api.com") ||
		strings.Contains(baseURL, "127.0.0.1:8317") ||
		strings.Contains(baseURL, "localhost:8317") {
		return false
	}
	return true
}

// parseReasoningEffortFromModelSuffix 从模型名称中解析推理级别
// support OAI models: o1-mini/o3-mini/o4-mini/o1/o3 etc...
// minimal effort only available in gpt-5
func parseReasoningEffortFromModelSuffix(model string) (string, string) {
	effortSuffixes := []string{"-high", "-minimal", "-low", "-medium", "-none", "-xhigh"}
	for _, suffix := range effortSuffixes {
		if strings.HasSuffix(model, suffix) {
			effort := strings.TrimPrefix(suffix, "-")
			originModel := strings.TrimSuffix(model, suffix)
			return effort, originModel
		}
	}
	return "", model
}

func (a *Adaptor) ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	// 使用 service.GeminiToOpenAIRequest 转换请求格式
	openaiRequest, err := service.GeminiToOpenAIRequest(request, info)
	if err != nil {
		return nil, err
	}
	return a.ConvertOpenAIRequest(c, info, openaiRequest)
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	//if !strings.Contains(request.Model, "claude") {
	//	return nil, fmt.Errorf("you are using openai channel type with path /v1/messages, only claude model supported convert, but got %s", request.Model)
	//}
	//if common.DebugEnabled {
	//	bodyBytes := []byte(common.GetJsonString(request))
	//	err := os.WriteFile(fmt.Sprintf("claude_request_%s.txt", c.GetString(common.RequestIdKey)), bodyBytes, 0644)
	//	if err != nil {
	//		println(fmt.Sprintf("failed to save request body to file: %v", err))
	//	}
	//}
	aiRequest, err := service.ClaudeToOpenAIRequest(*request, info)
	if err != nil {
		return nil, err
	}
	//if common.DebugEnabled {
	//	println(fmt.Sprintf("convert claude to openai request result: %s", common.GetJsonString(aiRequest)))
	//	// Save request body to file for debugging
	//	bodyBytes := []byte(common.GetJsonString(aiRequest))
	//	err = os.WriteFile(fmt.Sprintf("claude_to_openai_request_%s.txt", c.GetString(common.RequestIdKey)), bodyBytes, 0644)
	//	if err != nil {
	//		println(fmt.Sprintf("failed to save request body to file: %v", err))
	//	}
	//}
	if info.SupportStreamOptions && info.IsStream {
		aiRequest.StreamOptions = &dto.StreamOptions{
			IncludeUsage: true,
		}
	}
	return a.ConvertOpenAIRequest(c, info, aiRequest)
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType

	// initialize ThinkingContentInfo when thinking_to_content is enabled
	if info.ChannelSetting.ThinkingToContent {
		info.ThinkingContentInfo = relaycommon.ThinkingContentInfo{
			IsFirstThinkingContent:  true,
			SendLastThinkingContent: false,
			HasSentThinkingContent:  false,
		}
	}
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if info.RelayMode == relayconstant.RelayModeRealtime {
		if strings.HasPrefix(info.ChannelBaseUrl, "https://") {
			baseUrl := strings.TrimPrefix(info.ChannelBaseUrl, "https://")
			baseUrl = "wss://" + baseUrl
			info.ChannelBaseUrl = baseUrl
		} else if strings.HasPrefix(info.ChannelBaseUrl, "http://") {
			baseUrl := strings.TrimPrefix(info.ChannelBaseUrl, "http://")
			baseUrl = "ws://" + baseUrl
			info.ChannelBaseUrl = baseUrl
		}
	}
	switch info.ChannelType {
	case constant.ChannelTypeAzure:
		apiVersion := info.ApiVersion
		if apiVersion == "" {
			apiVersion = constant.AzureDefaultAPIVersion
		}
		// https://learn.microsoft.com/en-us/azure/cognitive-services/openai/chatgpt-quickstart?pivots=rest-api&tabs=command-line#rest-api
		requestURL := strings.Split(info.RequestURLPath, "?")[0]
		requestURL = fmt.Sprintf("%s?api-version=%s", requestURL, apiVersion)
		task := strings.TrimPrefix(requestURL, "/v1/")

		if info.RelayFormat == types.RelayFormatClaude {
			task = strings.TrimPrefix(task, "messages")
			task = "chat/completions" + task
		}

		// 特殊处理 responses API（包含 compact）
		if info.RelayMode == relayconstant.RelayModeResponses || info.RelayMode == relayconstant.RelayModeResponsesCompact {
			responsesApiVersion := "preview"

			subUrl := "/openai/v1/responses"
			if strings.Contains(info.ChannelBaseUrl, "cognitiveservices.azure.com") {
				subUrl = "/openai/responses"
				responsesApiVersion = apiVersion
			}

			if info.ChannelOtherSettings.AzureResponsesVersion != "" {
				responsesApiVersion = info.ChannelOtherSettings.AzureResponsesVersion
			}

			// compact 模式追加 /compact
			if info.RelayMode == relayconstant.RelayModeResponsesCompact {
				subUrl = subUrl + "/compact"
			}

			requestURL = fmt.Sprintf("%s?api-version=%s", subUrl, responsesApiVersion)
			return relaycommon.GetFullRequestURL(info.ChannelBaseUrl, requestURL, info.ChannelType), nil
		}

		model_ := info.UpstreamModelName
		// 2025年5月10日后创建的渠道不移除.
		if info.ChannelCreateTime < constant.AzureNoRemoveDotTime {
			model_ = strings.Replace(model_, ".", "", -1)
		}
		// https://github.com/songquanpeng/one-api/issues/67
		requestURL = fmt.Sprintf("/openai/deployments/%s/%s", model_, task)
		if info.RelayMode == relayconstant.RelayModeRealtime {
			requestURL = fmt.Sprintf("/openai/realtime?deployment=%s&api-version=%s", model_, apiVersion)
		}
		return relaycommon.GetFullRequestURL(info.ChannelBaseUrl, requestURL, info.ChannelType), nil
	//case constant.ChannelTypeMiniMax:
	//	return minimax.GetRequestURL(info)
	case constant.ChannelTypeCustom:
		url := info.ChannelBaseUrl
		url = strings.Replace(url, "{model}", info.UpstreamModelName, -1)
		return url, nil
	default:
		if isGrsaiImageCompat(info) {
			return relaycommon.GetFullRequestURL(info.ChannelBaseUrl, "/v1/api/generate", info.ChannelType), nil
		}
		if (info.RelayFormat == types.RelayFormatClaude || info.RelayFormat == types.RelayFormatGemini) &&
			info.RelayMode != relayconstant.RelayModeResponses &&
			info.RelayMode != relayconstant.RelayModeResponsesCompact {
			return fmt.Sprintf("%s/v1/chat/completions", info.ChannelBaseUrl), nil
		}
		return relaycommon.GetFullRequestURL(info.ChannelBaseUrl, info.RequestURLPath, info.ChannelType), nil
	}
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, header *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, header)
	if info.ChannelType == constant.ChannelTypeAzure {
		header.Set("api-key", info.ApiKey)
		return nil
	}
	if info.ChannelType == constant.ChannelTypeOpenAI && "" != info.Organization {
		header.Set("OpenAI-Organization", info.Organization)
	}
	// 检查 Header Override 是否已设置 Authorization，如果已设置则跳过默认设置
	// 这样可以避免在 Header Override 应用时被覆盖（虽然 Header Override 会在之后应用，但这里作为额外保护）
	hasAuthOverride := false
	if len(info.HeadersOverride) > 0 {
		for k := range info.HeadersOverride {
			if strings.EqualFold(k, "Authorization") {
				hasAuthOverride = true
				break
			}
		}
	}
	if info.RelayMode == relayconstant.RelayModeRealtime {
		swp := c.Request.Header.Get("Sec-WebSocket-Protocol")
		if swp != "" {
			items := []string{
				"realtime",
				"openai-insecure-api-key." + info.ApiKey,
				"openai-beta.realtime-v1",
			}
			header.Set("Sec-WebSocket-Protocol", strings.Join(items, ","))
			//req.Header.Set("Sec-WebSocket-Key", c.Request.Header.Get("Sec-WebSocket-Key"))
			//req.Header.Set("Sec-Websocket-Extensions", c.Request.Header.Get("Sec-Websocket-Extensions"))
			//req.Header.Set("Sec-Websocket-Version", c.Request.Header.Get("Sec-Websocket-Version"))
		} else {
			header.Set("openai-beta", "realtime=v1")
			if !hasAuthOverride {
				header.Set("Authorization", "Bearer "+info.ApiKey)
			}
		}
	} else {
		if !hasAuthOverride {
			header.Set("Authorization", "Bearer "+info.ApiKey)
		}
	}
	if info.ChannelType == constant.ChannelTypeOpenRouter {
		if header.Get("HTTP-Referer") == "" {
			header.Set("HTTP-Referer", "https://www.newapi.ai")
		}
		if header.Get("X-OpenRouter-Title") == "" {
			header.Set("X-OpenRouter-Title", "New API")
		}
	}
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	if info.ChannelType != constant.ChannelTypeOpenAI && info.ChannelType != constant.ChannelTypeAzure {
		request.StreamOptions = nil
	}
	if info.ChannelType == constant.ChannelTypeOpenRouter {
		if len(request.Usage) == 0 {
			request.Usage = json.RawMessage(`{"include":true}`)
		}
		// 适配 OpenRouter 的 thinking 后缀
		if !model_setting.ShouldPreserveThinkingSuffix(info.OriginModelName) &&
			strings.HasSuffix(info.UpstreamModelName, "-thinking") {
			info.UpstreamModelName = strings.TrimSuffix(info.UpstreamModelName, "-thinking")
			request.Model = info.UpstreamModelName
			if len(request.Reasoning) == 0 {
				reasoning := map[string]any{
					"enabled": true,
				}
				if request.ReasoningEffort != "" && request.ReasoningEffort != "none" {
					reasoning["effort"] = request.ReasoningEffort
				}
				marshal, err := common.Marshal(reasoning)
				if err != nil {
					return nil, fmt.Errorf("error marshalling reasoning: %w", err)
				}
				request.Reasoning = marshal
			}
			// 清空多余的ReasoningEffort
			request.ReasoningEffort = ""
		} else {
			if len(request.Reasoning) == 0 {
				// 适配 OpenAI 的 ReasoningEffort 格式
				if request.ReasoningEffort != "" {
					reasoning := map[string]any{
						"enabled": true,
					}
					if request.ReasoningEffort != "none" {
						reasoning["effort"] = request.ReasoningEffort
						marshal, err := common.Marshal(reasoning)
						if err != nil {
							return nil, fmt.Errorf("error marshalling reasoning: %w", err)
						}
						request.Reasoning = marshal
					}
				}
			}
			request.ReasoningEffort = ""
		}

		// https://docs.anthropic.com/en/api/openai-sdk#extended-thinking-support
		// 没有做排除3.5Haiku等，要出问题再加吧，最佳兼容性（不是
		if request.THINKING != nil && strings.HasPrefix(info.UpstreamModelName, "anthropic") {
			var thinking dto.Thinking // Claude标准Thinking格式
			if err := json.Unmarshal(request.THINKING, &thinking); err != nil {
				return nil, fmt.Errorf("error Unmarshal thinking: %w", err)
			}

			// 只有当 thinking.Type 是 "enabled" 时才处理
			if thinking.Type == "enabled" {
				// 检查 BudgetTokens 是否为 nil
				if thinking.BudgetTokens == nil {
					return nil, fmt.Errorf("BudgetTokens is nil when thinking is enabled")
				}

				reasoning := openrouter.RequestReasoning{
					Enabled:   true,
					MaxTokens: *thinking.BudgetTokens,
				}

				marshal, err := common.Marshal(reasoning)
				if err != nil {
					return nil, fmt.Errorf("error marshalling reasoning: %w", err)
				}

				request.Reasoning = marshal
			}

			// 清空 THINKING
			request.THINKING = nil
		}

	}
	if strings.HasPrefix(info.UpstreamModelName, "o") || strings.HasPrefix(info.UpstreamModelName, "gpt-5") {
		if lo.FromPtrOr(request.MaxCompletionTokens, uint(0)) == 0 && lo.FromPtrOr(request.MaxTokens, uint(0)) != 0 {
			request.MaxCompletionTokens = request.MaxTokens
			request.MaxTokens = nil
		}

		if strings.HasPrefix(info.UpstreamModelName, "o") {
			request.Temperature = nil
		}

		// gpt-5系列模型适配 归零不再支持的参数
		if strings.HasPrefix(info.UpstreamModelName, "gpt-5") {
			request.Temperature = nil
			request.TopP = nil
			request.LogProbs = nil
		}

		// 转换模型推理力度后缀
		effort, originModel := parseReasoningEffortFromModelSuffix(info.UpstreamModelName)
		if effort != "" {
			request.ReasoningEffort = effort
			info.UpstreamModelName = originModel
			request.Model = originModel
		}

		info.ReasoningEffort = request.ReasoningEffort

		// o系列模型developer适配（o1-mini除外）
		if !strings.HasPrefix(info.UpstreamModelName, "o1-mini") && !strings.HasPrefix(info.UpstreamModelName, "o1-preview") {
			//修改第一个Message的内容，将system改为developer
			if len(request.Messages) > 0 && request.Messages[0].Role == "system" {
				request.Messages[0].Role = "developer"
			}
		}
	}

	return request, nil
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return request, nil
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	return request, nil
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	a.ResponseFormat = request.ResponseFormat
	if info.RelayMode == relayconstant.RelayModeAudioSpeech {
		jsonData, err := common.Marshal(request)
		if err != nil {
			return nil, fmt.Errorf("error marshalling object: %w", err)
		}
		return bytes.NewReader(jsonData), nil
	} else {
		var requestBody bytes.Buffer
		writer := multipart.NewWriter(&requestBody)

		writer.WriteField("model", request.Model)

		formData, err2 := common.ParseMultipartFormReusable(c)
		if err2 != nil {
			return nil, fmt.Errorf("error parsing multipart form: %w", err2)
		}

		// 打印类似 curl 命令格式的信息
		logger.LogDebug(c.Request.Context(), fmt.Sprintf("--form 'model=\"%s\"'", request.Model))

		// 遍历表单字段并打印输出
		for key, values := range formData.Value {
			if key == "model" {
				continue
			}
			for _, value := range values {
				writer.WriteField(key, value)
				logger.LogDebug(c.Request.Context(), fmt.Sprintf("--form '%s=\"%s\"'", key, value))
			}
		}

		// 从 formData 中获取文件
		fileHeaders := formData.File["file"]
		if len(fileHeaders) == 0 {
			return nil, errors.New("file is required")
		}

		// 使用 formData 中的第一个文件
		fileHeader := fileHeaders[0]
		logger.LogDebug(c.Request.Context(), fmt.Sprintf("--form 'file=@\"%s\"' (size: %d bytes, content-type: %s)",
			fileHeader.Filename, fileHeader.Size, fileHeader.Header.Get("Content-Type")))

		file, err := fileHeader.Open()
		if err != nil {
			return nil, fmt.Errorf("error opening audio file: %v", err)
		}
		defer file.Close()

		part, err := writer.CreateFormFile("file", fileHeader.Filename)
		if err != nil {
			return nil, errors.New("create form file failed")
		}
		if _, err := io.Copy(part, file); err != nil {
			return nil, errors.New("copy file failed")
		}

		// 关闭 multipart 编写器以设置分界线
		writer.Close()
		c.Request.Header.Set("Content-Type", writer.FormDataContentType())
		logger.LogDebug(c.Request.Context(), fmt.Sprintf("--header 'Content-Type: %s'", writer.FormDataContentType()))
		return &requestBody, nil
	}
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	switch info.RelayMode {
	case relayconstant.RelayModeImagesGenerations:
		if isGrsaiImageBaseURL(info) && isGrsaiNativeImageModel(request.Model) {
			return convertGrsaiImageGenerateRequest(request), nil
		}
		return request, nil
	case relayconstant.RelayModeImagesEdits:
		if isGrsaiImageCompat(info) {
			return convertGrsaiImageEditRequest(c, request)
		}

		var requestBody bytes.Buffer
		writer := multipart.NewWriter(&requestBody)

		writer.WriteField("model", request.Model)
		// 使用已解析的 multipart 表单，避免重复解析
		mf := c.Request.MultipartForm
		if mf == nil {
			if _, err := c.MultipartForm(); err != nil {
				return nil, errors.New("failed to parse multipart form")
			}
			mf = c.Request.MultipartForm
		}

		// 写入所有非文件字段
		if mf != nil {
			for key, values := range mf.Value {
				if key == "model" {
					continue
				}
				for _, value := range values {
					writer.WriteField(key, value)
				}
			}
		}

		if mf != nil && mf.File != nil {
			// Check if "image" field exists in any form, including array notation
			var imageFiles []*multipart.FileHeader
			var exists bool

			// First check for standard "image" field
			if imageFiles, exists = mf.File["image"]; !exists || len(imageFiles) == 0 {
				// If not found, check for "image[]" field
				if imageFiles, exists = mf.File["image[]"]; !exists || len(imageFiles) == 0 {
					// If still not found, iterate through all fields to find any that start with "image["
					foundArrayImages := false
					for fieldName, files := range mf.File {
						if strings.HasPrefix(fieldName, "image[") && len(files) > 0 {
							foundArrayImages = true
							imageFiles = append(imageFiles, files...)
						}
					}

					// If no image fields found at all
					if !foundArrayImages && (len(imageFiles) == 0) {
						return nil, errors.New("image is required")
					}
				}
			}

			// Process all image files
			for i, fileHeader := range imageFiles {
				file, err := fileHeader.Open()
				if err != nil {
					return nil, fmt.Errorf("failed to open image file %d: %w", i, err)
				}

				// If multiple images, use image[] as the field name
				fieldName := "image"
				if len(imageFiles) > 1 {
					fieldName = "image[]"
				}

				// Determine MIME type based on file extension
				mimeType := detectImageMimeType(fileHeader.Filename)

				// Create a form file with the appropriate content type
				h := make(textproto.MIMEHeader)
				h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, fieldName, fileHeader.Filename))
				h.Set("Content-Type", mimeType)

				part, err := writer.CreatePart(h)
				if err != nil {
					return nil, fmt.Errorf("create form part failed for image %d: %w", i, err)
				}

				if _, err := io.Copy(part, file); err != nil {
					return nil, fmt.Errorf("copy file failed for image %d: %w", i, err)
				}

				// 复制完立即关闭，避免在循环内使用 defer 占用资源
				_ = file.Close()
			}

			// Handle mask file if present
			if maskFiles, exists := mf.File["mask"]; exists && len(maskFiles) > 0 {
				maskFile, err := maskFiles[0].Open()
				if err != nil {
					return nil, errors.New("failed to open mask file")
				}
				// 复制完立即关闭，避免在循环内使用 defer 占用资源

				// Determine MIME type for mask file
				mimeType := detectImageMimeType(maskFiles[0].Filename)

				// Create a form file with the appropriate content type
				h := make(textproto.MIMEHeader)
				h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="mask"; filename="%s"`, maskFiles[0].Filename))
				h.Set("Content-Type", mimeType)

				maskPart, err := writer.CreatePart(h)
				if err != nil {
					return nil, errors.New("create form file failed for mask")
				}

				if _, err := io.Copy(maskPart, maskFile); err != nil {
					return nil, errors.New("copy mask file failed")
				}
				_ = maskFile.Close()
			}
		} else {
			return nil, errors.New("no multipart form data found")
		}

		// 关闭 multipart 编写器以设置分界线
		writer.Close()
		c.Request.Header.Set("Content-Type", writer.FormDataContentType())
		return &requestBody, nil

	default:
		return request, nil
	}
}

func convertGrsaiImageGenerateRequest(request dto.ImageRequest) map[string]any {
	body := make(map[string]any)
	body["model"] = request.Model
	body["prompt"] = request.Prompt
	body["replyType"] = "json"
	addGrsaiImageShape(body, request)
	addGrsaiReferenceImages(body, request)
	return body
}

func convertGrsaiImageEditRequest(c *gin.Context, request dto.ImageRequest) (map[string]any, error) {
	mf := c.Request.MultipartForm
	if mf == nil {
		if _, err := c.MultipartForm(); err != nil {
			return nil, errors.New("failed to parse multipart form")
		}
		mf = c.Request.MultipartForm
	}
	if mf == nil {
		return nil, errors.New("no multipart form data found")
	}

	body := make(map[string]any)
	body["model"] = request.Model
	body["prompt"] = request.Prompt
	body["replyType"] = "json"
	addGrsaiImageShape(body, request)

	for key, values := range mf.Value {
		if key == "" || key == "model" || key == "prompt" || key == "n" ||
			key == "size" || key == "quality" || key == "response_format" || len(values) == 0 {
			continue
		}
		if len(values) == 1 {
			body[key] = values[0]
		} else {
			body[key] = values
		}
	}

	imageFiles := collectMultipartImageFiles(mf)
	if len(imageFiles) == 0 {
		return nil, errors.New("image is required")
	}
	images := make([]string, 0, len(imageFiles))
	for i, fileHeader := range imageFiles {
		file, err := fileHeader.Open()
		if err != nil {
			return nil, fmt.Errorf("failed to open image file %d: %w", i, err)
		}
		data, err := io.ReadAll(file)
		_ = file.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read image file %d: %w", i, err)
		}
		images = append(images, base64.StdEncoding.EncodeToString(data))
	}
	body["images"] = images

	c.Request.Header.Set("Content-Type", "application/json")
	return body, nil
}

func addGrsaiImageShape(body map[string]any, request dto.ImageRequest) {
	if body == nil {
		return
	}
	if isGrsaiNanoBananaImageModel(request.Model) {
		body["aspectRatio"] = grsaiAspectRatioFromImageSize("", request.Size, request.Quality)
		body["imageSize"] = grsaiImageSizeTierFromImageSize(request.Size, request.Quality)
		return
	}
	body["aspectRatio"] = grsaiAspectRatioFromImageSize(request.Model, request.Size, request.Quality)
}

func addGrsaiReferenceImages(body map[string]any, request dto.ImageRequest) {
	if body == nil {
		return
	}
	if raw, ok := request.Extra["images"]; ok && len(raw) > 0 {
		var images any
		if err := common.Unmarshal(raw, &images); err == nil {
			body["images"] = images
			return
		}
	}
	if len(request.Image) == 0 {
		return
	}
	var image any
	if err := common.Unmarshal(request.Image, &image); err != nil {
		return
	}
	switch value := image.(type) {
	case []any:
		body["images"] = value
	case string:
		if strings.TrimSpace(value) != "" {
			body["images"] = []string{value}
		}
	default:
		body["images"] = value
	}
}

func grsaiAspectRatioFromImageSize(model, size, quality string) string {
	size = strings.TrimSpace(size)
	isVIP := isGrsaiVIPImageModel(model)
	if size == "" {
		if isVIP {
			return "1024x1024"
		}
		return "1:1"
	}

	if isKSize(size) {
		ratio := "1:1"
		tier := grsaiResolutionTier(size, quality)
		if isVIP {
			return grsaiVIPSizeForRatio(ratio, tier)
		}
		return ratio
	}

	if strings.Contains(size, ":") && !strings.ContainsAny(size, "xX×") {
		ratio := normalizeGrsaiRatio(size)
		if isVIP {
			return grsaiVIPSizeForRatio(ratio, grsaiResolutionTier(size, quality))
		}
		return ratio
	}

	normalized := strings.ReplaceAll(size, "×", "x")
	normalized = strings.ReplaceAll(normalized, "X", "x")
	parts := strings.Split(normalized, "x")
	if len(parts) != 2 {
		if isVIP {
			return "1024x1024"
		}
		return "1:1"
	}

	left := strings.TrimSpace(parts[0])
	right := strings.TrimSpace(parts[1])
	width, widthErr := strconv.Atoi(left)
	height, heightErr := strconv.Atoi(right)
	if widthErr == nil && heightErr == nil && width > 0 && height > 0 {
		if isVIP {
			if isValidGrsaiVIPPixelSize(width, height) {
				return fmt.Sprintf("%dx%d", width, height)
			}
			return grsaiVIPSizeForRatio(grsaiKnownRatio(width, height), grsaiResolutionTierForPixels(width, height, quality))
		}
		if isGrsaiStandard1KPixelSize(width, height) {
			return fmt.Sprintf("%dx%d", width, height)
		}
		return grsaiKnownRatio(width, height)
	}

	switch {
	case left == right:
		return "1:1"
	case left == "1024" && right == "1536", left == "768" && right == "1152":
		return "2:3"
	case left == "1536" && right == "1024", left == "1152" && right == "768":
		return "3:2"
	case left == "1024" && right == "1792":
		return "9:16"
	case left == "1792" && right == "1024":
		return "16:9"
	default:
		return fmt.Sprintf("%s:%s", left, right)
	}
}

func isGrsaiVIPImageModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	if !isGrsaiGPTImageModel(model) {
		return false
	}
	return strings.Contains(model, "vip") ||
		strings.Contains(model, "pro") ||
		strings.Contains(model, "max")
}

func isGrsaiNativeImageModel(model string) bool {
	return isGrsaiGPTImageModel(model) || isGrsaiNanoBananaImageModel(model)
}

func isGrsaiGPTImageModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	switch model {
	case "image-2", "gpt-image-2", "gpt-image-2-pro", "gpt-image-2-vip", "gpt-image-2-max":
		return true
	default:
		return false
	}
}

func isGrsaiNanoBananaImageModel(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(model, "nano-banana") ||
		strings.HasPrefix(model, "gemini-3.1-flash-image-preview") ||
		strings.HasPrefix(model, "gemini-3-pro-image-preview")
}

func isKSize(size string) bool {
	size = strings.ToLower(strings.TrimSpace(size))
	return size == "1k" || size == "2k" || size == "4k"
}

func grsaiImageSizeTierFromImageSize(size, quality string) string {
	switch grsaiResolutionTierFromImageSize(size, quality) {
	case 2:
		return "4K"
	case 1:
		return "2K"
	default:
		return "1K"
	}
}

func grsaiResolutionTierFromImageSize(size, quality string) int {
	if tier := grsaiResolutionTier(size, quality); tier > 0 {
		return tier
	}
	normalized := strings.ReplaceAll(size, "×", "x")
	normalized = strings.ReplaceAll(normalized, "X", "x")
	parts := strings.Split(normalized, "x")
	if len(parts) != 2 {
		return 0
	}
	width, widthErr := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, heightErr := strconv.Atoi(strings.TrimSpace(parts[1]))
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
		return 0
	}
	return grsaiResolutionTierForPixels(width, height, quality)
}

func grsaiResolutionTier(size, quality string) int {
	value := strings.ToLower(strings.TrimSpace(size + " " + quality))
	switch {
	case strings.Contains(value, "4k"):
		return 2
	case strings.Contains(value, "2k"):
		return 1
	default:
		return 0
	}
}

func grsaiResolutionTierForPixels(width, height int, quality string) int {
	if tier := grsaiResolutionTier("", quality); tier > 0 {
		return tier
	}
	maxSide := width
	if height > maxSide {
		maxSide = height
	}
	pixels := width * height
	switch {
	case maxSide > 3840 || pixels > 4200000:
		return 2
	case maxSide >= 1800 || pixels >= 2000000:
		return 1
	default:
		return 0
	}
}

func isValidGrsaiVIPPixelSize(width, height int) bool {
	if width <= 0 || height <= 0 || width%16 != 0 || height%16 != 0 {
		return false
	}
	if width > 3840 || height > 3840 {
		return false
	}
	longSide, shortSide := width, height
	if height > width {
		longSide, shortSide = height, width
	}
	if longSide > shortSide*3 {
		return false
	}
	pixels := width * height
	return pixels >= 655360 && pixels <= 8294400
}

func isGrsaiStandard1KPixelSize(width, height int) bool {
	switch fmt.Sprintf("%dx%d", width, height) {
	case "1024x1024", "1672x941", "941x1672", "1443x1090", "1090x1443",
		"1536x1024", "1024x1536", "1408x1120", "1120x1408",
		"1920x832", "832x1920", "896x1792", "1792x896":
		return true
	default:
		return false
	}
}

func grsaiKnownRatio(width, height int) string {
	if width <= 0 || height <= 0 {
		return "1:1"
	}
	g := gcd(width, height)
	ratio := fmt.Sprintf("%d:%d", width/g, height/g)
	return normalizeGrsaiRatio(ratio)
}

func normalizeGrsaiRatio(ratio string) string {
	ratio = strings.TrimSpace(ratio)
	ratio = strings.ReplaceAll(ratio, " ", "")
	switch ratio {
	case "8:7", "7:8":
		return "1:1"
	default:
		return ratio
	}
}

func grsaiVIPSizeForRatio(ratio string, tier int) string {
	if tier < 0 {
		tier = 0
	}
	if tier > 2 {
		tier = 2
	}
	sizes := map[string][3]string{
		"1:1":  {"1024x1024", "2048x2048", "2880x2880"},
		"16:9": {"1280x720", "2048x1152", "3840x2160"},
		"9:16": {"720x1280", "1152x2048", "2160x3840"},
		"4:3":  {"1152x864", "2304x1728", "3264x2448"},
		"3:4":  {"864x1152", "1728x2304", "2448x3264"},
		"3:2":  {"1536x1024", "2048x1360", "3504x2336"},
		"2:3":  {"1024x1536", "1360x2048", "2336x3504"},
		"5:4":  {"1120x896", "2240x1792", "3200x2560"},
		"4:5":  {"896x1120", "1792x2240", "2560x3200"},
		"21:9": {"1456x624", "2912x1248", "3840x1648"},
		"9:21": {"624x1456", "1248x2912", "1648x3840"},
		"1:3":  {"688x2048", "688x2048", "1280x3840"},
		"3:1":  {"2048x688", "2048x688", "3840x1280"},
		"2:1":  {"1536x768", "3072x1536", "3840x1920"},
		"1:2":  {"768x1536", "1536x3072", "1920x3840"},
	}
	if values, ok := sizes[normalizeGrsaiRatio(ratio)]; ok {
		return values[tier]
	}
	return sizes["1:1"][tier]
}

func gcd(a, b int) int {
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}
	for b != 0 {
		a, b = b, a%b
	}
	if a == 0 {
		return 1
	}
	return a
}

func collectMultipartImageFiles(mf *multipart.Form) []*multipart.FileHeader {
	if mf == nil || mf.File == nil {
		return nil
	}
	if imageFiles := mf.File["image"]; len(imageFiles) > 0 {
		return imageFiles
	}
	if imageFiles := mf.File["image[]"]; len(imageFiles) > 0 {
		return imageFiles
	}
	var imageFiles []*multipart.FileHeader
	for fieldName, files := range mf.File {
		if strings.HasPrefix(fieldName, "image[") && len(files) > 0 {
			imageFiles = append(imageFiles, files...)
		}
	}
	return imageFiles
}

// detectImageMimeType determines the MIME type based on the file extension
func detectImageMimeType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		// Try to detect from extension if possible
		if strings.HasPrefix(ext, ".jp") {
			return "image/jpeg"
		}
		// Default to png as a fallback
		return "image/png"
	}
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	//  转换模型推理力度后缀
	effort, originModel := parseReasoningEffortFromModelSuffix(request.Model)
	if effort != "" {
		if request.Reasoning == nil {
			request.Reasoning = &dto.Reasoning{
				Effort: effort,
			}
		} else {
			request.Reasoning.Effort = effort
		}
		request.Model = originModel
	}
	if info != nil && request.Reasoning != nil && request.Reasoning.Effort != "" {
		info.ReasoningEffort = request.Reasoning.Effort
	}
	if shouldNormalizeResponsesRequestArguments(info) {
		request.Input = dto.NormalizeResponsesRequestInputArguments(request.Input)
	}
	return request, nil
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	if info.RelayMode == relayconstant.RelayModeAudioTranscription ||
		info.RelayMode == relayconstant.RelayModeAudioTranslation ||
		info.RelayMode == relayconstant.RelayModeImagesEdits {
		return channel.DoFormRequest(a, c, info, requestBody)
	} else if info.RelayMode == relayconstant.RelayModeRealtime {
		return channel.DoWssRequest(a, c, info, requestBody)
	} else {
		return channel.DoApiRequest(a, c, info, requestBody)
	}
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	switch info.RelayMode {
	case relayconstant.RelayModeRealtime:
		err, usage = OpenaiRealtimeHandler(c, info)
	case relayconstant.RelayModeAudioSpeech:
		usage = OpenaiTTSHandler(c, resp, info)
	case relayconstant.RelayModeAudioTranslation:
		fallthrough
	case relayconstant.RelayModeAudioTranscription:
		err, usage = OpenaiSTTHandler(c, resp, info, a.ResponseFormat)
	case relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits:
		if isGrsaiImageCompat(info) {
			usage, err = GrsaiImageHandler(c, info, resp)
		} else {
			usage, err = OpenaiHandlerWithUsage(c, info, resp)
		}
	case relayconstant.RelayModeRerank:
		usage, err = common_handler.RerankHandler(c, info, resp)
	case relayconstant.RelayModeResponses:
		if info.IsStream {
			usage, err = OaiResponsesStreamHandler(c, info, resp)
		} else {
			usage, err = OaiResponsesHandler(c, info, resp)
		}
	case relayconstant.RelayModeResponsesCompact:
		usage, err = OaiResponsesCompactionHandler(c, resp)
	default:
		if info.IsStream {
			usage, err = OaiStreamHandler(c, info, resp)
		} else {
			usage, err = OpenaiHandler(c, info, resp)
		}
	}
	return
}

func (a *Adaptor) GetModelList() []string {
	switch a.ChannelType {
	case constant.ChannelType360:
		return ai360.ModelList
	case constant.ChannelTypeLingYiWanWu:
		return lingyiwanwu.ModelList
	//case constant.ChannelTypeMiniMax:
	//	return minimax.ModelList
	case constant.ChannelTypeXinference:
		return xinference.ModelList
	case constant.ChannelTypeOpenRouter:
		return openrouter.ModelList
	default:
		return ModelList
	}
}

func (a *Adaptor) GetChannelName() string {
	switch a.ChannelType {
	case constant.ChannelType360:
		return ai360.ChannelName
	case constant.ChannelTypeLingYiWanWu:
		return lingyiwanwu.ChannelName
	//case constant.ChannelTypeMiniMax:
	//	return minimax.ChannelName
	case constant.ChannelTypeXinference:
		return xinference.ChannelName
	case constant.ChannelTypeOpenRouter:
		return openrouter.ChannelName
	default:
		return ChannelName
	}
}
