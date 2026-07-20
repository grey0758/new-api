package common

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestTaskSubmitReqAcceptsGrokImageObjects(t *testing.T) {
	var req TaskSubmitReq
	err := json.Unmarshal([]byte(`{
		"model":"seedance-2.0-fast-10s",
		"prompt":"test",
		"image":{"url":"https://cdn.example.com/first.jpg"},
		"images":[{"url":"https://cdn.example.com/second.jpg"},{"url":"https://cdn.example.com/third.jpg"}],
		"duration":"10",
		"resolution":"720p",
		"aspect_ratio":"16:9"
	}`), &req)

	require.NoError(t, err)
	require.Equal(t, "https://cdn.example.com/first.jpg", req.Image)
	require.Equal(t, []string{"https://cdn.example.com/second.jpg", "https://cdn.example.com/third.jpg"}, req.Images)
	require.Equal(t, 10, req.Duration)
	require.Equal(t, "720p", req.Resolution)
	require.Equal(t, "16:9", req.AspectRatio)
	require.True(t, req.HasImage())
}

func TestTaskSubmitReqKeepsStringImageCompatibility(t *testing.T) {
	var req TaskSubmitReq
	require.NoError(t, json.Unmarshal([]byte(`{"image":"https://cdn.example.com/first.jpg","images":["https://cdn.example.com/second.jpg"]}`), &req))
	require.Equal(t, "https://cdn.example.com/first.jpg", req.Image)
	require.Equal(t, []string{"https://cdn.example.com/second.jpg"}, req.Images)
}

func TestRelayInfoGetFinalRequestRelayFormatPrefersExplicitFinal(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:             types.RelayFormatOpenAI,
		RequestConversionChain:  []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
		FinalRequestRelayFormat: types.RelayFormatOpenAIResponses,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToConversionChain(t *testing.T) {
	info := &RelayInfo{
		RelayFormat:            types.RelayFormatOpenAI,
		RequestConversionChain: []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude},
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatClaude), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatFallsBackToRelayFormat(t *testing.T) {
	info := &RelayInfo{
		RelayFormat: types.RelayFormatGemini,
	}

	require.Equal(t, types.RelayFormat(types.RelayFormatGemini), info.GetFinalRequestRelayFormat())
}

func TestRelayInfoGetFinalRequestRelayFormatNilReceiver(t *testing.T) {
	var info *RelayInfo
	require.Equal(t, types.RelayFormat(""), info.GetFinalRequestRelayFormat())
}
