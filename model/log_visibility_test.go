package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetUserLogsExcludesSystemLogs(t *testing.T) {
	truncateTables(t)

	logs := []Log{
		{UserId: 42, Type: LogTypeConsume, Content: "consume", TokenId: 9, CreatedAt: 100},
		{UserId: 42, Type: LogTypeSystem, Content: "admin-only health event", TokenId: 9, CreatedAt: 101},
		{UserId: 42, Type: LogTypeError, Content: "request error", TokenId: 9, CreatedAt: 102},
		{UserId: 7, Type: LogTypeSystem, Content: "other user system event", TokenId: 8, CreatedAt: 103},
	}
	require.NoError(t, LOG_DB.Create(&logs).Error)

	userLogs, total, err := GetUserLogs(42, LogTypeUnknown, 0, 0, "", "", 0, 20, "", "")
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)
	require.Len(t, userLogs, 2)
	assert.Equal(t, []int{LogTypeError, LogTypeConsume}, []int{userLogs[0].Type, userLogs[1].Type})

	systemLogs, systemTotal, err := GetUserLogs(42, LogTypeSystem, 0, 0, "", "", 0, 20, "", "")
	require.NoError(t, err)
	assert.Zero(t, systemTotal)
	assert.Empty(t, systemLogs)
}

func TestUserTokenLogsExcludeSystemLogsButAdminLogsKeepThem(t *testing.T) {
	truncateTables(t)

	logs := []Log{
		{UserId: 42, Type: LogTypeConsume, Content: "consume", TokenId: 9, CreatedAt: 100},
		{UserId: 42, Type: LogTypeSystem, Content: "admin-only health event", TokenId: 9, CreatedAt: 101},
	}
	require.NoError(t, LOG_DB.Create(&logs).Error)

	tokenLogs, err := GetLogByTokenId(9)
	require.NoError(t, err)
	require.Len(t, tokenLogs, 1)
	assert.Equal(t, LogTypeConsume, tokenLogs[0].Type)

	adminLogs, total, err := GetAllLogs(LogTypeSystem, 0, 0, "", "", "", 0, 20, 0, "", "")
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, adminLogs, 1)
	assert.Equal(t, LogTypeSystem, adminLogs[0].Type)
}
