package dex

import (
	"testing"
	"time"

	"unified-tx-parser/internal/types"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEVMDexExtractor_NilConfig(t *testing.T) {
	ext := NewEVMDexExtractor(nil)
	require.NotNil(t, ext)
	require.NotNil(t, ext.BaseDexExtractor)
}

func TestNewEVMDexExtractor_WithConfig(t *testing.T) {
	cfg := &BaseDexExtractorConfig{
		Protocols:       []string{"test-evm"},
		SupportedChains: []types.ChainType{types.ChainTypeBSC},
	}
	ext := NewEVMDexExtractor(cfg)
	assert.Equal(t, []string{"test-evm"}, ext.GetSupportedProtocols())
	assert.Equal(t, []types.ChainType{types.ChainTypeBSC}, ext.GetSupportedChains())
}

func TestEVMDexExtractor_InheritsBaseMethods(t *testing.T) {
	cfg := &BaseDexExtractorConfig{
		Protocols:       []string{"evm-proto"},
		SupportedChains: []types.ChainType{types.ChainTypeBSC, types.ChainTypeEthereum},
		QuoteAssets:     map[string]int{"0xUSDT": 100},
	}
	ext := NewEVMDexExtractor(cfg)

	// Test inherited methods
	assert.Equal(t, []string{"evm-proto"}, ext.GetSupportedProtocols())
	assert.True(t, ext.IsChainSupported(types.ChainTypeBSC))
	assert.Equal(t, 100, ext.GetQuoteAssetRank("0xUSDT"))
	assert.NotNil(t, ext.GetLogger())
}

func TestEVMDexExtractor_ExtractEVMLogs_NilTx(t *testing.T) {
	ext := NewEVMDexExtractor(nil)
	logs := ext.ExtractEVMLogs(nil)
	assert.Empty(t, logs)
}

func TestEVMDexExtractor_ExtractEVMLogs_NilRawData(t *testing.T) {
	ext := NewEVMDexExtractor(nil)
	tx := &types.UnifiedTransaction{RawData: nil}
	logs := ext.ExtractEVMLogs(tx)
	assert.Empty(t, logs)
}

func TestEVMDexExtractor_ExtractEVMLogs_FromReceipt(t *testing.T) {
	ext := NewEVMDexExtractor(nil)

	ethLogs := []*ethtypes.Log{
		{Address: common.HexToAddress("0x1111"), Topics: []common.Hash{common.HexToHash("0xaaa")}},
		{Address: common.HexToAddress("0x2222"), Topics: []common.Hash{common.HexToHash("0xbbb")}},
	}
	tx := &types.UnifiedTransaction{
		RawData: &ethtypes.Receipt{Logs: ethLogs},
	}

	logs := ext.ExtractEVMLogs(tx)
	require.Len(t, logs, 2)
	assert.Equal(t, common.HexToAddress("0x1111"), logs[0].Address)
	assert.Equal(t, common.HexToAddress("0x2222"), logs[1].Address)
}

func TestEVMDexExtractor_ExtractEVMLogs_FromLogSlice(t *testing.T) {
	ext := NewEVMDexExtractor(nil)

	ethLogs := []*ethtypes.Log{
		{Address: common.HexToAddress("0x3333")},
	}
	tx := &types.UnifiedTransaction{
		RawData: ethLogs,
	}

	logs := ext.ExtractEVMLogs(tx)
	require.Len(t, logs, 1)
	assert.Equal(t, common.HexToAddress("0x3333"), logs[0].Address)
}

func TestEVMDexExtractor_ExtractEVMLogs_FromReceiptNilLogs(t *testing.T) {
	ext := NewEVMDexExtractor(nil)
	tx := &types.UnifiedTransaction{
		RawData: &ethtypes.Receipt{Logs: nil},
	}
	logs := ext.ExtractEVMLogs(tx)
	assert.Empty(t, logs)
}

func TestEVMDexExtractor_ExtractEVMLogs_UnsupportedRawDataType(t *testing.T) {
	ext := NewEVMDexExtractor(nil)
	tx := &types.UnifiedTransaction{
		RawData: "unsupported string type",
	}
	logs := ext.ExtractEVMLogs(tx)
	assert.Empty(t, logs)
}

func TestEVMDexExtractor_IsEVMChainSupported(t *testing.T) {
	ext := NewEVMDexExtractor(nil)

	assert.True(t, ext.IsEVMChainSupported(types.ChainTypeEthereum))
	assert.True(t, ext.IsEVMChainSupported(types.ChainTypeBSC))
	assert.False(t, ext.IsEVMChainSupported(types.ChainTypeSolana))
	assert.False(t, ext.IsEVMChainSupported(types.ChainTypeSui))
}

func TestEVMDexExtractor_FilterLogsByTopics(t *testing.T) {
	ext := NewEVMDexExtractor(nil)

	topicA := common.HexToHash("0xaaaa")
	topicB := common.HexToHash("0xbbbb")
	topicC := common.HexToHash("0xcccc")

	logs := []*ethtypes.Log{
		{Topics: []common.Hash{topicA}},
		{Topics: []common.Hash{topicB}},
		{Topics: []common.Hash{topicC}},
		{Topics: []common.Hash{}}, // empty topics, should be skipped
		{Topics: nil},             // nil topics, should be skipped
	}

	filter := map[string]bool{
		topicA.Hex(): true,
		topicC.Hex(): true,
	}

	filtered := ext.FilterLogsByTopics(logs, filter)
	require.Len(t, filtered, 2)
	assert.Equal(t, topicA, filtered[0].Topics[0])
	assert.Equal(t, topicC, filtered[1].Topics[0])
}

func TestEVMDexExtractor_FilterLogsByTopics_Empty(t *testing.T) {
	ext := NewEVMDexExtractor(nil)

	filtered := ext.FilterLogsByTopics(nil, map[string]bool{})
	assert.Empty(t, filtered)

	filtered2 := ext.FilterLogsByTopics([]*ethtypes.Log{}, map[string]bool{"0x1": true})
	assert.Empty(t, filtered2)
}

func TestEVMDexExtractor_FilterLogsByTopics_NoMatch(t *testing.T) {
	ext := NewEVMDexExtractor(nil)

	logs := []*ethtypes.Log{
		{Topics: []common.Hash{common.HexToHash("0xaaaa")}},
	}
	filter := map[string]bool{
		common.HexToHash("0xbbbb").Hex(): true,
	}

	filtered := ext.FilterLogsByTopics(logs, filter)
	assert.Empty(t, filtered)
}

func TestShouldCacheExpire(t *testing.T) {
	// Past time should be expired
	assert.True(t, ShouldCacheExpire(time.Now().Add(-1*time.Second)))

	// Future time should not be expired
	assert.False(t, ShouldCacheExpire(time.Now().Add(1*time.Hour)))
}

func TestCalculateCacheExpiration(t *testing.T) {
	before := time.Now()
	expiration := CalculateCacheExpiration(5 * time.Minute)
	after := time.Now()

	assert.True(t, expiration.After(before.Add(4*time.Minute+59*time.Second)))
	assert.True(t, expiration.Before(after.Add(5*time.Minute+1*time.Second)))
}
