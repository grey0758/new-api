package openai

import (
	"io"
	"strings"
	"testing"

	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/sjson"
)

func TestKrillHistoryCompatibilityMigratedEndpoint(t *testing.T) {
	for _, base := range []string{
		"https://api.krill-ai.net/codex",
		"https://krill-ai.net/codex",
		"https://api.kbsfdxc.net/codex",
		"https://API.KBSFDXC.NET/codex/",
	} {
		t.Run(base, func(t *testing.T) {
			info := krillHistoryInfo()
			info.ChannelBaseUrl = base
			for _, fixture := range []struct {
				body string
				path string
			}{
				{customHistoryFixture, "input.1.id"},
				{genericMessageHistoryFixture, "input.0.id"},
			} {
				prepared, err := PrepareResponsesHistoryBody(info, strings.NewReader(fixture.body))
				require.NoError(t, err)
				got, err := io.ReadAll(prepared)
				require.NoError(t, err)
				want, err := sjson.Delete(fixture.body, fixture.path)
				require.NoError(t, err)
				require.Equal(t, want, string(got), "preserve all fields except the optional history ID")
			}
			info.ChannelId = 47
			require.True(t, usesChannel47ImageCompatibility(info))
			info.ChannelId = 48
			require.False(t, usesChannel47ImageCompatibility(info))
		})
	}
}

func TestKrillHistoryCompatibilityMigratedEndpointBoundaries(t *testing.T) {
	for _, base := range []string{
		"https://kbsfdxc.net/codex",
		"https://api-slb.kbsfdxc.net/backend-api/codex",
		"https://api.kbsfdxc.net.evil.test/codex",
		"https://evil.test/api.kbsfdxc.net/codex",
		"https://api.kbsfdxc.net/v1",
		"https://api.kbsfdxc.net/codex/v1",
		"https://api.kbsfdxc.net/codex?route=other",
		"https://api.kbsfdxc.net/codex#other",
		"https://user@api.kbsfdxc.net/codex",
		"ftp://api.kbsfdxc.net/codex",
	} {
		t.Run(base, func(t *testing.T) {
			info := krillHistoryInfo()
			info.ChannelBaseUrl = base
			reader := strings.NewReader(genericMessageHistoryFixture)
			got, err := PrepareResponsesHistoryBody(info, reader)
			require.NoError(t, err)
			require.Same(t, reader, got)
		})
	}
	info := krillHistoryInfo()
	info.ChannelBaseUrl = "https://api.kbsfdxc.net/codex"
	info.RelayMode = relayconstant.RelayModeResponsesCompact
	require.False(t, usesKrillCodexHistoryCompatibility(info))
	info.RelayMode = relayconstant.RelayModeResponses
	for _, body := range []string{
		strings.Replace(genericMessageHistoryFixture, `"store":false`, `"store":true`, 1),
		strings.Replace(genericMessageHistoryFixture, `"store":false,`, "", 1),
		strings.Replace(genericMessageHistoryFixture, `"store":false`, `"store":false,"previous_response_id":"resp_previous"`, 1),
		strings.Replace(genericMessageHistoryFixture, `"store":false`, `"store":false,"conversation":"conv_previous"`, 1),
	} {
		prepared, err := PrepareResponsesHistoryBody(info, strings.NewReader(body))
		require.NoError(t, err)
		got, err := io.ReadAll(prepared)
		require.NoError(t, err)
		require.Equal(t, body, string(got))
	}
}
