package service

// Native Codex tool RPCs have a route owner. Group and token changes must not
// silently turn a continuation into a request to a different provider. The
// bounded registry holds only digests and routing metadata, never tool data.
import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Outlive the bridge's 30-day original-key affinity. Dropping an epoch sooner
// could resurrect a disabled historical account when the original key returns.
// This is digest-only routing state; private request packets retain their 48h TTL.
const nativeRouteTTL = 31 * 24 * time.Hour
const nativeRouteCapacity = 8192
const nativeRouteNamespace = "new-api:native_responses_route:v1:"
const nativeRouteContext = "native_responses_route"
const NativeRouteChangedCode types.ErrorCode = "native_session_route_changed"
const NativeRouteChangedMessage = "This native turn belongs to a different route or group. No request or tool result was replayed. Finish it on its original authorized route, or send a complete new user turn after it has ended. If that route is no longer available, start a new session with the completed work summarized."

type nativeRouteBoundary struct {
	Key, Turn string
	History   []string
	Safe      bool
	Compact   bool
}
type nativeRouteRecord struct {
	Group         string   `json:"group"`
	Channel       int      `json:"channel"`
	Turn          string   `json:"turn"`
	State         string   `json:"state"`
	HistoryCount  int      `json:"history_count"`
	HistoryDigest string   `json:"history_digest"`
	Epoch         string   `json:"epoch"`
	Lease         string   `json:"lease"`
	LeaseUntil    int64    `json:"lease_until"`
	Expires       int64    `json:"expires"`
	Retired       []string `json:"retired,omitempty"`
}
type nativeRouteRequest struct {
	Boundary nativeRouteBoundary
	Prior    *nativeRouteRecord
	Record   *nativeRouteRecord
	Raw      string
	Switched bool
	Observer bool // Only the CAS owner may finish the routing record.
	Deferred bool // A same-route relay must prove acceptance of a new turn.
}

var nativeRoutes = struct {
	sync.Mutex
	entries map[string]string
}{entries: make(map[string]string)}

func nativeDigest(v any) string {
	b, _ := common.Marshal(v)
	return fmt.Sprintf("%x", sha256.Sum256(b))
}
func nativeRouteError(code types.ErrorCode, message string, status int) *types.NewAPIError {
	return types.NewErrorWithStatusCode(errors.New(message), code, status, types.ErrOptionWithSkipRetry(), types.ErrOptionWithPreserveUserError())
}
func nativeRegistryError() *types.NewAPIError {
	return nativeRouteError("native_route_registry_unavailable", "Native session routing state is temporarily unavailable. No request was dispatched; retry later.", http.StatusServiceUnavailable)
}
func nativeBoundary(c *gin.Context, model string) *nativeRouteBoundary {
	if c == nil || c.Request == nil || c.Request.Method != http.MethodPost || c.Request.URL.Path != "/v1/responses" || !common.GetEnvOrDefaultBool("CODEX_NATIVE_ROUTE_GUARD_ENABLED", false) || c.GetInt("id") <= 0 {
		return nil
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil
	}
	raw, err := storage.Bytes()
	if err != nil {
		return nil
	}
	p := gjson.ParseBytes(raw)
	key := p.Get("prompt_cache_key").String()
	turn := p.Get("client_metadata.turn_id").String()
	session := p.Get("client_metadata.session_id").String()
	if session == "" {
		session = p.Get("client_metadata.thread_id").String()
	}
	if key == "" || key != session || turn == "" || len(key) > 256 || len(turn) > 256 {
		return nil
	}
	items := p.Get("input")
	if !items.IsArray() {
		return nil
	}
	history := make([]string, 0)
	calls := map[string]string{}
	outputs := map[string]bool{}
	paired := true
	catalogTools := false
	last := gjson.Result{}
	for _, item := range items.Array() {
		typ := item.Get("type").String()
		if typ == "additional_tools" {
			tools := item.Get("tools")
			catalogTools = catalogTools || tools.Exists() && (!tools.IsArray() || len(tools.Array()) > 0)
			continue
		}
		if !item.IsObject() {
			return nil
		}
		var obj any
		if common.DecodeJsonWithNumber(strings.NewReader(item.Raw), &obj) != nil {
			return nil
		}
		history = append(history, nativeDigest(obj))
		last = item
		id := item.Get("call_id").String()
		switch typ {
		case "function_call", "custom_tool_call":
			if id == "" || calls[id] != "" {
				paired = false
			}
			calls[id] = typ
		case "function_call_output", "custom_tool_call_output":
			want := strings.TrimSuffix(typ, "_output")
			if id == "" || calls[id] != want || outputs[id] {
				paired = false
			}
			outputs[id] = true
		case "item_reference":
			paired = false
		}
	}
	if len(history) == 0 || len(history) > 8192 {
		return nil
	}
	paired = paired && len(calls) == len(outputs)
	text := last.Get("content").String()
	if last.Get("content").IsArray() {
		text = ""
		for _, x := range last.Get("content").Array() {
			if x.Get("text").Type == gjson.String {
				text += x.Get("text").String()
			}
		}
	}
	meta := p.Get("client_metadata.x-codex-turn-metadata")
	if meta.Type == gjson.String {
		meta = gjson.Parse(meta.String())
	}
	newUser := last.Get("role").String() == "user" && (last.Get("type").String() == "" || last.Get("type").String() == "message") && strings.TrimSpace(text) != ""
	stateless := p.Get("store").Raw == "false" && !p.Get("previous_response_id").Exists() && !p.Get("conversation").Exists() && !p.Get("background").Bool()
	requested := meta.Get("request_kind").String() == "compaction"
	tools := p.Get("tools")
	toolFree := !tools.Exists() || tools.IsArray() && len(tools.Array()) == 0
	compact := requested && meta.Get("compaction.implementation").String() == "responses" && slices.Contains([]string{"pre_turn", "mid_turn", "manual"}, meta.Get("compaction.phase").String()) && toolFree && !catalogTools && p.Get("tool_choice").String() == "auto" && newUser && paired && stateless
	safe := newUser && paired && !requested && stateless
	// Tokens, groups and model changes must not hide a live session owner.
	return &nativeRouteBoundary{Key: nativeDigest([]any{os.Getenv("CODEX_NATIVE_ROUTE_GUARD_NAMESPACE"), c.GetInt("id"), key}), Turn: nativeDigest(turn), History: history, Safe: safe, Compact: compact}
}

func nativeRegistryReady() bool {
	if common.RedisEnabled && common.RDB == nil {
		return false
	}
	return !common.GetEnvOrDefaultBool("CODEX_NATIVE_ROUTE_GUARD_REQUIRE_REDIS", false) || common.RedisEnabled && common.RDB != nil && os.Getenv("CODEX_NATIVE_ROUTE_GUARD_NAMESPACE") != ""
}
func readNativeRoute(key string) (*nativeRouteRecord, string, error) {
	if !nativeRegistryReady() {
		return nil, "", errors.New("native_route_registry_unavailable")
	}
	var raw string
	if common.RedisEnabled && common.RDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		v, err := common.RDB.Get(ctx, nativeRouteNamespace+key).Result()
		if errors.Is(err, redis.Nil) {
			return nil, "", nil
		}
		if err != nil {
			return nil, "", err
		}
		raw = v
	} else {
		nativeRoutes.Lock()
		raw = nativeRoutes.entries[key]
		nativeRoutes.Unlock()
	}
	if raw == "" {
		return nil, "", nil
	}
	var v nativeRouteRecord
	if err := common.UnmarshalJsonStr(raw, &v); err != nil {
		return nil, "", err
	}
	if v.Expires <= time.Now().Unix() {
		return nil, raw, nil
	}
	if v.Channel <= 0 || v.Group == "" || len(v.Turn) != 64 || v.HistoryCount < 1 || v.HistoryCount > 8192 || len(v.HistoryDigest) != 64 || len(v.Retired) > 128 || !slices.Contains([]string{"inflight", "paused", "unknown", "completed", "failed"}, v.State) || v.State == "inflight" && v.Lease == "" {
		return nil, "", errors.New("native_route_invalid_record")
	}
	if v.Epoch != "" && (!strings.HasPrefix(v.Epoch, "native-route-") || len(v.Epoch) != len("native-route-")+48) {
		return nil, "", errors.New("native_route_invalid_epoch")
	}
	return &v, raw, nil
}

// Compare the exact prior value atomically across both old and green processes.
// The expiry index bounds Redis entries without evicting live routing owners.
const nativeRouteCAS = `
local current=redis.call('GET',KEYS[1]) or ''
if current~=ARGV[1] then return 0 end
redis.call('ZREMRANGEBYSCORE',KEYS[2],'-inf',ARGV[4])
if current=='' and redis.call('ZCARD',KEYS[2])>=tonumber(ARGV[5]) then return -1 end
redis.call('SET',KEYS[1],ARGV[2],'EX',ARGV[3])
redis.call('ZADD',KEYS[2],tonumber(ARGV[4])+tonumber(ARGV[3]),KEYS[1])
redis.call('EXPIRE',KEYS[2],ARGV[3])
return 1`

func writeNativeRoute(key, expected string, record nativeRouteRecord) (bool, error) {
	if !nativeRegistryReady() {
		return false, errors.New("native_route_registry_unavailable")
	}
	b, err := common.Marshal(record)
	if err != nil {
		return false, err
	}
	raw := string(b)
	if common.RedisEnabled && common.RDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		n, err := common.RDB.Eval(ctx, nativeRouteCAS, []string{nativeRouteNamespace + key, nativeRouteNamespace + "index"}, expected, raw, int(nativeRouteTTL.Seconds()), time.Now().Unix(), nativeRouteCapacity).Int()
		if err != nil {
			return false, err
		}
		if n < 0 {
			return false, errors.New("native_route_capacity")
		}
		return n == 1, nil
	}
	nativeRoutes.Lock()
	defer nativeRoutes.Unlock()
	if nativeRoutes.entries[key] != expected {
		return false, nil
	}
	if _, ok := nativeRoutes.entries[key]; !ok && len(nativeRoutes.entries) >= nativeRouteCapacity {
		for k, v := range nativeRoutes.entries {
			var r nativeRouteRecord
			if common.UnmarshalJsonStr(v, &r) == nil && r.Expires <= time.Now().Unix() {
				delete(nativeRoutes.entries, k)
			}
		}
		if len(nativeRoutes.entries) >= nativeRouteCapacity {
			return false, errors.New("native_route_capacity")
		}
	}
	nativeRoutes.entries[key] = raw
	return true, nil
}
func nativeCanSwitch(b nativeRouteBoundary, r nativeRouteRecord) bool {
	return b.Safe && b.Turn != r.Turn && (r.State == "completed" || r.State == "failed") && r.HistoryCount > 0 && len(b.History) > r.HistoryCount && nativeDigest(b.History[:r.HistoryCount]) == r.HistoryDigest
}

func nativeSameRequest(b nativeRouteBoundary, r nativeRouteRecord) bool {
	return b.Turn == r.Turn && len(b.History) == r.HistoryCount && nativeDigest(b.History) == r.HistoryDigest
}

func waitNativeRoute(c *gin.Context, r *nativeRouteRequest) error {
	deadline := time.NewTimer(60 * time.Second)
	defer deadline.Stop()
	for r.Prior != nil && r.Prior.State == "inflight" && !nativeSameRequest(r.Boundary, *r.Prior) {
		if r.Prior.LeaseUntil > 0 && r.Prior.LeaseUntil <= time.Now().Unix() {
			// Process loss is not completion. Permit only the original route to
			// consult its relay, never a migration inferred from elapsed time.
			copy := *r.Prior
			copy.State, copy.Lease = "unknown", ""
			if _, err := writeNativeRoute(r.Boundary.Key, r.Raw, copy); err != nil {
				return err
			}
		}
		select {
		case <-c.Request.Context().Done():
			return c.Request.Context().Err()
		case <-deadline.C:
			return errors.New("native_route_busy")
		case <-time.After(100 * time.Millisecond):
		}
		var err error
		r.Prior, r.Raw, err = readNativeRoute(r.Boundary.Key)
		if err != nil {
			return err
		}
	}
	return nil
}

// Returns an existing route only when moving this native request is unsafe.
// The distributor must still enforce the current group's channel authorization.
func PreferredNativeResponsesRoute(c *gin.Context, model string) (int, *types.NewAPIError) {
	b := nativeBoundary(c, model)
	if b == nil {
		return 0, nil
	}
	r, raw, err := readNativeRoute(b.Key)
	if err != nil {
		return 0, nativeRegistryError()
	}
	request := &nativeRouteRequest{Boundary: *b, Prior: r, Raw: raw}
	c.Set(nativeRouteContext, request)
	if !b.Compact && r != nil && slices.Contains(r.Retired, b.Turn) {
		return 0, nativeRouteError(NativeRouteChangedCode, NativeRouteChangedMessage, http.StatusBadRequest)
	}
	if !b.Compact {
		if err := waitNativeRoute(c, request); err != nil {
			return 0, nativeRegistryError()
		}
		r = request.Prior
	}
	if !b.Compact && r != nil && slices.Contains(r.Retired, b.Turn) {
		return 0, nativeRouteError(NativeRouteChangedCode, NativeRouteChangedMessage, http.StatusBadRequest)
	}
	if b.Compact || r == nil || nativeCanSwitch(*b, *r) {
		return 0, nil
	}
	return r.Channel, nil
}
func nativeRouteRequestFromContext(c *gin.Context) *nativeRouteRequest {
	if c == nil {
		return nil
	}
	v, _ := c.Get(nativeRouteContext)
	r, _ := v.(*nativeRouteRequest)
	return r
}

// Called after authorization and channel selection, before any upstream request.
func AdmitNativeResponsesRoute(c *gin.Context, group string, channel int) *types.NewAPIError {
	r := nativeRouteRequestFromContext(c)
	if r == nil {
		return nil
	}
	if r.Boundary.Compact {
		if r.Prior != nil && r.Prior.Channel == channel {
			copy := *r.Prior
			r.Record = &copy
		}
		return nil
	}
	for attempt := 0; attempt < 4; attempt++ {
		old := r.Prior
		if old != nil && old.State == "inflight" {
			if nativeSameRequest(r.Boundary, *old) && old.Channel == channel {
				copy := *old
				r.Record = &copy
				return nil // The bridge coalesces duplicates; no registry ownership change.
			}
			return nativeRegistryError() // Another admission won after selection.
		}
		if old != nil && slices.Contains(old.Retired, r.Boundary.Turn) {
			return nativeRouteError(NativeRouteChangedCode, NativeRouteChangedMessage, http.StatusBadRequest)
		}
		retired := []string{}
		if old != nil {
			retired = append(retired, old.Retired...)
			if nativeCanSwitch(r.Boundary, *old) {
				if len(retired) >= 128 {
					return nativeRouteError("native_route_history_capacity", "Native session routing history is full. Start a new session with completed work summarized.", http.StatusTooManyRequests)
				}
				retired = append(retired, old.Turn)
			}
		}
		epoch := ""
		switched := false
		if old != nil {
			epoch = old.Epoch
			changed := old.Group != group || old.Channel != channel
			if changed && !nativeCanSwitch(r.Boundary, *old) {
				// Continuing on the same physical route is safe after authorization even
				// when billing groups change. A different provider is never substituted.
				if old.Channel != channel {
					return nativeRouteError(NativeRouteChangedCode, NativeRouteChangedMessage, http.StatusBadRequest)
				}
			}
			if changed && nativeCanSwitch(r.Boundary, *old) {
				epoch = "native-route-" + nativeDigest([]string{r.Boundary.Key, old.Epoch, r.Boundary.Turn, group, fmt.Sprint(channel)})[:48]
				switched = true
			}
		}
		v := nativeRouteRecord{Retired: retired, Group: group, Channel: channel, Turn: r.Boundary.Turn, State: "inflight", HistoryCount: len(r.Boundary.History), HistoryDigest: nativeDigest(r.Boundary.History), Epoch: epoch, Lease: common.GetUUID(), LeaseUntil: time.Now().Add(15 * time.Minute).Unix(), Expires: time.Now().Add(nativeRouteTTL).Unix()}
		if old != nil && old.Turn != r.Boundary.Turn && !nativeCanSwitch(r.Boundary, *old) {
			r.Deferred = true
			v.Turn, v.HistoryCount, v.HistoryDigest = old.Turn, old.HistoryCount, old.HistoryDigest
		}
		ok, err := writeNativeRoute(r.Boundary.Key, r.Raw, v)
		if err != nil {
			return nativeRegistryError()
		}
		if ok {
			r.Record = &v
			r.Switched = switched
			r.Observer = true
			return nil
		}
		r.Prior, r.Raw, err = readNativeRoute(r.Boundary.Key)
		if err != nil {
			return nativeRegistryError()
		}
	}
	return nativeRegistryError()
}
func NativeResponsesRouteClaimed(c *gin.Context) bool {
	r := nativeRouteRequestFromContext(c)
	return r != nil && r.Record != nil
}
func ApplyNativeResponsesRouteKey(c *gin.Context, raw []byte, outerTools bool) ([]byte, error) {
	r := nativeRouteRequestFromContext(c)
	if r == nil || r.Record == nil || r.Record.Epoch == "" || !outerTools {
		return raw, nil
	}
	return sjson.SetBytes(raw, "prompt_cache_key", r.Record.Epoch)
}
func ObserveNativeResponsesOutcome(c *gin.Context, response *dto.OpenAIResponsesResponse) {
	if response == nil || !NativeResponsesRouteClaimed(c) {
		return
	}
	state := "unknown"
	status := strings.Trim(string(response.Status), "\"")
	if response.Error != nil {
		err := response.GetOpenAIError()
		if err != nil && (fmt.Sprint(err.Code) == "outer_tool_turn_terminated" || gjson.GetBytes(err.Metadata, "original_code").String() == "outer_tool_turn_terminated") {
			state = "failed"
		}
	} else if status == "failed" || status == "cancelled" {
		state = "failed"
	} else if status == "completed" && response.IncompleteDetails == nil {
		state = "completed"
		for _, o := range response.Output {
			if o.Type == "function_call" || o.Type == "custom_tool_call" {
				state = "paused"
				break
			}
		}
	}
	c.Set("native_route_outcome", state)
}
func FinishNativeResponsesRoute(c *gin.Context) {
	r := nativeRouteRequestFromContext(c)
	if r == nil || r.Record == nil || r.Boundary.Compact || !r.Observer {
		return
	}
	old, raw, err := readNativeRoute(r.Boundary.Key)
	if err != nil || old == nil || old.Lease != r.Record.Lease {
		return
	}
	old.State = c.GetString("native_route_outcome")
	if old.State == "" {
		old.State = "unknown"
	}
	if c.Request.Context().Err() != nil {
		old.State = "unknown"
	}
	if r.Deferred && (old.State == "completed" || old.State == "paused" || old.State == "failed") {
		if len(old.Retired) >= 128 {
			old.State = "unknown"
		} else {
			old.Retired = append(old.Retired, old.Turn)
			old.Turn = r.Boundary.Turn
			old.HistoryCount, old.HistoryDigest = len(r.Boundary.History), nativeDigest(r.Boundary.History)
		}
	}
	_, err = writeNativeRoute(r.Boundary.Key, raw, *old)
	if err != nil {
		common.SysError("native route finish state unavailable")
	}
}
func AppendNativeResponsesRouteInfo(c *gin.Context, admin map[string]interface{}) {
	r := nativeRouteRequestFromContext(c)
	if r == nil {
		return
	}
	v := map[string]interface{}{"session_digest": r.Boundary.Key[:16], "turn_digest": r.Boundary.Turn[:16], "safe_new_turn": r.Boundary.Safe, "route_switched": r.Switched}
	if r.Prior != nil {
		v["previous_group"] = r.Prior.Group
		v["previous_channel"] = r.Prior.Channel
		v["previous_state"] = r.Prior.State
	}
	if r.Record != nil {
		v["group"] = r.Record.Group
		v["channel"] = r.Record.Channel
	}
	admin["native_route"] = v
}
func NativeResponsesEffectiveGroup(c *gin.Context) string {
	g := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if g == "auto" {
		g = common.GetContextKeyString(c, constant.ContextKeyAutoGroup)
	}
	return g
}

func LogNativeResponsesRouteRejection(c *gin.Context, code types.ErrorCode) {
	v := map[string]interface{}{"code": code, "request_id": c.GetString(common.RequestIdKey), "group": NativeResponsesEffectiveGroup(c)}
	AppendNativeResponsesRouteInfo(c, v)
	b, _ := common.Marshal(v)
	common.SysLog("native_route_rejected " + string(b))
}
