package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
)

func TestNativeDistributorUsesCurrentGroupAuthorizationAndNeverReplaysPendingTools(t *testing.T) {
	t.Setenv("CODEX_NATIVE_ROUTE_GUARD_ENABLED", "true")
	oldDB, oldMemory, oldRedis := model.DB, common.MemoryCacheEnabled, common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "routes.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	model.DB, common.MemoryCacheEnabled, common.RedisEnabled = db, true, false
	t.Cleanup(func() { model.DB, common.MemoryCacheEnabled, common.RedisEnabled = oldDB, oldMemory, oldRedis })
	base71, base77 := "http://private-test/outer-tools", "http://private-test/v1"
	for _, channel := range []model.Channel{
		{Id: 90071, Type: 1, Status: common.ChannelStatusEnabled, Key: "synthetic-only", BaseURL: &base71, Group: "route-protest", Models: "native-test"},
		{Id: 90077, Type: 1, Status: common.ChannelStatusEnabled, Key: "synthetic-only", BaseURL: &base77, Group: "route-svip", Models: "native-test"},
	} {
		require.NoError(t, db.Create(&channel).Error)
		require.NoError(t, db.Create(&model.Ability{Group: channel.Group, Model: "native-test", ChannelId: channel.Id, Enabled: true}).Error)
	}
	model.InitChannelCache()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	dispatches := 0
	paused := false
	router.Use(func(c *gin.Context) {
		c.Set("id", 987654)
		common.SetContextKey(c, constant.ContextKeyUsingGroup, c.GetHeader("Test-Group"))
		common.SetContextKey(c, constant.ContextKeyUserGroup, c.GetHeader("Test-Group"))
		defer common.CleanupBodyStorage(c)
		c.Next()
	})
	router.POST("/v1/responses", Distribute(), func(c *gin.Context) {
		dispatches++
		output := []dto.ResponsesOutput{}
		if paused {
			output = append(output, dto.ResponsesOutput{Type: "function_call", CallId: "test-call"})
		}
		service.ObserveNativeResponsesOutcome(c, &dto.OpenAIResponsesResponse{Status: []byte(`"completed"`), Output: output})
		storage, err := common.GetBodyStorage(c)
		require.NoError(t, err)
		raw, err := storage.Bytes()
		require.NoError(t, err)
		channel := common.GetContextKeyInt(c, constant.ContextKeyChannelId)
		rewritten, err := service.ApplyNativeResponsesRouteKey(c, raw, channel == 90071)
		require.NoError(t, err)
		c.JSON(200, gin.H{"channel": channel, "group": service.NativeResponsesEffectiveGroup(c), "epoch": gjson.GetBytes(rewritten, "prompt_cache_key").String()})
	})
	send := func(session, turn, group string, items []any) *httptest.ResponseRecorder {
		raw, err := common.Marshal(map[string]any{"model": "native-test", "prompt_cache_key": session,
			"client_metadata": map[string]string{"session_id": session, "turn_id": turn}, "input": items, "store": false})
		require.NoError(t, err)
		r := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(raw)))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Test-Group", group)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	user := func(text string) any { return map[string]string{"role": "user", "content": text} }
	t.Run("completed boundary switches and switch-back creates another bridge epoch", func(t *testing.T) {
		items := []any{user("one")}
		var epochs []string
		for i, group := range []string{"route-svip", "route-protest", "route-svip", "route-protest"} {
			if i > 0 {
				items = append(items, user(fmt.Sprint(i)))
			}
			w := send("integration-completed", fmt.Sprint(i), group, items)
			require.Equal(t, 200, w.Code, w.Body.String())
			expected := int64(90077)
			if group == "route-protest" {
				expected = 90071
				epochs = append(epochs, gjson.Get(w.Body.String(), "epoch").String())
			}
			require.Equal(t, expected, gjson.Get(w.Body.String(), "channel").Int())
			require.Equal(t, group, gjson.Get(w.Body.String(), "group").String())
		}
		require.NotEqual(t, epochs[0], epochs[1])
	})
	t.Run("pending tools cannot leave removed group route", func(t *testing.T) {
		paused = true
		items := []any{user("work")}
		w := send("integration-pending", "one", "route-svip", items)
		require.Equal(t, 200, w.Code, w.Body.String())
		items = append(items, map[string]string{"type": "function_call", "call_id": "test-call", "arguments": "{}"},
			map[string]string{"type": "function_call_output", "call_id": "test-call", "output": "done"})
		before := dispatches
		w = send("integration-pending", "one", "route-protest", items)
		require.Equal(t, 400, w.Code, w.Body.String())
		require.Equal(t, "native_session_route_changed", gjson.Get(w.Body.String(), "error.code").String())
		require.Equal(t, before, dispatches)
		// The current group explicitly authorizes the original physical route.
		require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 90077).Update("group", "route-svip,route-protest").Error)
		model.InitChannelCache()
		paused = false
		w = send("integration-pending", "one", "route-protest", items)
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Equal(t, int64(90077), gjson.Get(w.Body.String(), "channel").Int())
		require.Equal(t, "route-protest", gjson.Get(w.Body.String(), "group").String())
		require.Equal(t, before+1, dispatches)
	})
}
