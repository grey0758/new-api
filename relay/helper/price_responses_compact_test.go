package helper

import (
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResolvePricingModelNameUsesBaseModelForResponsesCompact(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.6-sol" + ratio_setting.CompactModelSuffix,
	}

	pricingModelName, err := resolvePricingModelName(ctx, info)

	require.NoError(t, err)
	require.Equal(t, "gpt-5.6-sol", pricingModelName)
}

func TestResolvePricingModelNameMapsResponsesCompactBaseModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("model_mapping", `{"gpt-5.6-sol":"gpt-5.5"}`)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.6-sol" + ratio_setting.CompactModelSuffix,
	}

	pricingModelName, err := resolvePricingModelName(ctx, info)

	require.NoError(t, err)
	require.Equal(t, "gpt-5.5", pricingModelName)
}

func TestResolvePricingModelNameDoesNotStripCompactSuffixForOrdinaryResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	compactModelName := "gpt-5.6-sol" + ratio_setting.CompactModelSuffix
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: compactModelName,
	}

	pricingModelName, err := resolvePricingModelName(ctx, info)

	require.NoError(t, err)
	require.Equal(t, compactModelName, pricingModelName)
}
