package service

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRecordFinalChannelHealthErrorSkipsNoRecordErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("channel_id", 2)

	oldLogDB := model.LOG_DB
	model.LOG_DB = nil
	t.Cleanup(func() {
		model.LOG_DB = oldLogDB
	})

	err := types.NewErrorWithStatusCode(
		errors.New("idempotency key conflict"),
		types.ErrorCode("idempotency_key_conflict"),
		http.StatusConflict,
		types.ErrOptionWithSkipRetry(),
		types.ErrOptionWithNoRecordErrorLog(),
	)

	require.NotPanics(t, func() {
		RecordFinalChannelHealthError(c, err)
	})
}
