package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const ImageJobMaxRequestBytes = 8 << 20
const ImageJobMaxResultBytes = 48 << 20

type imageJobContextKey struct{}
type ImageJobExecution struct {
	ID, Owner, RequestID string
	ResultObserved       bool
	CheckpointFailed     bool
	FinalCode            string
	ProviderRejected     bool
}

func WithImageJob(ctx context.Context, execution *ImageJobExecution) context.Context {
	return context.WithValue(ctx, imageJobContextKey{}, execution)
}
func ImageJobExecutionFrom(ctx context.Context) *ImageJobExecution {
	v, _ := ctx.Value(imageJobContextKey{}).(*ImageJobExecution)
	return v
}
func IsImageJob(c *gin.Context) bool {
	return c != nil && c.Request != nil && ImageJobExecutionFrom(c.Request.Context()) != nil
}
func ImageJobsEnabled() bool { return strings.EqualFold(os.Getenv("IMAGE_JOBS_ENABLED"), "true") }
func ImageJobDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func imageJobCheckpoint(c *gin.Context, values map[string]any) error {
	if !IsImageJob(c) {
		return nil
	}
	execution := ImageJobExecutionFrom(c.Request.Context())
	var err error
	for i := 0; i < 3; i++ {
		if err = model.UpdateOwnedImageJob(execution.ID, execution.Owner, values); err == nil {
			return nil
		}
		if errors.Is(err, model.ErrImageJobLost) {
			break
		}
		time.Sleep(time.Duration(i+1) * 50 * time.Millisecond)
	}
	execution.CheckpointFailed = true
	return err
}

func BeforeImageJobSubmit(c *gin.Context) error {
	return imageJobCheckpoint(c, map[string]any{"provider_started": true})
}
func CheckpointImageJobBilling(c *gin.Context, state string) error {
	return imageJobCheckpoint(c, map[string]any{"billing_status": state})
}

func PersistImageJobResult(c *gin.Context, body []byte) error {
	if !IsImageJob(c) {
		return nil
	}
	execution := ImageJobExecutionFrom(c.Request.Context())
	execution.ResultObserved = true
	var result dto.ImageResponse
	if len(body) > ImageJobMaxResultBytes || common.Unmarshal(body, &result) != nil || len(result.Data) == 0 {
		execution.CheckpointFailed = true
		return errors.New("image job response cannot be checkpointed")
	}
	// Signed upstream URLs are not permanent. First version conservatively
	// exposes a one-hour retrieval window; inline responses use the same TTL.
	// No refetch/re-upload/regeneration occurs here.
	expires := time.Now().Add(time.Hour).Unix()
	for _, image := range result.Data {
		parsed, err := url.Parse(image.Url)
		if err != nil {
			continue
		}
		query := parsed.Query()
		for _, key := range []string{"exp", "expires", "Expires"} {
			if value, err := strconv.ParseInt(query.Get(key), 10, 64); err == nil && value > 0 && value < expires {
				expires = value
			}
		}
		if start, err := time.Parse("20060102T150405Z", query.Get("X-Amz-Date")); err == nil {
			if seconds, err := strconv.ParseInt(query.Get("X-Amz-Expires"), 10, 64); err == nil && seconds > 0 && start.Unix()+seconds < expires {
				expires = start.Unix() + seconds
			}
		}
	}
	var err error
	for i := 0; i < 3; i++ {
		err = model.CheckpointImageJobResult(execution.ID, execution.Owner, ImageJobDigest(body), body, expires)
		if err == nil {
			return nil
		}
		if errors.Is(err, model.ErrImageJobConflict) {
			break
		}
		time.Sleep(time.Duration(i+1) * 50 * time.Millisecond)
	}
	execution.CheckpointFailed = true
	return err
}
