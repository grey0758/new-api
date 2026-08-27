package dto

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInputTokenDetailsNormalizedCacheWriteTokens(t *testing.T) {
	require.Equal(t, 1200, (InputTokenDetails{CacheWriteTokens: 1200}).NormalizedCacheWriteTokens())
	require.Equal(t, 700, (InputTokenDetails{CachedCreationTokens: 700}).NormalizedCacheWriteTokens())
	require.Equal(t, 1200, (InputTokenDetails{
		CacheWriteTokens:     1200,
		CachedCreationTokens: 700,
	}).NormalizedCacheWriteTokens())
	require.Zero(t, (InputTokenDetails{CacheWriteTokens: -1}).NormalizedCacheWriteTokens())
}
