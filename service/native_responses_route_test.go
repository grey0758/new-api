package service

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func nativeTestSetup(t *testing.T) {
	t.Helper()
	t.Setenv("CODEX_NATIVE_ROUTE_GUARD_ENABLED", "true")
	t.Setenv("CODEX_NATIVE_ROUTE_GUARD_REDIS_URL", "")
	prior := common.RedisEnabled
	common.RedisEnabled = false
	nativeRoutes.Lock()
	nativeRoutes.entries = make(map[string]string)
	nativeRoutes.Unlock()
	t.Cleanup(func() {
		common.RedisEnabled = prior
		nativeRoutes.Lock()
		nativeRoutes.entries = make(map[string]string)
		nativeRoutes.Unlock()
	})
}

func TestNativeRouteDedicatedRedisConfigurationFailsClosedWithoutSecretDisclosure(t *testing.T) {
	nativeTestSetup(t)
	t.Setenv("CODEX_NATIVE_ROUTE_GUARD_NAMESPACE", "test-dedicated-config")
	for _, value := range []string{"https://native-private:do-not-leak@invalid", "redis://native-private:do-not-leak@localhost:invalid"} {
		t.Setenv("CODEX_NATIVE_ROUTE_GUARD_REDIS_URL", value)
		_, err := nativeRouteRedisClient()
		require.Error(t, err)
		require.NotContains(t, err.Error(), "do-not-leak")
		require.NotContains(t, err.Error(), "native-private")
		_, _, err = readNativeRoute("never-create")
		require.Error(t, err)
	}
	t.Setenv("CODEX_NATIVE_ROUTE_GUARD_REDIS_URL", "redis://127.0.0.1:6379")
	t.Setenv("CODEX_NATIVE_ROUTE_GUARD_NAMESPACE", "")
	_, err := nativeRouteRedisClient()
	require.Error(t, err)
	require.Empty(t, nativeRoutes.entries)
}

func TestNativeRouteDedicatedRedisOutageNeverEnablesApplicationCacheOrMemoryFallback(t *testing.T) {
	nativeTestSetup(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	t.Setenv("CODEX_NATIVE_ROUTE_GUARD_REDIS_URL", "redis://"+address)
	t.Setenv("CODEX_NATIVE_ROUTE_GUARD_REQUIRE_REDIS", "true")
	t.Setenv("CODEX_NATIVE_ROUTE_GUARD_NAMESPACE", "test-dedicated-outage")
	prior := common.RDB
	client, err := nativeRouteRedisClient()
	require.NoError(t, err)
	require.NotNil(t, client)
	require.False(t, common.RedisEnabled)
	require.Same(t, prior, common.RDB)
	_, err = writeNativeRoute("no-fallback", "", nativeRouteRecord{})
	require.Error(t, err)
	require.Empty(t, nativeRoutes.entries)
	c := nativeTestContext(t, 1, "one", "protest", []any{nativeUser("private")})
	_, e := PreferredNativeResponsesRoute(c, "native-test")
	require.NotNil(t, e)
	require.Equal(t, "native_route_registry_unavailable", string(e.GetErrorCode()))
	require.Equal(t, http.StatusServiceUnavailable, e.StatusCode)
}
func nativeTestBody(turn string, items []any) string {
	b, _ := common.Marshal(map[string]any{"model": "gpt-6.1-sol", "prompt_cache_key": "test-private-session", "client_metadata": map[string]any{"session_id": "test-private-session", "turn_id": turn}, "input": items, "store": false, "stream": true})
	return string(b)
}
func nativeTestContext(t *testing.T, user int, turn, group string, items []any) *gin.Context {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(nativeTestBody(turn, items)))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("id", user)
	common.SetContextKey(c, constant.ContextKeyUsingGroup, group)
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	return c
}
func nativeUser(text string) any { return map[string]any{"role": "user", "content": text} }
func nativeFinish(c *gin.Context, tool bool) {
	o := []dto.ResponsesOutput{}
	if tool {
		o = append(o, dto.ResponsesOutput{Type: "custom_tool_call", CallId: "call-private"})
	}
	ObserveNativeResponsesOutcome(c, &dto.OpenAIResponsesResponse{Status: []byte(`"completed"`), Output: o})
	FinishNativeResponsesRoute(c)
}
func nativeTestClaim(t *testing.T, c *gin.Context, group string, channel int) {
	t.Helper()
	_, err := PreferredNativeResponsesRoute(c, "gpt-6.1-sol")
	require.Nil(t, err)
	require.Nil(t, AdmitNativeResponsesRoute(c, group, channel))
}
func TestNativeRouteCompletedGroupSwitchAndSwitchBackIsolatesBridgeEpoch(t *testing.T) {
	nativeTestSetup(t)
	items := []any{nativeUser("private one")}
	one := nativeTestContext(t, 1, "turn-one", "svip", items)
	nativeTestClaim(t, one, "svip", 77)
	nativeFinish(one, false)
	items = append(items, map[string]any{"role": "assistant", "content": "private answer"}, nativeUser("private two"))
	two := nativeTestContext(t, 1, "turn-two", "protest", items)
	pref, e := PreferredNativeResponsesRoute(two, "gpt-6.1-sol")
	require.Nil(t, e)
	require.Zero(t, pref)
	require.Nil(t, AdmitNativeResponsesRoute(two, "protest", 71))
	raw := []byte(nativeTestBody("turn-two", items))
	rewritten, err := ApplyNativeResponsesRouteKey(two, raw, true)
	require.NoError(t, err)
	epoch := gjson.GetBytes(rewritten, "prompt_cache_key").String()
	require.NotEqual(t, "test-private-session", epoch)
	require.Equal(t, gjson.GetBytes(raw, "input").Raw, gjson.GetBytes(rewritten, "input").Raw)
	require.Equal(t, gjson.GetBytes(raw, "client_metadata").Raw, gjson.GetBytes(rewritten, "client_metadata").Raw)
	unchanged, err := ApplyNativeResponsesRouteKey(two, raw, false)
	require.NoError(t, err)
	require.Equal(t, raw, unchanged)
	nativeFinish(two, false)
	items = append(items, nativeUser("three"))
	three := nativeTestContext(t, 1, "turn-three", "svip", items)
	nativeTestClaim(t, three, "svip", 77)
	nativeFinish(three, false)
	items = append(items, nativeUser("four"))
	four := nativeTestContext(t, 1, "turn-four", "protest", items)
	nativeTestClaim(t, four, "protest", 71)
	again, err := ApplyNativeResponsesRouteKey(four, []byte(nativeTestBody("turn-four", items)), true)
	require.NoError(t, err)
	require.NotEqual(t, epoch, gjson.GetBytes(again, "prompt_cache_key").String())
	// An old turn cannot overwrite the newer routing owner or revive its RPC.
	late := nativeTestContext(t, 1, "turn-one", "svip", []any{nativeUser("private one")})
	_, e = PreferredNativeResponsesRoute(late, "gpt-6.1-sol")
	require.NotNil(t, e)
	require.Equal(t, NativeRouteChangedCode, e.GetErrorCode())
}
func TestNativeRoutePausedToolsCannotMoveAcrossGroupsOrProviders(t *testing.T) {
	nativeTestSetup(t)
	items := []any{nativeUser("work")}
	one := nativeTestContext(t, 1, "turn-one", "svip", items)
	nativeTestClaim(t, one, "svip", 77)
	nativeFinish(one, true)
	items = append(items, map[string]any{"type": "custom_tool_call", "call_id": "call-private", "name": "exec", "input": "private"}, map[string]any{"type": "custom_tool_call_output", "call_id": "call-private", "output": "private"})
	next := nativeTestContext(t, 1, "turn-one", "protest", items)
	pref, e := PreferredNativeResponsesRoute(next, "gpt-6.1-sol")
	require.Nil(t, e)
	require.Equal(t, 77, pref)
	e = AdmitNativeResponsesRoute(next, "protest", 71)
	require.NotNil(t, e)
	require.Equal(t, http.StatusBadRequest, e.StatusCode)
	require.False(t, NativeResponsesRouteClaimed(next))
	// Same physical route may continue under an independently authorized group.
	require.Nil(t, AdmitNativeResponsesRoute(next, "protest", 77))
	require.Empty(t, nativeRouteRequestFromContext(next).Record.Epoch)
	nativeFinish(next, false)
}
func TestNativeRouteSafeBoundaryRequiresEndedStateAndExactHistory(t *testing.T) {
	for _, state := range []string{"inflight", "paused", "unknown", "completed", "failed"} {
		for _, changed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/changed=%v", state, changed), func(t *testing.T) {
				nativeTestSetup(t)
				items := []any{nativeUser("work")}
				one := nativeTestContext(t, 1, "one", "a", items)
				nativeTestClaim(t, one, "a", 71)
				one.Set("native_route_outcome", state)
				FinishNativeResponsesRoute(one)
				history := append([]any{}, items...)
				if changed {
					history[0] = nativeUser("modified")
				}
				history = append(history, nativeUser("new"))
				next := nativeTestContext(t, 1, "two", "b", history)
				if state == "inflight" {
					ctx, cancel := context.WithCancel(next.Request.Context())
					cancel()
					next.Request = next.Request.WithContext(ctx)
					_, err := PreferredNativeResponsesRoute(next, "gpt-6.1-sol")
					require.NotNil(t, err)
					return
				}
				pref, e := PreferredNativeResponsesRoute(next, "gpt-6.1-sol")
				require.Nil(t, e)
				allowed := !changed && (state == "completed" || state == "failed")
				require.Equal(t, allowed, pref == 0)
				e = AdmitNativeResponsesRoute(next, "b", 77)
				if allowed {
					require.Nil(t, e)
				} else {
					require.NotNil(t, e)
					require.Equal(t, NativeRouteChangedCode, e.GetErrorCode())
				}
			})
		}
	}
}

func TestNativeRouteIdentityIsolationAndFeatureFlag(t *testing.T) {
	nativeTestSetup(t)
	one := nativeTestContext(t, 1, "one", "a", []any{nativeUser("work")})
	nativeTestClaim(t, one, "a", 71)
	other := nativeTestContext(t, 2, "one", "b", []any{nativeUser("work")})
	pref, e := PreferredNativeResponsesRoute(other, "gpt-6.1-sol")
	require.Nil(t, e)
	require.Zero(t, pref)
	require.NotEqual(t, nativeRouteRequestFromContext(one).Boundary.Key, nativeRouteRequestFromContext(other).Boundary.Key)
	t.Setenv("CODEX_NATIVE_ROUTE_GUARD_ENABLED", "false")
	off := nativeTestContext(t, 1, "two", "b", []any{nativeUser("work")})
	pref, e = PreferredNativeResponsesRoute(off, "gpt-6.1-sol")
	require.Nil(t, e)
	require.Zero(t, pref)
	require.Nil(t, nativeRouteRequestFromContext(off))
}
func TestNativeRouteCASAndOldCompletionCannotOverwriteNewOwner(t *testing.T) {
	nativeTestSetup(t)
	one := nativeTestContext(t, 1, "one", "a", []any{nativeUser("work")})
	nativeTestClaim(t, one, "a", 71)
	next := nativeTestContext(t, 1, "one", "a", []any{nativeUser("work")})
	nativeTestClaim(t, next, "a", 71)
	nativeFinish(one, false)
	r, _, err := readNativeRoute(nativeRouteRequestFromContext(one).Boundary.Key)
	require.NoError(t, err)
	require.Equal(t, "completed", r.State)
	nativeFinish(next, true)
	r, raw, err := readNativeRoute(nativeRouteRequestFromContext(one).Boundary.Key)
	require.NoError(t, err)
	require.Equal(t, "completed", r.State) // A duplicate HTTP observer cannot replace its owner.
	var successes int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			copy := *r
			copy.Lease = fmt.Sprint(i)
			ok, e := writeNativeRoute(nativeRouteRequestFromContext(one).Boundary.Key, raw, copy)
			if e == nil && ok {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	require.Equal(t, 1, successes)
}
func TestNativeRouteCapacityDoesNotEvictLiveOwnersAndExpiredEntriesReclaim(t *testing.T) {
	nativeTestSetup(t)
	nativeRoutes.Lock()
	for i := 0; i < nativeRouteCapacity; i++ {
		b, _ := common.Marshal(nativeRouteRecord{Expires: time.Now().Add(time.Hour).Unix()})
		nativeRoutes.entries[fmt.Sprint(i)] = string(b)
	}
	nativeRoutes.Unlock()
	ok, e := writeNativeRoute("fresh", "", nativeRouteRecord{Expires: time.Now().Add(time.Hour).Unix()})
	require.False(t, ok)
	require.Error(t, e)
	nativeRoutes.Lock()
	b, _ := common.Marshal(nativeRouteRecord{Expires: 1})
	nativeRoutes.entries["0"] = string(b)
	nativeRoutes.Unlock()
	ok, e = writeNativeRoute("fresh", "", nativeRouteRecord{Expires: time.Now().Add(time.Hour).Unix()})
	require.NoError(t, e)
	require.True(t, ok)
}
func TestNativeRouteCancellationAndUnknownResponseNeverAuthorizeMigration(t *testing.T) {
	nativeTestSetup(t)
	one := nativeTestContext(t, 1, "one", "a", []any{nativeUser("work")})
	nativeTestClaim(t, one, "a", 71)
	ctx, cancel := context.WithCancel(one.Request.Context())
	one.Request = one.Request.WithContext(ctx)
	cancel()
	nativeFinish(one, false)
	r, _, e := readNativeRoute(nativeRouteRequestFromContext(one).Boundary.Key)
	require.NoError(t, e)
	require.Equal(t, "unknown", r.State)
}

func TestNativeRouteExactNumbersAndModelChangeDoNotHideOwner(t *testing.T) {
	nativeTestSetup(t)
	one := nativeTestContext(t, 1, "one", "a", []any{nativeUser("work")})
	raw := strings.Replace(nativeTestBody("one", []any{map[string]any{"role": "user", "content": "work", "n": "NUMBER"}}), `"NUMBER"`, "9007199254740992", 1)
	one.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(raw))
	nativeTestClaim(t, one, "a", 71)
	nativeFinish(one, false)
	two := nativeTestContext(t, 1, "two", "b", []any{nativeUser("new")})
	changed := strings.Replace(raw, "9007199254740992", "9007199254740993", 1)
	changed = strings.Replace(changed, `"turn_id":"one"`, `"turn_id":"two"`, 1)
	changed = strings.Replace(changed, `}],"model"`, `},{"role":"user","content":"new"}],"model"`, 1)
	two.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(changed))
	b := nativeBoundary(two, "different-model")
	require.NotNil(t, b)
	require.Equal(t, nativeRouteRequestFromContext(one).Boundary.Key, b.Key)
	require.NotEqual(t, nativeRouteRequestFromContext(one).Boundary.History[0], b.History[0])
	r, _, err := readNativeRoute(b.Key)
	require.NoError(t, err)
	require.False(t, nativeCanSwitch(*b, *r))
}

func TestNativeRouteCompactionNeedsExactToolFreeEnvelopeAndDoesNotRetireBusiness(t *testing.T) {
	nativeTestSetup(t)
	items := []any{nativeUser("work")}
	one := nativeTestContext(t, 1, "one", "a", items)
	nativeTestClaim(t, one, "a", 71)
	nativeFinish(one, true)
	for _, valid := range []bool{true, false} {
		c := nativeTestContext(t, 1, "one", "b", items)
		payload := map[string]any{}
		require.NoError(t, common.UnmarshalJsonStr(nativeTestBody("one", items), &payload))
		payload["client_metadata"] = map[string]any{"session_id": "test-private-session", "turn_id": "one", "x-codex-turn-metadata": map[string]any{"request_kind": "compaction", "compaction": map[string]any{"implementation": "responses", "phase": "pre_turn"}}}
		payload["tool_choice"] = "auto"
		if !valid {
			payload["tools"] = []any{map[string]string{"type": "function", "name": "private-tool"}}
		}
		raw, err := common.Marshal(payload)
		require.NoError(t, err)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(raw)))
		pref, e := PreferredNativeResponsesRoute(c, "gpt-6.1-sol")
		require.Nil(t, e)
		require.Equal(t, valid, nativeRouteRequestFromContext(c).Boundary.Compact)
		if valid {
			require.Zero(t, pref)
			require.Nil(t, AdmitNativeResponsesRoute(c, "b", 77))
			FinishNativeResponsesRoute(c)
		} else {
			require.Equal(t, 71, pref)
			require.NotNil(t, AdmitNativeResponsesRoute(c, "b", 77))
		}
	}
	r, _, err := readNativeRoute(nativeRouteRequestFromContext(one).Boundary.Key)
	require.NoError(t, err)
	require.Equal(t, "paused", r.State)
	require.Empty(t, r.Retired)
}

func TestNativeRouteInflightAppendWaitsButRetiredTurnRejectsImmediately(t *testing.T) {
	nativeTestSetup(t)
	items := []any{nativeUser("work")}
	one := nativeTestContext(t, 1, "one", "a", items)
	nativeTestClaim(t, one, "a", 71)
	two := nativeTestContext(t, 1, "two", "b", append(items, nativeUser("new")))
	finished := make(chan struct{})
	go func() {
		time.Sleep(20 * time.Millisecond)
		nativeFinish(one, false)
		close(finished)
	}()
	pref, e := PreferredNativeResponsesRoute(two, "gpt-6.1-sol")
	require.Nil(t, e)
	<-finished
	require.Zero(t, pref)
	require.Nil(t, AdmitNativeResponsesRoute(two, "b", 77))
	late := nativeTestContext(t, 1, "one", "a", items)
	_, e = PreferredNativeResponsesRoute(late, "gpt-6.1-sol")
	require.NotNil(t, e)
	require.Equal(t, NativeRouteChangedCode, e.GetErrorCode())
}

func TestNativeRouteRedisCASAndTTLOnIsolatedRedis(t *testing.T) {
	address := os.Getenv("NATIVE_ROUTE_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("requires an isolated Redis instance, never production")
	}
	nativeTestSetup(t)
	prior := common.RDB
	common.RDB = redis.NewClient(&redis.Options{Addr: address})
	common.RedisEnabled = true
	t.Cleanup(func() { common.RDB.Close(); common.RDB = prior })
	key := "test-" + common.GetUUID()
	t.Cleanup(func() {
		common.RDB.Del(context.Background(), nativeRouteNamespace+key)
		common.RDB.ZRem(context.Background(), nativeRouteNamespace+"index", nativeRouteNamespace+key)
	})
	record := nativeRouteRecord{Group: "a", Channel: 71, Turn: nativeDigest("one"), State: "paused", HistoryCount: 1, HistoryDigest: nativeDigest([]string{"one"}), Expires: time.Now().Add(nativeRouteTTL).Unix()}
	ok, err := writeNativeRoute(key, "", record)
	require.NoError(t, err)
	require.True(t, ok)
	r, raw, err := readNativeRoute(key)
	require.NoError(t, err)
	require.Equal(t, record.Channel, r.Channel)
	var wg sync.WaitGroup
	var mu sync.Mutex
	winners := 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			copy := record
			copy.Lease = fmt.Sprint(i)
			ok, e := writeNativeRoute(key, raw, copy)
			if e == nil && ok {
				mu.Lock()
				winners++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	require.Equal(t, 1, winners)
	ttl, err := common.RDB.TTL(context.Background(), nativeRouteNamespace+key).Result()
	require.NoError(t, err)
	require.InDelta(t, nativeRouteTTL.Seconds(), ttl.Seconds(), 5)
}

func TestNativeRouteRedisCASAndTTLOnDedicatedRedisWithApplicationCacheDisabled(t *testing.T) {
	address := os.Getenv("NATIVE_ROUTE_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("requires an isolated Redis instance, never production")
	}
	nativeTestSetup(t)
	t.Setenv("CODEX_NATIVE_ROUTE_GUARD_REDIS_URL", "redis://"+address)
	t.Setenv("CODEX_NATIVE_ROUTE_GUARD_REQUIRE_REDIS", "true")
	t.Setenv("CODEX_NATIVE_ROUTE_GUARD_NAMESPACE", "test-dedicated-cas")
	prior := common.RDB
	client, err := nativeRouteRedisClient()
	require.NoError(t, err)
	require.False(t, common.RedisEnabled)
	require.Same(t, prior, common.RDB)
	key := "dedicated-test-" + common.GetUUID()
	t.Cleanup(func() {
		client.Del(context.Background(), nativeRouteNamespace+key)
		client.ZRem(context.Background(), nativeRouteNamespace+"index", nativeRouteNamespace+key)
	})
	record := nativeRouteRecord{Group: "a", Channel: 71, Turn: nativeDigest("one"), State: "paused", HistoryCount: 1, HistoryDigest: nativeDigest([]string{"one"}), Expires: time.Now().Add(nativeRouteTTL).Unix()}
	ok, err := writeNativeRoute(key, "", record)
	require.NoError(t, err)
	require.True(t, ok)
	r, raw, err := readNativeRoute(key)
	require.NoError(t, err)
	require.Equal(t, 71, r.Channel)
	var wg sync.WaitGroup
	var mu sync.Mutex
	winners := 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			copy := record
			copy.Lease = fmt.Sprint(i)
			ok, e := writeNativeRoute(key, raw, copy)
			if e == nil && ok {
				mu.Lock()
				winners++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	require.Equal(t, 1, winners)
	ttl, err := client.TTL(context.Background(), nativeRouteNamespace+key).Result()
	require.NoError(t, err)
	require.InDelta(t, nativeRouteTTL.Seconds(), ttl.Seconds(), 5)
	require.False(t, common.RedisEnabled)
	require.Same(t, prior, common.RDB)
}
