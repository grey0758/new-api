package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestImageResponseCaptureDoesNotWriteBeforePersistence(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	originalWriter := c.Writer
	captureWriter := newImageResponseCaptureWriter(originalWriter)
	c.Writer = captureWriter

	c.JSON(http.StatusOK, gin.H{"data": []gin.H{{"url": "https://provider.example/image.png"}}})

	require.True(t, captureWriter.Written())
	require.NotEmpty(t, captureWriter.body.Bytes())
	require.Empty(t, recorder.Body.Bytes())
	require.False(t, recorder.Flushed)

	c.Writer = originalWriter
	require.NoError(t, writeCapturedImageResponse(originalWriter, captureWriter.Status(), []byte(`{"task_id":"img_stable"}`)))
	require.JSONEq(t, `{"task_id":"img_stable"}`, recorder.Body.String())
	require.True(t, recorder.Flushed)
}
