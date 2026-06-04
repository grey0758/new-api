package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/cachex"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/samber/hot"
)

const (
	responsesStreamFailoverNamespace = "new-api:responses_stream_failover:v1"
	responsesStreamFailoverTTL       = 5 * time.Minute
)

var (
	responsesStreamFailoverOnce  sync.Once
	responsesStreamFailoverCache *cachex.HybridCache[map[int]bool]
)

func getResponsesStreamFailoverCache() *cachex.HybridCache[map[int]bool] {
	responsesStreamFailoverOnce.Do(func() {
		responsesStreamFailoverCache = cachex.NewHybridCache[map[int]bool](cachex.HybridCacheConfig[map[int]bool]{
			Namespace:    cachex.Namespace(responsesStreamFailoverNamespace),
			Redis:        common.RDB,
			RedisEnabled: func() bool { return common.RedisEnabled },
			RedisCodec:   cachex.JSONCodec[map[int]bool]{},
			Memory: func() *hot.HotCache[string, map[int]bool] {
				return hot.NewHotCache[string, map[int]bool](hot.LRU, 4096).Build()
			},
		})
	})
	return responsesStreamFailoverCache
}

func IsResponsesStreamIncompleteError(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	return err.GetErrorCode() == types.ErrorCodeResponsesStreamIncomplete
}

func ResponsesStreamFailoverKeyFromContext(c *gin.Context, modelName string) string {
	if c == nil || c.Request == nil {
		return ""
	}
	if c.Request.Method != http.MethodPost {
		return ""
	}
	path := ""
	if c.Request.URL != nil {
		path = c.Request.URL.Path
	}
	if path != "/v1/responses" {
		return ""
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		common.SysError(fmt.Sprintf("responses stream failover body read failed: %v", err))
		return ""
	}
	body, err := storage.Bytes()
	if err != nil {
		common.SysError(fmt.Sprintf("responses stream failover body bytes failed: %v", err))
		return ""
	}
	tokenID := c.GetInt("token_id")
	userID := c.GetInt("id")
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%s:%s:%s", userID, tokenID, modelName, path, string(body))))
	return hex.EncodeToString(sum[:])
}

func GetResponsesStreamFailoverExcludedChannels(c *gin.Context, modelName string) map[int]bool {
	key := ResponsesStreamFailoverKeyFromContext(c, modelName)
	if key == "" {
		return nil
	}
	excluded, found, err := getResponsesStreamFailoverCache().Get(key)
	if err != nil {
		common.SysError(fmt.Sprintf("responses stream failover cache get failed: key=%s err=%v", key, err))
		return nil
	}
	if !found || len(excluded) == 0 {
		return nil
	}
	c.Set("responses_stream_failover_key", key)
	result := make(map[int]bool, len(excluded))
	for channelID, skip := range excluded {
		if skip && channelID > 0 {
			result[channelID] = true
		}
	}
	return result
}

func ShouldSkipChannelForResponsesStreamFailover(c *gin.Context, modelName string, channelID int) bool {
	if channelID <= 0 {
		return false
	}
	excluded := GetResponsesStreamFailoverExcludedChannels(c, modelName)
	return excluded != nil && excluded[channelID]
}

func RecordResponsesStreamFailover(c *gin.Context, modelName string, channelID int) {
	if channelID <= 0 {
		return
	}
	key := ResponsesStreamFailoverKeyFromContext(c, modelName)
	if key == "" {
		return
	}
	excluded := map[int]bool{channelID: true}
	if current, found, err := getResponsesStreamFailoverCache().Get(key); err == nil && found {
		for id, skip := range current {
			if skip && id > 0 {
				excluded[id] = true
			}
		}
	}
	if err := getResponsesStreamFailoverCache().SetWithTTL(key, excluded, responsesStreamFailoverTTL); err != nil {
		common.SysError(fmt.Sprintf("responses stream failover cache set failed: key=%s channel=%d err=%v", key, channelID, err))
		return
	}
	c.Set("responses_stream_failover_key", key)
	c.Set("responses_stream_failover_channel_id", channelID)
	c.Set("responses_stream_incomplete_seen", true)
}

func HasResponsesStreamIncomplete(c *gin.Context) bool {
	if c == nil {
		return false
	}
	return c.GetBool("responses_stream_incomplete_seen")
}

func ClearResponsesStreamFailover(c *gin.Context, modelName string) {
	key := ""
	if c != nil {
		if v, ok := c.Get("responses_stream_failover_key"); ok {
			key, _ = v.(string)
		}
	}
	if strings.TrimSpace(key) == "" {
		key = ResponsesStreamFailoverKeyFromContext(c, modelName)
	}
	if strings.TrimSpace(key) == "" {
		return
	}
	if _, err := getResponsesStreamFailoverCache().DeleteMany([]string{key}); err != nil {
		common.SysError(fmt.Sprintf("responses stream failover cache delete failed: key=%s err=%v", key, err))
	}
}
