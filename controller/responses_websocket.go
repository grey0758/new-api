package controller

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const (
	responsesWebSocketMaxLifetime = 60 * time.Minute
	responsesWebSocketReadLimit   = 10 << 20
	responsesWebSocketDefaultLane = "\x00default"
)

var responsesWebSocketUpgrader = websocket.Upgrader{
	ReadBufferSize:  32 << 10,
	WriteBufferSize: 32 << 10,
	CheckOrigin: func(_ *http.Request) bool {
		// Authentication is performed by TokenAuth before the upgrade. The
		// provider is also protected by the private NewAPI ingress boundary.
		return true
	},
}

type responsesWebSocketBillingEntry struct {
	info *relaycommon.RelayInfo
	ctx  *gin.Context
}

type responsesWebSocketBilling struct {
	mu     sync.Mutex
	lanes  map[string][]*responsesWebSocketBillingEntry
	closed bool
}

func newResponsesWebSocketBilling() *responsesWebSocketBilling {
	return &responsesWebSocketBilling{lanes: make(map[string][]*responsesWebSocketBillingEntry)}
}

func responsesWebSocketLaneKey(streamID string) string {
	if streamID == "" {
		return responsesWebSocketDefaultLane
	}
	return streamID
}

func (b *responsesWebSocketBilling) append(lane string, entry *responsesWebSocketBillingEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.lanes[lane] = append(b.lanes[lane], entry)
}

func (b *responsesWebSocketBilling) take(lane string) *responsesWebSocketBillingEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	entries := b.lanes[lane]
	if len(entries) == 0 {
		return nil
	}
	entry := entries[0]
	if len(entries) == 1 {
		delete(b.lanes, lane)
	} else {
		b.lanes[lane] = entries[1:]
	}
	return entry
}

func (b *responsesWebSocketBilling) remove(lane string, target *responsesWebSocketBillingEntry) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	entries := b.lanes[lane]
	for index, entry := range entries {
		if entry != target {
			continue
		}
		entries = append(entries[:index], entries[index+1:]...)
		if len(entries) == 0 {
			delete(b.lanes, lane)
		} else {
			b.lanes[lane] = entries
		}
		return true
	}
	return false
}

func (b *responsesWebSocketBilling) drain() []*responsesWebSocketBillingEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	var result []*responsesWebSocketBillingEntry
	for lane, entries := range b.lanes {
		result = append(result, entries...)
		delete(b.lanes, lane)
	}
	return result
}

func responsesWebSocketError(code, message string, status int, streamID string) map[string]any {
	if status == 0 {
		status = http.StatusBadRequest
	}
	errorType := "invalid_request_error"
	if status >= 500 {
		errorType = "server_error"
	}
	event := map[string]any{
		"type":   "error",
		"status": status,
		"error": map[string]any{
			"type":    errorType,
			"code":    code,
			"message": message,
		},
	}
	if streamID != "" {
		event["stream_id"] = streamID
	}
	return event
}

func responsesWebSocketWriteError(ws *websocket.Conn, writeMu *sync.Mutex, code, message string, status int, streamID string) error {
	event := responsesWebSocketError(code, message, status, streamID)
	writeMu.Lock()
	defer writeMu.Unlock()
	return ws.WriteJSON(event)
}

func responsesWebSocketRequestMap(raw []byte) (map[string]any, error) {
	var payload map[string]any
	if err := common.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	if payload == nil {
		return nil, errors.New("message must be a JSON object")
	}
	return payload, nil
}

func responsesWebSocketStreamID(payload map[string]any) (string, error) {
	value, exists := payload["stream_id"]
	if !exists {
		return "", nil
	}
	streamID, ok := value.(string)
	if !ok || strings.TrimSpace(streamID) == "" || len(streamID) > 256 {
		return "", errors.New("stream_id must be 1-256 characters")
	}
	for _, r := range streamID {
		if !(r == '_' || r == '-' || r == '.' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return "", errors.New("stream_id contains unsupported characters")
		}
	}
	return streamID, nil
}

func responsesWebSocketBool(payload map[string]any, field string, defaultValue bool) (bool, error) {
	value, exists := payload[field]
	if !exists {
		return defaultValue, nil
	}
	parsed, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("%s must be a boolean", field)
	}
	return parsed, nil
}

func responsesWebSocketModelAllowed(c *gin.Context, modelName string) bool {
	if !common.GetContextKeyBool(c, constant.ContextKeyTokenModelLimitEnabled) {
		return true
	}
	value, ok := common.GetContextKey(c, constant.ContextKeyTokenModelLimit)
	if !ok {
		return false
	}
	limits, ok := value.(map[string]bool)
	return ok && limits[modelName]
}

func responsesWebSocketSelectChannel(c *gin.Context, modelName string) (*model.Channel, error) {
	if !responsesWebSocketModelAllowed(c, modelName) {
		return nil, fmt.Errorf("model %s is not allowed for this token", modelName)
	}

	if specific, ok := c.Get("specific_channel_id"); ok {
		var channelID int
		if _, err := fmt.Sscanf(fmt.Sprint(specific), "%d", &channelID); err == nil {
			channel, getErr := model.GetChannelById(channelID, true)
			if getErr != nil {
				return nil, getErr
			}
			if channel.Status != common.ChannelStatusEnabled {
				return nil, errors.New("specified channel is disabled")
			}
			if err := middleware.SetupContextForSelectedChannel(c, channel, modelName); err != nil {
				return nil, err
			}
			return channel, nil
		}
	}

	usingGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	param := &service.RetryParam{
		Ctx:        c,
		TokenGroup: usingGroup,
		ModelName:  modelName,
		Retry:      common.GetPointer(0),
	}
	channel, selectedGroup, err := service.CacheGetRandomSatisfiedChannel(param)
	if err != nil {
		return nil, err
	}
	if channel == nil {
		return nil, fmt.Errorf("no available channel for model %s in group %s", modelName, usingGroup)
	}
	// CacheGetRandomSatisfiedChannel may resolve an automatic group that differs
	// from the token's initial group. Set it before SetupContextForSelectedChannel
	// so channel metadata, pricing, and billing all observe the selected group.
	if selectedGroup != "" && selectedGroup != usingGroup {
		common.SetContextKey(c, constant.ContextKeyUsingGroup, selectedGroup)
	}
	if err := middleware.SetupContextForSelectedChannel(c, channel, modelName); err != nil {
		return nil, err
	}
	return channel, nil
}

func responsesWebSocketUpstreamURL(info *relaycommon.RelayInfo) (string, error) {
	adaptor := relay.GetAdaptor(info.ApiType)
	if adaptor == nil {
		return "", errors.New("invalid channel API type")
	}
	adaptor.Init(info)
	requestURL, err := adaptor.GetRequestURL(info)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(requestURL)
	if err != nil {
		return "", err
	}
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("unsupported upstream URL scheme %q", parsed.Scheme)
	}
	return parsed.String(), nil
}

func responsesWebSocketCopyMetadataHeaders(src http.Header, dst http.Header) {
	for name, values := range src {
		lower := strings.ToLower(name)
		if lower == "authorization" || lower == "host" || lower == "connection" || lower == "upgrade" ||
			lower == "content-length" || strings.HasPrefix(lower, "sec-websocket-") {
			continue
		}
		if strings.HasPrefix(lower, "openai-") || strings.HasPrefix(lower, "x-openai-") ||
			strings.HasPrefix(lower, "x-stainless-") || lower == "user-agent" || lower == "traceparent" {
			dst[name] = append([]string(nil), values...)
		}
	}
}

func responsesWebSocketDialUpstream(c *gin.Context, info *relaycommon.RelayInfo) (*websocket.Conn, error) {
	adaptor := relay.GetAdaptor(info.ApiType)
	if adaptor == nil {
		return nil, errors.New("invalid channel API type")
	}
	urlValue, err := responsesWebSocketUpstreamURL(info)
	if err != nil {
		return nil, err
	}
	header := http.Header{}
	if err := adaptor.SetupRequestHeader(c, &header, info); err != nil {
		return nil, err
	}
	responsesWebSocketCopyMetadataHeaders(c.Request.Header, header)
	dialer := responsesWebSocketDialer()
	conn, _, err := dialer.Dial(urlValue, header)
	if err != nil {
		return nil, fmt.Errorf("dial responses upstream: %w", err)
	}
	conn.SetReadLimit(responsesWebSocketReadLimit)
	return conn, nil
}

func responsesWebSocketDialer() websocket.Dialer {
	// DefaultDialer is a package-global pointer. Copy it before setting the
	// request-specific handshake timeout so concurrent upgrades do not race.
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 20 * time.Second
	return dialer
}

func responsesWebSocketCloneContext(parent *gin.Context, raw []byte, requestID string) *gin.Context {
	ctx := parent.Copy()
	request := parent.Request.Clone(parent.Request.Context())
	request.Body = http.NoBody
	request.ContentLength = int64(len(raw))
	ctx.Request = request
	ctx.Set(common.RequestIdKey, requestID)
	ctx.Set(string(constant.ContextKeyRequestStartTime), time.Now())
	return ctx
}

func responsesWebSocketBuildBilling(parent *gin.Context, raw []byte, modelName string, requestID string) (*responsesWebSocketBillingEntry, error) {
	payload, err := responsesWebSocketRequestMap(raw)
	if err != nil {
		return nil, err
	}
	if payload["type"] != "response.create" {
		return nil, errors.New("only response.create events are supported")
	}
	if supplied, ok := payload["model"].(string); ok && supplied != "" && supplied != modelName {
		return nil, fmt.Errorf("model must remain %s for this connection", modelName)
	}
	payload["model"] = modelName
	normalized, err := common.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var request dto.OpenAIResponsesRequest
	if err := common.Unmarshal(normalized, &request); err != nil {
		return nil, err
	}
	stream := true
	request.Stream = &stream
	ctx := responsesWebSocketCloneContext(parent, normalized, requestID)
	common.SetContextKey(ctx, constant.ContextKeyOriginalModel, modelName)
	info := relaycommon.GenRelayInfoResponses(ctx, &request)
	info.RelayMode = relayconstant.RelayModeResponses
	info.IsStream = true
	info.RequestURLPath = "/v1/responses"
	info.InitChannelMeta(ctx)
	meta := request.GetTokenCountMeta()
	tokens, err := service.EstimateRequestToken(ctx, meta, info)
	if err != nil {
		return nil, err
	}
	info.SetEstimatePromptTokens(tokens)
	priceData, err := helper.ModelPriceHelper(ctx, info, tokens, meta)
	if err != nil {
		return nil, err
	}
	if !priceData.FreeModel {
		if apiErr := service.PreConsumeBilling(ctx, priceData.QuotaToPreConsume, info); apiErr != nil {
			return nil, errors.New(apiErr.Error())
		}
	}
	return &responsesWebSocketBillingEntry{info: info, ctx: ctx}, nil
}

func responsesWebSocketUsage(event map[string]any) *dto.Usage {
	response, ok := event["response"].(map[string]any)
	if !ok {
		return nil
	}
	usageValue, ok := response["usage"].(map[string]any)
	if !ok {
		return nil
	}
	usage := &dto.Usage{
		PromptTokens:     intFromAny(usageValue["input_tokens"]),
		CompletionTokens: intFromAny(usageValue["output_tokens"]),
		TotalTokens:      intFromAny(usageValue["total_tokens"]),
		InputTokens:      intFromAny(usageValue["input_tokens"]),
		OutputTokens:     intFromAny(usageValue["output_tokens"]),
		UsageSource:      "responses_websocket",
	}
	if details, ok := usageValue["input_tokens_details"].(map[string]any); ok {
		usage.PromptTokensDetails.CachedTokens = intFromAny(details["cached_tokens"])
		usage.PromptTokensDetails.CachedCreationTokens = intFromAny(details["cached_creation_tokens"])
		usage.PromptTokensDetails.CacheWriteTokens = intFromAny(details["cache_write_tokens"])
		if usage.PromptTokensDetails.CachedCreationTokens == 0 {
			usage.PromptTokensDetails.CachedCreationTokens = usage.PromptTokensDetails.CacheWriteTokens
		}
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	if usage.TotalTokens == 0 {
		return nil
	}
	return usage
}

func intFromAny(value any) int {
	switch parsed := value.(type) {
	case float64:
		return int(parsed)
	case int:
		return parsed
	case int64:
		return int(parsed)
	default:
		return 0
	}
}

func responsesWebSocketTerminal(eventType string) bool {
	switch eventType {
	case "response.completed", "response.failed", "response.incomplete", "response.cancelled":
		return true
	default:
		return false
	}
}

func responsesWebSocketSettle(entry *responsesWebSocketBillingEntry, event map[string]any) {
	if entry == nil || entry.info == nil {
		return
	}
	usage := responsesWebSocketUsage(event)
	if usage == nil {
		if entry.info.Billing != nil {
			entry.info.Billing.Refund(entry.ctx)
		}
		return
	}
	service.PostTextConsumeQuota(entry.ctx, entry.info, usage, nil)
}

// RelayResponsesWebSocket exposes the OpenAI Responses WebSocket protocol.
// It deliberately performs channel selection after the first response.create
// frame, because the HTTP upgrade request has no model in its query string.
func RelayResponsesWebSocket(c *gin.Context) {
	cws, err := responsesWebSocketUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer cws.Close()
	cws.SetReadLimit(responsesWebSocketReadLimit)
	cws.SetReadDeadline(time.Now().Add(responsesWebSocketMaxLifetime))

	writeMu := &sync.Mutex{}
	messageType, firstRaw, err := cws.ReadMessage()
	if err != nil {
		return
	}
	if messageType != websocket.TextMessage {
		_ = responsesWebSocketWriteError(cws, writeMu, "invalid_event", "response.create must be a text JSON event", http.StatusBadRequest, "")
		return
	}
	firstPayload, err := responsesWebSocketRequestMap(firstRaw)
	if err != nil || firstPayload["type"] != "response.create" {
		message := "only response.create events are supported"
		if err != nil {
			message = "message must be valid JSON"
		}
		_ = responsesWebSocketWriteError(cws, writeMu, "invalid_event", message, http.StatusBadRequest, "")
		return
	}
	firstStreamID, err := responsesWebSocketStreamID(firstPayload)
	if err != nil {
		_ = responsesWebSocketWriteError(cws, writeMu, "invalid_stream_id", err.Error(), http.StatusBadRequest, "")
		return
	}
	modelName, ok := firstPayload["model"].(string)
	if !ok || strings.TrimSpace(modelName) == "" {
		_ = responsesWebSocketWriteError(cws, writeMu, "model_required", "the first response.create event must include model", http.StatusBadRequest, firstStreamID)
		return
	}
	if !responsesWebSocketModelAllowed(c, modelName) {
		_ = responsesWebSocketWriteError(cws, writeMu, "model_not_allowed", "model is not allowed for this token", http.StatusForbidden, firstStreamID)
		return
	}
	channel, err := responsesWebSocketSelectChannel(c, modelName)
	if err != nil {
		_ = responsesWebSocketWriteError(cws, writeMu, "channel_unavailable", err.Error(), http.StatusServiceUnavailable, firstStreamID)
		return
	}
	base := c.Copy()
	base.Request = c.Request.Clone(c.Request.Context())
	common.SetContextKey(base, constant.ContextKeyOriginalModel, modelName)
	common.SetContextKey(base, constant.ContextKeyRequestStartTime, time.Now())
	if channel == nil {
		_ = responsesWebSocketWriteError(cws, writeMu, "channel_unavailable", "channel selection failed", http.StatusServiceUnavailable, firstStreamID)
		return
	}
	upstreamInfo := relaycommon.GenRelayInfoResponses(base, &dto.OpenAIResponsesRequest{Model: modelName})
	upstreamInfo.RelayMode = relayconstant.RelayModeResponses
	upstreamInfo.IsStream = true
	upstreamInfo.RequestURLPath = "/v1/responses"
	upstreamInfo.InitChannelMeta(base)
	upstream, err := responsesWebSocketDialUpstream(base, upstreamInfo)
	if err != nil {
		_ = responsesWebSocketWriteError(cws, writeMu, "upstream_unavailable", "responses upstream is unavailable", http.StatusServiceUnavailable, firstStreamID)
		return
	}
	defer upstream.Close()
	upstreamWriteMu := &sync.Mutex{}
	upstream.SetReadDeadline(time.Now().Add(responsesWebSocketMaxLifetime))

	billing := newResponsesWebSocketBilling()
	defer func() {
		for _, entry := range billing.drain() {
			if entry != nil && entry.info != nil && entry.info.Billing != nil {
				entry.info.Billing.Refund(entry.ctx)
			}
		}
	}()

	forwardCreate := func(raw []byte, payload map[string]any, streamID string) error {
		payload["model"] = modelName
		normalizedRaw, err := common.Marshal(payload)
		if err != nil {
			return responsesWebSocketWriteError(cws, writeMu, "invalid_event", "response.create could not be encoded", http.StatusBadRequest, streamID)
		}
		generate, err := responsesWebSocketBool(payload, "generate", true)
		if err != nil {
			return responsesWebSocketWriteError(cws, writeMu, "invalid_generate", err.Error(), http.StatusBadRequest, streamID)
		}
		// A non-generating warmup still occupies a FIFO position, so give every
		// response.create a unique queue entry even when no billing is prepared.
		entry := &responsesWebSocketBillingEntry{}
		if generate {
			requestID := common.GetTimeString() + common.GetRandomString(8)
			entry, err = responsesWebSocketBuildBilling(base, normalizedRaw, modelName, requestID)
			if err != nil {
				return responsesWebSocketWriteError(cws, writeMu, "billing_prepare_failed", "unable to prepare response billing", http.StatusBadRequest, streamID)
			}
		}
		billing.append(responsesWebSocketLaneKey(streamID), entry)
		upstreamWriteMu.Lock()
		defer upstreamWriteMu.Unlock()
		if err := upstream.WriteMessage(websocket.TextMessage, normalizedRaw); err != nil {
			// Remove exactly this request before refunding. Leaving it queued would
			// let the connection drain refund the same pre-consumption a second time.
			if billing.remove(responsesWebSocketLaneKey(streamID), entry) &&
				entry.info != nil && entry.info.Billing != nil {
				entry.info.Billing.Refund(entry.ctx)
			}
			return err
		}
		return nil
	}

	if err := forwardCreate(firstRaw, firstPayload, firstStreamID); err != nil {
		return
	}
	var once sync.Once
	closeBoth := func() {
		once.Do(func() {
			_ = cws.Close()
			_ = upstream.Close()
		})
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			messageType, raw, readErr := cws.ReadMessage()
			if readErr != nil {
				closeBoth()
				return
			}
			if messageType == websocket.CloseMessage {
				closeBoth()
				return
			}
			if messageType != websocket.TextMessage {
				_ = responsesWebSocketWriteError(cws, writeMu, "invalid_event", "only text JSON events are supported", http.StatusBadRequest, "")
				continue
			}
			payload, parseErr := responsesWebSocketRequestMap(raw)
			if parseErr != nil {
				_ = responsesWebSocketWriteError(cws, writeMu, "invalid_json", "message must be valid JSON", http.StatusBadRequest, "")
				continue
			}
			if payload["type"] != "response.create" {
				_ = responsesWebSocketWriteError(cws, writeMu, "invalid_event", "only response.create events are supported", http.StatusBadRequest, "")
				continue
			}
			streamID, streamErr := responsesWebSocketStreamID(payload)
			if streamErr != nil {
				_ = responsesWebSocketWriteError(cws, writeMu, "invalid_stream_id", streamErr.Error(), http.StatusBadRequest, "")
				continue
			}
			if supplied, exists := payload["model"]; exists {
				if value, ok := supplied.(string); !ok || value != modelName {
					_ = responsesWebSocketWriteError(cws, writeMu, "model_mismatch", "model must remain fixed for this connection", http.StatusBadRequest, streamID)
					continue
				}
			}
			if err := forwardCreate(raw, payload, streamID); err != nil {
				closeBoth()
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for {
			messageType, raw, readErr := upstream.ReadMessage()
			if readErr != nil {
				closeBoth()
				return
			}
			writeMu.Lock()
			writeErr := cws.WriteMessage(messageType, raw)
			writeMu.Unlock()
			if writeErr != nil {
				closeBoth()
				return
			}
			if messageType != websocket.TextMessage {
				continue
			}
			event, parseErr := responsesWebSocketRequestMap(raw)
			if parseErr != nil {
				continue
			}
			eventType, _ := event["type"].(string)
			if !responsesWebSocketTerminal(eventType) {
				continue
			}
			streamID, _ := event["stream_id"].(string)
			entry := billing.take(responsesWebSocketLaneKey(streamID))
			responsesWebSocketSettle(entry, event)
		}
	}()
	wg.Wait()
}
