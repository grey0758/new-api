package dto

import (
	"encoding/json"
	"reflect"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type ImageRequest struct {
	Model             string          `json:"model"`
	Prompt            string          `json:"prompt" binding:"required"`
	N                 *uint           `json:"n,omitempty"`
	Size              string          `json:"size,omitempty"`
	Quality           string          `json:"quality,omitempty"`
	ResponseFormat    string          `json:"response_format,omitempty"`
	Style             json.RawMessage `json:"style,omitempty"`
	User              json.RawMessage `json:"user,omitempty"`
	ExtraFields       json.RawMessage `json:"extra_fields,omitempty"`
	Background        json.RawMessage `json:"background,omitempty"`
	Moderation        json.RawMessage `json:"moderation,omitempty"`
	OutputFormat      json.RawMessage `json:"output_format,omitempty"`
	OutputCompression json.RawMessage `json:"output_compression,omitempty"`
	PartialImages     json.RawMessage `json:"partial_images,omitempty"`
	// Stream            bool            `json:"stream,omitempty"`
	Watermark *bool `json:"watermark,omitempty"`
	// zhipu 4v
	WatermarkEnabled json.RawMessage `json:"watermark_enabled,omitempty"`
	UserId           json.RawMessage `json:"user_id,omitempty"`
	Image            json.RawMessage `json:"image,omitempty"`
	// 用匿名参数接收额外参数
	Extra map[string]json.RawMessage `json:"-"`
}

func (i *ImageRequest) UnmarshalJSON(data []byte) error {
	// 先解析成 map[string]interface{}
	var rawMap map[string]json.RawMessage
	if err := common.Unmarshal(data, &rawMap); err != nil {
		return err
	}

	// 用 struct tag 获取所有已定义字段名
	knownFields := GetJSONFieldNames(reflect.TypeOf(*i))

	// 再正常解析已定义字段
	type Alias ImageRequest
	var known Alias
	if err := common.Unmarshal(data, &known); err != nil {
		return err
	}
	*i = ImageRequest(known)

	// 提取多余字段
	i.Extra = make(map[string]json.RawMessage)
	for k, v := range rawMap {
		if _, ok := knownFields[k]; !ok {
			i.Extra[k] = v
		}
	}
	return nil
}

// 序列化时需要重新把字段平铺
func (r ImageRequest) MarshalJSON() ([]byte, error) {
	// 将已定义字段转为 map
	type Alias ImageRequest
	alias := Alias(r)
	base, err := common.Marshal(alias)
	if err != nil {
		return nil, err
	}

	var baseMap map[string]json.RawMessage
	if err := common.Unmarshal(base, &baseMap); err != nil {
		return nil, err
	}

	// 不能合并ExtraFields！！！！！！！！
	// 合并 ExtraFields
	//for k, v := range r.Extra {
	//	if _, exists := baseMap[k]; !exists {
	//		baseMap[k] = v
	//	}
	//}

	return common.Marshal(baseMap)
}

func GetJSONFieldNames(t reflect.Type) map[string]struct{} {
	fields := make(map[string]struct{})
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// 跳过匿名字段（例如 ExtraFields）
		if field.Anonymous {
			continue
		}

		tag := field.Tag.Get("json")
		if tag == "-" || tag == "" {
			continue
		}

		// 取逗号前字段名（排除 omitempty 等）
		name := tag
		if commaIdx := indexComma(tag); commaIdx != -1 {
			name = tag[:commaIdx]
		}
		fields[name] = struct{}{}
	}
	return fields
}

func indexComma(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			return i
		}
	}
	return -1
}

const (
	gemini31FlashImagePrice1K = 0.067
	gemini31FlashImagePrice2K = 0.101
	gemini31FlashImagePrice4K = 0.151
	gemini31FlashImagePrice05 = 0.045

	gemini3ProImagePrice1K = 0.134
	gemini3ProImagePrice4K = 0.24
)

func normalizeGeminiImageSize(size string) string {
	size = strings.ToUpper(strings.TrimSpace(size))
	switch size {
	case "0.5K", "512", "512X512":
		return "0.5K"
	case "1K", "1024", "1024X1024", "":
		return "1K"
	case "2K", "2048", "2048X2048":
		return "2K"
	case "4K", "4096", "4096X4096":
		return "4K"
	default:
		return size
	}
}

func (i *ImageRequest) getExtraBodyGeminiImageSize() string {
	raw, ok := i.Extra["extra_body"]
	if !ok || len(raw) == 0 {
		return ""
	}

	var extraBody map[string]any
	if err := common.Unmarshal(raw, &extraBody); err != nil {
		return ""
	}
	googleBody, ok := extraBody["google"].(map[string]any)
	if !ok {
		return ""
	}
	imageConfig, ok := googleBody["image_config"].(map[string]any)
	if !ok {
		return ""
	}
	imageSize, ok := imageConfig["image_size"].(string)
	if !ok {
		return ""
	}
	return normalizeGeminiImageSize(imageSize)
}

func inferGeminiImageSizeFromAlias(modelName string) string {
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	switch {
	case strings.Contains(modelName, "-small-"):
		return "0.5K"
	case strings.Contains(modelName, "-pro-large-"):
		return "4K"
	case strings.Contains(modelName, "-2-2k-"), strings.Contains(modelName, "-pro-2k-"):
		return "2K"
	}
	return ""
}

func inferGeminiImageSizeFromQuality(quality string) string {
	switch strings.ToLower(strings.TrimSpace(quality)) {
	case "0.5k", "512", "512x512":
		return "0.5K"
	case "4k", "4096", "4096x4096":
		return "4K"
	case "hd", "high", "2k":
		return "2K"
	case "standard", "medium", "low", "auto", "1k", "":
		return "1K"
	default:
		return ""
	}
}

func resolveGeminiImageSize(i *ImageRequest) string {
	if size := i.getExtraBodyGeminiImageSize(); size != "" {
		return size
	}
	if size := inferGeminiImageSizeFromAlias(i.Model); size != "" {
		return size
	}
	if size := inferGeminiImageSizeFromQuality(i.Quality); size != "" {
		return size
	}
	return "1K"
}

func isGemini31FlashImageFamily(modelName string) bool {
	modelName = strings.ToLower(modelName)
	if strings.HasPrefix(modelName, "gemini-3.1-flash-image-preview") {
		return true
	}
	return strings.Contains(modelName, "nano-banana-") && !strings.Contains(modelName, "-pro")
}

func isGemini3ProImageFamily(modelName string) bool {
	modelName = strings.ToLower(modelName)
	if strings.HasPrefix(modelName, "gemini-3-pro-image-preview") {
		return true
	}
	return strings.Contains(modelName, "nano-banana-") && strings.Contains(modelName, "-pro")
}

func resolveGeminiImagePriceRatio(modelName string, imageSize string) float64 {
	imageSize = normalizeGeminiImageSize(imageSize)

	switch {
	case isGemini31FlashImageFamily(modelName):
		switch imageSize {
		case "0.5K":
			return gemini31FlashImagePrice05 / gemini31FlashImagePrice1K
		case "2K":
			return gemini31FlashImagePrice2K / gemini31FlashImagePrice1K
		case "4K":
			return gemini31FlashImagePrice4K / gemini31FlashImagePrice1K
		default:
			return 1
		}
	case isGemini3ProImageFamily(modelName):
		if imageSize == "4K" {
			return gemini3ProImagePrice4K / gemini3ProImagePrice1K
		}
		return 1
	default:
		return 1
	}
}

func (i *ImageRequest) GetTokenCountMeta() *types.TokenCountMeta {
	var sizeRatio = 1.0
	var qualityRatio = 1.0

	if strings.HasPrefix(i.Model, "dall-e") {
		// Size
		if i.Size == "256x256" {
			sizeRatio = 0.4
		} else if i.Size == "512x512" {
			sizeRatio = 0.45
		} else if i.Size == "1024x1024" {
			sizeRatio = 1
		} else if i.Size == "1024x1792" || i.Size == "1792x1024" {
			sizeRatio = 2
		}

		if i.Model == "dall-e-3" && i.Quality == "hd" {
			qualityRatio = 2.0
			if i.Size == "1024x1792" || i.Size == "1792x1024" {
				qualityRatio = 1.5
			}
		}
	}

	if strings.HasPrefix(i.Model, "gemini-") || strings.Contains(i.Model, "nano-banana-") {
		sizeRatio = resolveGeminiImagePriceRatio(i.Model, resolveGeminiImageSize(i))
		qualityRatio = 1
	}

	// n is NOT included here; it is handled via OtherRatio("n") in
	// image_handler.go (default) or channel adaptors (actual count).
	// Including n here caused double-counting for channels that also
	// set OtherRatio("n") (e.g. Ali/Bailian).
	return &types.TokenCountMeta{
		CombineText:     i.Prompt,
		MaxTokens:       1584,
		ImagePriceRatio: sizeRatio * qualityRatio,
	}
}

func (i *ImageRequest) IsStream(c *gin.Context) bool {
	return false
}

func (i *ImageRequest) SetModelName(modelName string) {
	if modelName != "" {
		i.Model = modelName
	}
}

type ImageResponse struct {
	Data     []ImageData     `json:"data"`
	Created  int64           `json:"created"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
}
type ImageData struct {
	Url           string `json:"url"`
	B64Json       string `json:"b64_json"`
	RevisedPrompt string `json:"revised_prompt"`
}
