package dex

import (
	"context"
	"testing"
	"time"

	"unified-tx-parser/internal/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBaseDexExtractor_NilConfig(t *testing.T) {
	ext := NewBaseDexExtractor(nil)
	require.NotNil(t, ext)
	assert.Empty(t, ext.protocols)
	assert.Empty(t, ext.supportedChains)
	assert.NotNil(t, ext.quoteAssets)
	assert.NotNil(t, ext.log)
}

func TestNewBaseDexExtractor_WithConfig(t *testing.T) {
	cfg := &BaseDexExtractorConfig{
		Protocols:        []string{"test-dex", "test-dex-v2"},
		SupportedChains:  []types.ChainType{types.ChainTypeBSC, types.ChainTypeEthereum},
		QuoteAssets:      map[string]int{"0xUSDT": 100, "0xWBNB": 50},
		EnableTokenCache: true,
		EnablePoolCache:  true,
		TokenCacheTTL:    5 * time.Minute,
		PoolCacheTTL:     10 * time.Minute,
		LoggerModuleName: "test-module",
	}

	ext := NewBaseDexExtractor(cfg)
	require.NotNil(t, ext)
	assert.Equal(t, cfg.Protocols, ext.protocols)
	assert.Equal(t, cfg.SupportedChains, ext.supportedChains)
	assert.Equal(t, cfg.QuoteAssets, ext.quoteAssets)
	assert.True(t, ext.enableTokenCache)
	assert.True(t, ext.enablePoolCache)
	assert.Equal(t, 5*time.Minute, ext.tokenCacheTTL)
	assert.Equal(t, 10*time.Minute, ext.poolCacheTTL)
}

func TestNewBaseDexExtractor_DefaultLoggerModule(t *testing.T) {
	cfg := &BaseDexExtractorConfig{
		LoggerModuleName: "",
	}
	ext := NewBaseDexExtractor(cfg)
	require.NotNil(t, ext.log)
	// Default module name is "dex-extractor"
	assert.Equal(t, "dex-extractor", ext.log.Data["module"])
}

func TestNewBaseDexExtractor_CustomLoggerModule(t *testing.T) {
	cfg := &BaseDexExtractorConfig{
		LoggerModuleName: "my-custom-dex",
	}
	ext := NewBaseDexExtractor(cfg)
	assert.Equal(t, "my-custom-dex", ext.log.Data["module"])
}

func TestBaseDexExtractor_GetSupportedProtocols(t *testing.T) {
	cfg := &BaseDexExtractorConfig{
		Protocols: []string{"proto1", "proto2"},
	}
	ext := NewBaseDexExtractor(cfg)
	assert.Equal(t, []string{"proto1", "proto2"}, ext.GetSupportedProtocols())
}

func TestBaseDexExtractor_GetSupportedProtocols_Empty(t *testing.T) {
	ext := NewBaseDexExtractor(nil)
	assert.Nil(t, ext.GetSupportedProtocols())
}

func TestBaseDexExtractor_GetSupportedChains(t *testing.T) {
	cfg := &BaseDexExtractorConfig{
		SupportedChains: []types.ChainType{types.ChainTypeBSC},
	}
	ext := NewBaseDexExtractor(cfg)
	assert.Equal(t, []types.ChainType{types.ChainTypeBSC}, ext.GetSupportedChains())
}

func TestBaseDexExtractor_SetQuoteAssets(t *testing.T) {
	ext := NewBaseDexExtractor(nil)

	assets := map[string]int{"0xUSDT": 100, "0xWBNB": 50}
	ext.SetQuoteAssets(assets)
	assert.Equal(t, assets, ext.GetQuoteAssets())
}

func TestBaseDexExtractor_SetQuoteAssets_EmptyDoesNotOverwrite(t *testing.T) {
	cfg := &BaseDexExtractorConfig{
		QuoteAssets: map[string]int{"0xUSDT": 100},
	}
	ext := NewBaseDexExtractor(cfg)

	ext.SetQuoteAssets(map[string]int{})
	assert.Equal(t, map[string]int{"0xUSDT": 100}, ext.GetQuoteAssets())
}

func TestBaseDexExtractor_GetQuoteAssets_ReturnsCopy(t *testing.T) {
	cfg := &BaseDexExtractorConfig{
		QuoteAssets: map[string]int{"0xUSDT": 100},
	}
	ext := NewBaseDexExtractor(cfg)

	copy1 := ext.GetQuoteAssets()
	copy1["0xNew"] = 50

	// Original should not be modified
	copy2 := ext.GetQuoteAssets()
	_, exists := copy2["0xNew"]
	assert.False(t, exists, "GetQuoteAssets should return a copy")
}

func TestBaseDexExtractor_IsChainSupported(t *testing.T) {
	cfg := &BaseDexExtractorConfig{
		SupportedChains: []types.ChainType{types.ChainTypeBSC, types.ChainTypeEthereum},
	}
	ext := NewBaseDexExtractor(cfg)

	assert.True(t, ext.IsChainSupported(types.ChainTypeBSC))
	assert.True(t, ext.IsChainSupported(types.ChainTypeEthereum))
	assert.False(t, ext.IsChainSupported(types.ChainTypeSolana))
	assert.False(t, ext.IsChainSupported(types.ChainTypeSui))
}

func TestBaseDexExtractor_IsChainSupported_Empty(t *testing.T) {
	ext := NewBaseDexExtractor(nil)
	assert.False(t, ext.IsChainSupported(types.ChainTypeBSC))
}

func TestBaseDexExtractor_GetLogger(t *testing.T) {
	ext := NewBaseDexExtractor(nil)
	logger := ext.GetLogger()
	require.NotNil(t, logger)
	assert.Equal(t, "parser", logger.Data["service"])
}

func TestBaseDexExtractor_ExtractDexData_Default(t *testing.T) {
	ext := NewBaseDexExtractor(nil)
	ctx := context.Background()

	result, err := ext.ExtractDexData(ctx, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Pools)
	assert.Empty(t, result.Transactions)
	assert.Empty(t, result.Liquidities)
	assert.Empty(t, result.Reserves)
	assert.Empty(t, result.Tokens)
}

func TestBaseDexExtractor_SupportsBlock_Default(t *testing.T) {
	ext := NewBaseDexExtractor(nil)
	block := &types.UnifiedBlock{}
	assert.False(t, ext.SupportsBlock(block), "default SupportsBlock should return false")
}

func TestBaseDexExtractor_MergeQuoteAssets(t *testing.T) {
	cfg := &BaseDexExtractorConfig{
		QuoteAssets: map[string]int{"0xUSDT": 100, "0xWBNB": 50},
	}
	ext := NewBaseDexExtractor(cfg)

	ext.MergeQuoteAssets(map[string]int{"0xUSDC": 90, "0xWBNB": 60})

	assets := ext.GetQuoteAssets()
	assert.Equal(t, 100, assets["0xUSDT"])
	assert.Equal(t, 60, assets["0xWBNB"], "external should overwrite existing")
	assert.Equal(t, 90, assets["0xUSDC"], "new asset should be added")
}

func TestBaseDexExtractor_MergeQuoteAssets_Empty(t *testing.T) {
	cfg := &BaseDexExtractorConfig{
		QuoteAssets: map[string]int{"0xUSDT": 100},
	}
	ext := NewBaseDexExtractor(cfg)

	ext.MergeQuoteAssets(map[string]int{})
	assert.Equal(t, 100, ext.GetQuoteAssetRank("0xUSDT"))
}

func TestBaseDexExtractor_GetQuoteAssetRank(t *testing.T) {
	cfg := &BaseDexExtractorConfig{
		QuoteAssets: map[string]int{"0xUSDT": 100, "0xWBNB": 50},
	}
	ext := NewBaseDexExtractor(cfg)

	assert.Equal(t, 100, ext.GetQuoteAssetRank("0xUSDT"))
	assert.Equal(t, 50, ext.GetQuoteAssetRank("0xWBNB"))
	assert.Equal(t, -1, ext.GetQuoteAssetRank("0xUnknown"))
}

func TestBaseDexExtractor_GetQuoteAssetRank_Empty(t *testing.T) {
	ext := NewBaseDexExtractor(nil)
	assert.Equal(t, -1, ext.GetQuoteAssetRank("0xAnything"))
}
