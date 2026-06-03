package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestGetRandomSatisfiedChannelExcludingSkipsFailedCachedChannel(t *testing.T) {
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalGroup2Model2Channels := group2model2channels
	originalChannelsIDM := channelsIDM
	t.Cleanup(func() {
		channelSyncLock.Lock()
		group2model2channels = originalGroup2Model2Channels
		channelsIDM = originalChannelsIDM
		channelSyncLock.Unlock()
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
	})

	priority := int64(10)
	weight := uint(1)
	common.MemoryCacheEnabled = true
	channelSyncLock.Lock()
	group2model2channels = map[string]map[string][]int{
		"default": {
			"gpt-test": {1, 2},
		},
	}
	channelsIDM = map[int]*Channel{
		1: {Id: 1, Priority: &priority, Weight: &weight},
		2: {Id: 2, Priority: &priority, Weight: &weight},
	}
	channelSyncLock.Unlock()

	channel, err := GetRandomSatisfiedChannelExcluding("default", "gpt-test", 0, map[int]bool{1: true})

	require.NoError(t, err)
	require.NotNil(t, channel)
	require.Equal(t, 2, channel.Id)
}

func TestGetRandomSatisfiedChannelExcludingFallsBackAcrossPriorityWhenExcluded(t *testing.T) {
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	originalGroup2Model2Channels := group2model2channels
	originalChannelsIDM := channelsIDM
	t.Cleanup(func() {
		channelSyncLock.Lock()
		group2model2channels = originalGroup2Model2Channels
		channelsIDM = originalChannelsIDM
		channelSyncLock.Unlock()
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
	})

	highPriority := int64(10)
	lowPriority := int64(5)
	weight := uint(1)
	common.MemoryCacheEnabled = true
	channelSyncLock.Lock()
	group2model2channels = map[string]map[string][]int{
		"default": {
			"gpt-test": {1, 2},
		},
	}
	channelsIDM = map[int]*Channel{
		1: {Id: 1, Priority: &highPriority, Weight: &weight},
		2: {Id: 2, Priority: &lowPriority, Weight: &weight},
	}
	channelSyncLock.Unlock()

	channel, err := GetRandomSatisfiedChannelExcluding("default", "gpt-test", 0, map[int]bool{1: true})

	require.NoError(t, err)
	require.NotNil(t, channel)
	require.Equal(t, 2, channel.Id)
}
