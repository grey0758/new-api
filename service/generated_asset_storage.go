package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	generatedAssetDefaultRetentionDays = 30
	generatedAssetDefaultPresignTTL    = 5 * time.Minute
	generatedAssetDefaultMaxBytes      = int64(64 * 1024 * 1024)
)

type generatedAssetStorageConfig struct {
	GatewayEndpoint string
	GatewaySecret   string
	Bucket          string
	Retention       time.Duration
	PresignTTL      time.Duration
	MaxAssetSize    int64
}

type generatedAssetObjectStore interface {
	Put(ctx context.Context, objectKey string, contentType string, body []byte, metadata map[string]string) error
	Get(ctx context.Context, objectKey string) ([]byte, string, error)
	Delete(ctx context.Context, objectKey string) error
	PresignGet(ctx context.Context, objectKey string, ttl time.Duration) (string, error)
}

type generatedAssetGatewayStore struct {
	endpoint     string
	secret       string
	bucket       string
	client       *http.Client
	maxAssetSize int64
}

func newGeneratedAssetGatewayStore(config generatedAssetStorageConfig) (*generatedAssetGatewayStore, error) {
	endpoint, err := url.Parse(config.GatewayEndpoint)
	if err != nil {
		return nil, fmt.Errorf("parse generated asset gateway endpoint: %w", err)
	}
	if endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil ||
		endpoint.RawQuery != "" || endpoint.Fragment != "" ||
		(endpoint.Path != "" && endpoint.Path != "/") {
		return nil, errors.New("generated asset gateway endpoint must be an origin-only https URL")
	}
	if strings.ContainsAny(config.Bucket, "/\\") {
		return nil, errors.New("generated asset bucket name is invalid")
	}
	return &generatedAssetGatewayStore{
		endpoint: strings.TrimRight(endpoint.String(), "/"),
		secret:   config.GatewaySecret,
		bucket:   config.Bucket,
		client: &http.Client{
			Timeout: 2 * time.Minute,
		},
		maxAssetSize: config.MaxAssetSize,
	}, nil
}

func (store *generatedAssetGatewayStore) Put(ctx context.Context, objectKey string, contentType string, body []byte, metadata map[string]string) error {
	if int64(len(body)) > store.maxAssetSize {
		return errors.New("generated asset payload exceeds gateway upload limit")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, store.objectURL(objectKey), bytes.NewReader(body))
	if err != nil {
		return err
	}
	store.authorize(request)
	request.ContentLength = int64(len(body))
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("X-Opencodex-Bucket", store.bucket)
	for key, value := range metadata {
		switch key {
		case "sha256":
			request.Header.Set("X-Opencodex-Meta-SHA256", value)
		case "request-id":
			request.Header.Set("X-Opencodex-Meta-Request-Id", value)
		case "expires-at":
			request.Header.Set("X-Opencodex-Meta-Expires-At", value)
		}
	}
	response, err := store.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("generated asset gateway upload returned HTTP %d", response.StatusCode)
	}
	return nil
}

func (store *generatedAssetGatewayStore) Get(ctx context.Context, objectKey string) ([]byte, string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, store.objectURL(objectKey), nil)
	if err != nil {
		return nil, "", err
	}
	store.authorize(request)
	request.Header.Set("X-Opencodex-Bucket", store.bucket)
	response, err := store.client.Do(request)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil, "", fmt.Errorf("generated asset gateway read returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, store.maxAssetSize+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(data)) > store.maxAssetSize {
		return nil, "", errors.New("stored generated asset exceeds read limit")
	}
	contentType := strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0])
	return data, contentType, nil
}

func (store *generatedAssetGatewayStore) Delete(ctx context.Context, objectKey string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, store.objectURL(objectKey), nil)
	if err != nil {
		return err
	}
	store.authorize(request)
	request.Header.Set("X-Opencodex-Bucket", store.bucket)
	response, err := store.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusNotFound {
		return fmt.Errorf("generated asset gateway delete returned HTTP %d", response.StatusCode)
	}
	return nil
}

func (store *generatedAssetGatewayStore) PresignGet(_ context.Context, objectKey string, ttl time.Duration) (string, error) {
	if ttl < 30*time.Second || ttl > 15*time.Minute {
		return "", errors.New("generated asset signed URL TTL is invalid")
	}
	expiresAt := time.Now().Add(ttl).Unix()
	payload := generatedAssetGatewaySignaturePayload(store.bucket, objectKey, expiresAt)
	mac := hmac.New(sha256.New, []byte(store.secret))
	_, _ = mac.Write([]byte(payload))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	signedURL, err := url.Parse(store.objectURL(objectKey))
	if err != nil {
		return "", err
	}
	query := signedURL.Query()
	query.Set("expires", strconv.FormatInt(expiresAt, 10))
	query.Set("sig", signature)
	signedURL.RawQuery = query.Encode()
	return signedURL.String(), nil
}

func generatedAssetGatewaySignaturePayload(bucket string, objectKey string, expiresAt int64) string {
	return "GET\n" + bucket + "\n" + objectKey + "\n" + strconv.FormatInt(expiresAt, 10)
}

func (store *generatedAssetGatewayStore) objectURL(objectKey string) string {
	segments := strings.Split(objectKey, "/")
	escaped := make([]string, 0, len(segments))
	for _, segment := range segments {
		escaped = append(escaped, url.PathEscape(segment))
	}
	return store.endpoint + "/objects/" + strings.Join(escaped, "/")
}

func (store *generatedAssetGatewayStore) authorize(request *http.Request) {
	request.Header.Set("Authorization", "Bearer "+store.secret)
}

type generatedAssetRuntime struct {
	config generatedAssetStorageConfig
	store  generatedAssetObjectStore
}

var (
	generatedAssetRuntimeOnce  sync.Once
	generatedAssetRuntimeValue *generatedAssetRuntime
	generatedAssetRuntimeErr   error
	generatedAssetRuntimeMu    sync.RWMutex
	generatedAssetRuntimeTest  *generatedAssetRuntime
)

func GeneratedAssetsEnabled() bool {
	enabled, err := strconv.ParseBool(strings.TrimSpace(os.Getenv("OPENCODEX_GENERATED_ASSETS_ENABLED")))
	return err == nil && enabled
}

func getGeneratedAssetRuntime() (*generatedAssetRuntime, error) {
	generatedAssetRuntimeMu.RLock()
	testRuntime := generatedAssetRuntimeTest
	generatedAssetRuntimeMu.RUnlock()
	if testRuntime != nil {
		return testRuntime, nil
	}
	generatedAssetRuntimeOnce.Do(func() {
		config, err := loadGeneratedAssetStorageConfig()
		if err != nil {
			generatedAssetRuntimeErr = err
			return
		}
		store, err := newGeneratedAssetGatewayStore(config)
		if err != nil {
			generatedAssetRuntimeErr = err
			return
		}
		generatedAssetRuntimeValue = &generatedAssetRuntime{config: config, store: store}
	})
	if generatedAssetRuntimeErr != nil {
		return nil, generatedAssetRuntimeErr
	}
	if generatedAssetRuntimeValue == nil {
		return nil, errors.New("generated asset runtime is unavailable")
	}
	return generatedAssetRuntimeValue, nil
}

func loadGeneratedAssetStorageConfig() (generatedAssetStorageConfig, error) {
	if !GeneratedAssetsEnabled() {
		return generatedAssetStorageConfig{}, errors.New("generated asset persistence is disabled")
	}
	config := generatedAssetStorageConfig{
		GatewayEndpoint: strings.TrimSpace(os.Getenv("OPENCODEX_GENERATED_ASSETS_GATEWAY_ENDPOINT")),
		GatewaySecret:   strings.TrimSpace(os.Getenv("OPENCODEX_GENERATED_ASSETS_GATEWAY_SECRET")),
		Bucket:          strings.TrimSpace(os.Getenv("OPENCODEX_GENERATED_ASSETS_R2_BUCKET")),
	}
	if config.GatewayEndpoint == "" || config.GatewaySecret == "" || config.Bucket == "" {
		return generatedAssetStorageConfig{}, errors.New("generated asset gateway configuration is incomplete")
	}

	retentionDays := generatedAssetDefaultRetentionDays
	if value := strings.TrimSpace(os.Getenv("OPENCODEX_GENERATED_ASSETS_RETENTION_DAYS")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 365 {
			return generatedAssetStorageConfig{}, errors.New("generated asset retention days must be between 1 and 365")
		}
		retentionDays = parsed
	}
	config.Retention = time.Duration(retentionDays) * 24 * time.Hour

	presignSeconds := int(generatedAssetDefaultPresignTTL.Seconds())
	if value := strings.TrimSpace(os.Getenv("OPENCODEX_GENERATED_ASSETS_PRESIGN_TTL_SECONDS")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 30 || parsed > 900 {
			return generatedAssetStorageConfig{}, errors.New("generated asset presign TTL must be between 30 and 900 seconds")
		}
		presignSeconds = parsed
	}
	config.PresignTTL = time.Duration(presignSeconds) * time.Second

	config.MaxAssetSize = generatedAssetDefaultMaxBytes
	if value := strings.TrimSpace(os.Getenv("OPENCODEX_GENERATED_ASSETS_MAX_BYTES")); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed < 1024 || parsed > 256*1024*1024 {
			return generatedAssetStorageConfig{}, errors.New("generated asset max bytes is invalid")
		}
		config.MaxAssetSize = parsed
	}
	return config, nil
}

func downloadGeneratedAsset(ctx context.Context, rawURL string, maxBytes int64) ([]byte, string, error) {
	parsed, err := validateGeneratedAssetSourceURL(rawURL)
	if err != nil {
		return nil, "", err
	}
	transport := &http.Transport{
		Proxy:                 nil,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		IdleConnTimeout:       30 * time.Second,
		DialContext:           publicOnlyDialContext,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   2 * time.Minute,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("generated asset source redirected too many times")
			}
			_, err := validateGeneratedAssetSourceURL(request.URL.String())
			return err
		},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, "", err
	}
	request.Header.Set("Accept", "image/png,image/jpeg,image/webp,image/gif")
	response, err := client.Do(request)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, "", fmt.Errorf("generated asset source returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maxBytes {
		return nil, "", errors.New("generated asset source is too large")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(data)) > maxBytes {
		return nil, "", errors.New("generated asset source is too large")
	}
	contentType := strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0])
	return data, contentType, nil
}

func validateGeneratedAssetSourceURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, errors.New("invalid generated asset source URL")
	}
	if parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return nil, errors.New("generated asset source URL must be public https")
	}
	if strings.EqualFold(parsed.Hostname(), "localhost") {
		return nil, errors.New("generated asset source host is not public")
	}
	if ip := net.ParseIP(parsed.Hostname()); ip != nil && !isPublicGeneratedAssetIP(ip) {
		return nil, errors.New("generated asset source IP is not public")
	}
	return parsed, nil
}

func publicOnlyDialContext(ctx context.Context, network string, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return nil, errors.New("generated asset source host has no addresses")
	}
	for _, candidate := range addresses {
		if !isPublicGeneratedAssetIP(candidate.IP) {
			return nil, errors.New("generated asset source resolved to a non-public address")
		}
	}
	dialer := net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	return dialer.DialContext(ctx, network, net.JoinHostPort(addresses[0].IP.String(), port))
}

func isPublicGeneratedAssetIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return false
	}
	blockedCIDRs := []string{
		"100.64.0.0/10",
		"192.0.0.0/24",
		"192.0.2.0/24",
		"198.18.0.0/15",
		"198.51.100.0/24",
		"203.0.113.0/24",
		"2001:db8::/32",
	}
	for _, rawCIDR := range blockedCIDRs {
		_, cidr, _ := net.ParseCIDR(rawCIDR)
		if cidr.Contains(ip) {
			return false
		}
	}
	return true
}

func setGeneratedAssetRuntimeForTest(runtime *generatedAssetRuntime) func() {
	generatedAssetRuntimeMu.Lock()
	previous := generatedAssetRuntimeTest
	generatedAssetRuntimeTest = runtime
	generatedAssetRuntimeMu.Unlock()
	return func() {
		generatedAssetRuntimeMu.Lock()
		generatedAssetRuntimeTest = previous
		generatedAssetRuntimeMu.Unlock()
	}
}
