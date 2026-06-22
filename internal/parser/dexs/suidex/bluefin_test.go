package suidex

import (
	"context"
	"testing"

	"unified-tx-parser/internal/parser/dexs/testdata"
	"unified-tx-parser/internal/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBluefinExtractor(t *testing.T) {
	ext := NewBluefinExtractor()
	require.NotNil(t, ext)
	assert.NotNil(t, ext.tokenCache)
	assert.NotNil(t, ext.poolCache)
	assert.NotNil(t, ext.quoteAssets)
}

func TestBluefin_GetSupportedProtocols(t *testing.T) {
	ext := NewBluefinExtractor()
	protocols := ext.GetSupportedProtocols()
	assert.Equal(t, []string{"bluefin"}, protocols)
}

func TestBluefin_GetSupportedChains(t *testing.T) {
	ext := NewBluefinExtractor()
	chains := ext.GetSupportedChains()
	assert.Equal(t, []types.ChainType{types.ChainTypeSui}, chains)
}

func TestBluefin_SetQuoteAssets(t *testing.T) {
	ext := NewBluefinExtractor()
	original := ext.quoteAssets

	newAssets := map[string]int{"0xNewToken": 100}
	ext.SetQuoteAssets(newAssets)
	assert.Equal(t, newAssets, ext.quoteAssets)

	// Empty map should not overwrite
	ext.SetQuoteAssets(map[string]int{})
	assert.Equal(t, newAssets, ext.quoteAssets)

	// Nil check - original defaults should have been set
	assert.NotNil(t, original)
}

func TestBluefin_SupportsBlock_Sui(t *testing.T) {
	ext := NewBluefinExtractor()

	block := testdata.EmptyBlock(types.ChainTypeSui, 100)
	assert.True(t, ext.SupportsBlock(&block), "should support Sui blocks")
}

func TestBluefin_SupportsBlock_WrongChain(t *testing.T) {
	ext := NewBluefinExtractor()

	tests := []struct {
		name      string
		chainType types.ChainType
	}{
		{"BSC", types.ChainTypeBSC},
		{"Ethereum", types.ChainTypeEthereum},
		{"Solana", types.ChainTypeSolana},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			block := testdata.EmptyBlock(tt.chainType, 100)
			assert.False(t, ext.SupportsBlock(&block))
		})
	}
}

func TestBluefin_ExtractDexData_NilClient(t *testing.T) {
	ext := NewBluefinExtractor()
	// client is nil by default
	ctx := context.Background()

	block := testdata.EmptyBlock(types.ChainTypeSui, 100)
	_, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	assert.Error(t, err, "should error when SuiProcessor is not set")
	assert.Contains(t, err.Error(), "未设置")
}

func TestBluefin_ExtractDexData_EmptyBlocks_NilClient(t *testing.T) {
	ext := NewBluefinExtractor()
	ctx := context.Background()

	// Empty blocks slice - should still error because client check happens first
	_, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{})
	assert.Error(t, err)
}

func TestBluefin_ExtractDexData_UnsupportedChainSkipped(t *testing.T) {
	// Even without a client, non-Sui blocks are skipped in the loop
	// But the nil client check happens before the loop
	ext := NewBluefinExtractor()
	ctx := context.Background()

	block := testdata.EmptyBlock(types.ChainTypeBSC, 100)
	_, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	// Still errors because of nil client check at the top
	assert.Error(t, err)
}

func TestBluefin_isBluefinEvent(t *testing.T) {
	ext := NewBluefinExtractor()

	tests := []struct {
		name     string
		event    map[string]interface{}
		expected bool
	}{
		{
			"AssetSwap event",
			map[string]interface{}{"type": bluefinAssetSwapEventType},
			true,
		},
		{
			"FlashSwap event",
			map[string]interface{}{"type": bluefinFlashSwapEventType},
			true,
		},
		{
			"AddLiquidity event",
			map[string]interface{}{"type": bluefinAddLiquidityEventType},
			true,
		},
		{
			"RemoveLiquidity event",
			map[string]interface{}{"type": bluefinRemoveLiquidityEventType},
			true,
		},
		{
			"PoolCreated event",
			map[string]interface{}{"type": bluefinPoolCreatedEventType},
			true,
		},
		{
			"Non-Bluefin event",
			map[string]interface{}{"type": "0xother::module::SomeEvent"},
			false,
		},
		{
			"Missing type",
			map[string]interface{}{},
			false,
		},
		{
			"Non-string type",
			map[string]interface{}{"type": 123},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, ext.isBluefinEvent(tt.event))
		})
	}
}

func TestBluefin_getEventType(t *testing.T) {
	ext := NewBluefinExtractor()

	tests := []struct {
		name     string
		event    map[string]interface{}
		expected string
	}{
		{"AssetSwap", map[string]interface{}{"type": bluefinAssetSwapEventType}, "swap"},
		{"FlashSwap", map[string]interface{}{"type": bluefinFlashSwapEventType}, "swap"},
		{"AddLiquidity", map[string]interface{}{"type": bluefinAddLiquidityEventType}, "add_liquidity"},
		{"RemoveLiquidity", map[string]interface{}{"type": bluefinRemoveLiquidityEventType}, "remove_liquidity"},
		{"PoolCreated", map[string]interface{}{"type": bluefinPoolCreatedEventType}, "pool_created"},
		{"Unknown", map[string]interface{}{"type": "unknown"}, ""},
		{"No type", map[string]interface{}{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, ext.getEventType(tt.event))
		})
	}
}

func TestBluefin_extractEventSeq(t *testing.T) {
	ext := NewBluefinExtractor()

	// Normal case
	event := map[string]interface{}{
		"id": map[string]interface{}{
			"eventSeq": "42",
		},
	}
	assert.Equal(t, "42", ext.extractEventSeq(event))

	// Missing id
	assert.Equal(t, "", ext.extractEventSeq(map[string]interface{}{}))

	// Missing eventSeq
	event2 := map[string]interface{}{
		"id": map[string]interface{}{},
	}
	assert.Equal(t, "", ext.extractEventSeq(event2))
}

func TestBluefin_extractSender(t *testing.T) {
	ext := NewBluefinExtractor()

	event := map[string]interface{}{"sender": "0xSuiUser"}
	assert.Equal(t, "0xSuiUser", ext.extractSender(event))

	assert.Equal(t, "", ext.extractSender(map[string]interface{}{}))
}

func TestBluefin_parseEventSeq(t *testing.T) {
	ext := NewBluefinExtractor()

	assert.Equal(t, int64(42), ext.parseEventSeq("42"))
	assert.Equal(t, int64(0), ext.parseEventSeq(""))
	assert.Equal(t, int64(0), ext.parseEventSeq("not_a_number"))
}

func TestBluefin_isStableCoin(t *testing.T) {
	ext := NewBluefinExtractor()

	// USDC has rank 100 >= 90
	usdc := "0xdba34672e30cb065b1f93e3ab55318768fd6fef66c15942c9f7cb846e2f900e7::usdc::USDC"
	assert.True(t, ext.isStableCoin(usdc))

	// Unknown token
	assert.False(t, ext.isStableCoin("0xunknown::token::TOKEN"))
}

func TestBluefin_ExtractPoolCoin(t *testing.T) {
	ext := NewBluefinExtractor()

	// Test with a typical pool type string
	// The actual format depends on utils.ExtractPoolTokens
	token0, token1 := ext.ExtractPoolCoin("0x2::sui::SUI")
	// At minimum, check it doesn't panic
	_ = token0
	_ = token1
}

func TestBluefin_getBlockNumber(t *testing.T) {
	ext := NewBluefinExtractor()

	tx := testdata.TxWithSuiEvents("0xtx", 12345, nil)
	assert.Equal(t, int64(12345), ext.getBlockNumber(&tx))

	txNilBlock := types.UnifiedTransaction{BlockNumber: nil}
	assert.Equal(t, int64(0), ext.getBlockNumber(&txNilBlock))
}

func TestBluefin_extractSuiEventsFromTransaction(t *testing.T) {
	ext := NewBluefinExtractor()

	// Transaction with events in RawData
	swapEvent := testdata.SuiSwapEvent(
		bluefinAssetSwapEventType,
		"0xpool1",
		"0xuser",
		"1000000",
		"2000000",
		true,
	)
	tx := testdata.TxWithSuiEvents("0xtx1", 100, []map[string]interface{}{swapEvent})

	events := ext.extractSuiEventsFromTransaction(&tx)
	require.NotEmpty(t, events)
	assert.True(t, ext.isBluefinEvent(events[0]))
}

func TestBluefin_extractSuiEventsFromTransaction_NoRawData(t *testing.T) {
	ext := NewBluefinExtractor()

	tx := types.UnifiedTransaction{RawData: nil}
	events := ext.extractSuiEventsFromTransaction(&tx)
	assert.Empty(t, events)
}

func TestBluefin_extractSuiEventsFromTransaction_NoEvents(t *testing.T) {
	ext := NewBluefinExtractor()

	tx := types.UnifiedTransaction{
		RawData: map[string]interface{}{
			"other": "data",
		},
	}
	events := ext.extractSuiEventsFromTransaction(&tx)
	assert.Empty(t, events)
}

func TestBluefin_getStringField(t *testing.T) {
	ext := NewBluefinExtractor()

	fields := map[string]interface{}{
		"pool_id": "0xabc",
		"number":  123,
	}

	assert.Equal(t, "0xabc", ext.getStringField(fields, "pool_id"))
	assert.Equal(t, "", ext.getStringField(fields, "missing"))
}

func TestBluefin_getBoolField(t *testing.T) {
	ext := NewBluefinExtractor()

	fields := map[string]interface{}{
		"a2b": true,
		"str": "not_bool",
	}

	assert.True(t, ext.getBoolField(fields, "a2b"))
	assert.False(t, ext.getBoolField(fields, "missing"))
	assert.False(t, ext.getBoolField(fields, "str"))
}
