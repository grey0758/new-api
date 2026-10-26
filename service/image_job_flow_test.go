package service

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestFlowImageJobRequestValidationAndExplicitZeros(t *testing.T) {
	for _, tc := range []struct {
		body string
		ok   bool
	}{
		{`{"prompt":"x","n":1,"top_p":0,"seed":0,"image_size":"4K","generationConfig":{"imageConfig":{"imageSize":"4K"}},"image_urls":["https://fixture.invalid/first","data:image/png;base64,aA=="]}`, true},
		{`{"prompt":"x","n":2}`, false},
		{`{"prompt":"x","response_format":"b64_json"}`, false},
		{`{"prompt":"x","image_urls":["file:///etc/passwd"]}`, false},
		{`{"prompt":"x","image_urls":{},"messages":[]}`, false},
		{`{"prompt":"x","image_urls":["https://fixture.invalid/a"],"image":"https://fixture.invalid/b"}`, false},
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/images/generations", strings.NewReader(tc.body))
		body, err := BuildFlowImageJobRequest(c, "mapped-flow-image")
		if !tc.ok {
			require.Error(t, err)
			continue
		}
		require.NoError(t, err)
		var fields map[string]any
		require.NoError(t, common.Unmarshal(body, &fields))
		require.Equal(t, float64(0), fields["top_p"])
		require.Equal(t, float64(0), fields["seed"])
		require.Equal(t, "mapped-flow-image", fields["model"])
		require.Equal(t, "4K", fields["image_size"])
		storage, _ := common.GetBodyStorage(c)
		_ = storage.Close()
	}
}

func TestFlowImageJobResponseConversionNeverDownloads(t *testing.T) {
	for _, tc := range []struct {
		content, url, b64 string
		ok                bool
	}{
		{"![Generated Image](https://fixture.invalid/image(a).png?expires=123)", "https://fixture.invalid/image(a).png?expires=123", "", true},
		{"![Generated Image](data:image/png;base64,aGVsbG8=)", "", "aGVsbG8=", true},
		{"generation not available", "", "", false},
		{"![Generated Image](file:///etc/passwd)", "", "", false},
	} {
		body, err := common.Marshal(map[string]any{"created": 123, "choices": []any{map[string]any{"message": map[string]string{"content": tc.content}}}})
		require.NoError(t, err)
		converted, err := ConvertFlowImageJobResponse(body)
		if !tc.ok {
			require.Error(t, err)
			continue
		}
		require.NoError(t, err)
		var response dto.ImageResponse
		require.NoError(t, common.Unmarshal(converted, &response))
		require.Len(t, response.Data, 1)
		require.Equal(t, tc.url, response.Data[0].Url)
		require.Equal(t, tc.b64, response.Data[0].B64Json)
	}
}
