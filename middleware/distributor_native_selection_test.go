package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
)

type nativeSelectionFixture struct {
	db         *gorm.DB
	router     *gin.Engine
	dispatches int
}

func newNativeSelectionFixture(t *testing.T) *nativeSelectionFixture {
	t.Helper()
	require.NoError(t, i18n.Init())
	// Stale opt-in configuration must not resurrect the removed registry.
	t.Setenv("CODEX_NATIVE_ROUTE_GUARD_ENABLED", "true")
	t.Setenv("CODEX_NATIVE_ROUTE_GUARD_REQUIRE_REDIS", "false")
	t.Setenv("CODEX_NATIVE_ROUTE_GUARD_REDIS_URL", "")
	t.Setenv("CODEX_NATIVE_ROUTE_GUARD_NAMESPACE", t.Name())
	oldDB, oldLogDB, oldMemory, oldRedis := model.DB, model.LOG_DB, common.MemoryCacheEnabled, common.RedisEnabled
	oldAffinity := *operation_setting.GetChannelAffinitySetting()
	t.Cleanup(func() {
		model.DB, common.MemoryCacheEnabled, common.RedisEnabled = oldDB, oldMemory, oldRedis
		model.LOG_DB = oldLogDB
		*operation_setting.GetChannelAffinitySetting() = oldAffinity
	})
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "routes.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.Log{}))
	model.DB, common.MemoryCacheEnabled, common.RedisEnabled = db, true, false
	model.LOG_DB = db
	// Exercise ordinary cache affinity as well as fresh channel selection.
	*operation_setting.GetChannelAffinitySetting() = operation_setting.ChannelAffinitySetting{
		Enabled: true, SwitchOnSuccess: true, MaxEntries: 1000, DefaultTTLSeconds: 60,
		Rules: []operation_setting.ChannelAffinityRule{{
			Name: t.Name(), ModelRegex: []string{"^gpt-6\\.1-sol$"}, PathRegex: []string{"/v1/responses"},
			KeySources:        []operation_setting.ChannelAffinityKeySource{{Type: "gjson", Path: "prompt_cache_key"}},
			IncludeUsingGroup: true, IncludeRuleName: true, TTLSeconds: 60,
		}},
	}
	base47, base71 := "http://synthetic/v1", "http://synthetic/outer-tools"
	for _, channel := range []model.Channel{
		{Id: 92047, Type: 1, Status: common.ChannelStatusEnabled, Key: "synthetic", BaseURL: &base47, Group: "route-svip", Models: "gpt-6.1-sol"},
		{Id: 92071, Type: 1, Status: common.ChannelStatusEnabled, Key: "synthetic", BaseURL: &base71, Group: "route-protest", Models: "gpt-6.1-sol"},
	} {
		require.NoError(t, db.Create(&channel).Error)
		require.NoError(t, db.Create(&model.Ability{Group: channel.Group, Model: channel.Models, ChannelId: channel.Id, Enabled: true}).Error)
	}
	model.InitChannelCache()
	gin.SetMode(gin.TestMode)
	f := &nativeSelectionFixture{db: db, router: gin.New()}
	f.router.Use(func(c *gin.Context) {
		c.Set("id", 987654)
		common.SetContextKey(c, constant.ContextKeyUsingGroup, c.GetHeader("Test-Group"))
		common.SetContextKey(c, constant.ContextKeyUserGroup, c.GetHeader("Test-Group"))
		if c.GetHeader("Test-Deny-Model") != "" {
			common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
			common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{"other-model": true})
		}
		defer common.CleanupBodyStorage(c)
		c.Next()
	})
	f.router.POST("/v1/responses", Distribute(), func(c *gin.Context) {
		f.dispatches++
		storage, err := common.GetBodyStorage(c)
		require.NoError(t, err)
		raw, err := storage.Bytes()
		require.NoError(t, err)
		c.JSON(http.StatusOK, gin.H{
			"channel": common.GetContextKeyInt(c, constant.ContextKeyChannelId),
			"group":   common.GetContextKeyString(c, constant.ContextKeyUsingGroup),
			"request": gjson.ParseBytes(raw).Value(),
			"status":  "completed",
			"output":  []any{map[string]string{"type": "function_call", "call_id": "synthetic-call", "name": "synthetic", "arguments": "{}"}},
		})
	})
	return f
}

func (f *nativeSelectionFixture) send(t *testing.T, session, turn, group string, items []any, denyModel bool) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := common.Marshal(map[string]any{
		"model": "gpt-6.1-sol", "prompt_cache_key": session, "store": false,
		"client_metadata": map[string]string{"session_id": session, "turn_id": turn}, "input": items,
	})
	require.NoError(t, err)
	r := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(raw)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Test-Group", group)
	if denyModel {
		r.Header.Set("Test-Deny-Model", "1")
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	return w
}

func TestNativeSelectionFollowsCurrentGroupAcrossToolTurns(t *testing.T) {
	f := newNativeSelectionFixture(t)
	items := []any{map[string]string{"role": "user", "content": "synthetic work"}}
	w := f.send(t, t.Name(), "one", "route-svip", items, false)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, int64(92047), gjson.Get(w.Body.String(), "channel").Int())
	items = append(items, map[string]string{"type": "function_call", "call_id": "synthetic-call", "name": "synthetic", "arguments": "{}"},
		map[string]string{"type": "function_call_output", "call_id": "synthetic-call", "output": "done"})
	for i, group := range []string{"route-protest", "route-svip", "route-protest"} {
		turn := "one"
		if i == 2 {
			turn = "two"
			items = append(items, map[string]string{"role": "user", "content": "synthetic next turn"})
		}
		w = f.send(t, t.Name(), turn, group, items, false)
		require.Equal(t, 200, w.Code, w.Body.String())
		want := int64(92071)
		if group == "route-svip" {
			want = 92047
		}
		require.Equal(t, want, gjson.Get(w.Body.String(), "channel").Int())
		require.Equal(t, group, gjson.Get(w.Body.String(), "group").String())
		require.Equal(t, t.Name(), gjson.Get(w.Body.String(), "request.prompt_cache_key").String())
		require.Equal(t, turn, gjson.Get(w.Body.String(), "request.client_metadata.turn_id").String())
		original, err := common.Marshal(items)
		require.NoError(t, err)
		require.JSONEq(t, string(original), gjson.Get(w.Body.String(), "request.input").Raw)
	}
	require.Equal(t, 4, f.dispatches)
	// A different group without an authorized channel must still fail before dispatch.
	w = f.send(t, t.Name(), "two", "route-unauthorized", items, false)
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	require.Equal(t, 4, f.dispatches)
	w = f.send(t, t.Name(), "two", "route-protest", items, true)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	require.Equal(t, 4, f.dispatches)
}

func TestNativeSelectionSkipsCoolingHistoricalChannel(t *testing.T) {
	f := newNativeSelectionFixture(t)
	items := []any{map[string]string{"role": "user", "content": "synthetic work"}}
	w := f.send(t, t.Name(), "one", "route-svip", items, false)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, int64(92047), gjson.Get(w.Body.String(), "channel").Int())
	// Make the alternate channel available in the same currently authorized group.
	require.NoError(t, f.db.Model(&model.Channel{}).Where("id = ?", 92071).Update("group", "route-svip,route-protest").Error)
	require.NoError(t, f.db.Create(&model.Ability{Group: "route-svip", Model: "gpt-6.1-sol", ChannelId: 92071, Enabled: true}).Error)
	model.InitChannelCache()
	oldEnabled, oldProbe := common.AutomaticChannelCooldownEnabled, common.ChannelCooldownProbeEnabled
	oldThreshold, oldWindow, oldSeconds := common.ChannelCooldownFailureThreshold, common.ChannelCooldownFailureWindowSeconds, common.ChannelCooldownSeconds
	common.AutomaticChannelCooldownEnabled, common.ChannelCooldownProbeEnabled = true, false
	common.ChannelCooldownFailureThreshold, common.ChannelCooldownFailureWindowSeconds, common.ChannelCooldownSeconds = 1, 60, 60
	t.Cleanup(func() {
		service.ClearChannelCooldown(92047)
		common.AutomaticChannelCooldownEnabled, common.ChannelCooldownProbeEnabled = oldEnabled, oldProbe
		common.ChannelCooldownFailureThreshold, common.ChannelCooldownFailureWindowSeconds, common.ChannelCooldownSeconds = oldThreshold, oldWindow, oldSeconds
	})
	err := types.NewOpenAIError(errors.New("synthetic upstream 500"), types.ErrorCodeBadResponseStatusCode, 500)
	service.RecordChannelFailureForCooldown(*types.NewChannelError(92047, 1, "synthetic", false, "", false), err)
	require.True(t, service.IsChannelCoolingDown(92047))
	items = append(items, map[string]string{"role": "user", "content": "synthetic next turn"})
	w = f.send(t, t.Name(), "two", "route-svip", items, false)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, int64(92071), gjson.Get(w.Body.String(), "channel").Int())
	require.Equal(t, 2, f.dispatches)
}

func TestNativeSelectionIgnoresRetiredRegistryConfiguration(t *testing.T) {
	f := newNativeSelectionFixture(t)
	t.Setenv("CODEX_NATIVE_ROUTE_GUARD_REQUIRE_REDIS", "true")
	t.Setenv("CODEX_NATIVE_ROUTE_GUARD_REDIS_URL", "redis://127.0.0.1:1")
	w := f.send(t, t.Name(), "one", "route-protest", []any{map[string]string{"role": "user", "content": "synthetic"}}, false)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, int64(92071), gjson.Get(w.Body.String(), "channel").Int())
	require.Equal(t, 1, f.dispatches)
}
