package relay

import (
	"bytes"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

type imageResponseCaptureWriter struct {
	gin.ResponseWriter
	body        bytes.Buffer
	statusCode  int
	wroteHeader bool
}

func newImageResponseCaptureWriter(writer gin.ResponseWriter) *imageResponseCaptureWriter {
	return &imageResponseCaptureWriter{
		ResponseWriter: writer,
		statusCode:     http.StatusOK,
	}
}

func (writer *imageResponseCaptureWriter) WriteHeader(statusCode int) {
	if writer.wroteHeader {
		return
	}
	writer.statusCode = statusCode
	writer.wroteHeader = true
}

func (writer *imageResponseCaptureWriter) WriteHeaderNow() {
	if !writer.wroteHeader {
		writer.WriteHeader(http.StatusOK)
	}
}

func (writer *imageResponseCaptureWriter) Write(data []byte) (int, error) {
	writer.WriteHeaderNow()
	return writer.body.Write(data)
}

func (writer *imageResponseCaptureWriter) WriteString(data string) (int, error) {
	writer.WriteHeaderNow()
	return writer.body.WriteString(data)
}

func (writer *imageResponseCaptureWriter) Status() int {
	return writer.statusCode
}

func (writer *imageResponseCaptureWriter) Size() int {
	return writer.body.Len()
}

func (writer *imageResponseCaptureWriter) Written() bool {
	return writer.wroteHeader || writer.body.Len() > 0
}

func (writer *imageResponseCaptureWriter) Flush() {
	writer.WriteHeaderNow()
}

func writeCapturedImageResponse(writer gin.ResponseWriter, statusCode int, data []byte) error {
	if writer == nil {
		return nil
	}
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	writer.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	writer.WriteHeader(statusCode)
	_, err := writer.Write(data)
	if err == nil {
		writer.Flush()
	}
	return err
}

func clearCapturedImageResponseHeaders(writer gin.ResponseWriter) {
	if writer == nil {
		return
	}
	writer.Header().Del("Content-Length")
	writer.Header().Del("Content-Encoding")
	writer.Header().Del("Transfer-Encoding")
}
