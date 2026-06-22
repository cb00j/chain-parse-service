package dex

import (
	"context"
	"fmt"
	"math/big"
	"testing"

	"unified-tx-parser/internal/model"
	"unified-tx-parser/internal/parser/dexs/testdata"
	"unified-tx-parser/internal/types"
	"unified-tx-parser/internal/types/mocks"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// newTestDEXExtractor creates a DEXExtractor with a manually constructed factory
// containing the given mock extractors, bypassing config-based creation.
func newTestDEXExtractor(extractors map[string]types.DexExtractors) *DEXExtractor {
	factory := NewExtractorFactory()
	for name, ext := range extractors {
		factory.RegisterExtractor(name, ext)
	}
	return &DEXExtractor{
		supportedChains: []types.ChainType{},
		factory:         factory,
	}
}

func TestDEXExtractor_GetSupportedProtocols(t *testing.T) {
	mockExt := new(mocks.MockDexExtractors)
	mockExt.On("GetSupportedProtocols").Return([]string{"pancakeswap-v2", "pancakeswap-v3"})

	extractor := newTestDEXExtractor(map[string]types.DexExtractors{"pancake": mockExt})

	protocols := extractor.GetSupportedProtocols()
	assert.ElementsMatch(t, []string{"pancakeswap-v2", "pancakeswap-v3"}, protocols)
}

func TestDEXExtractor_GetSupportedProtocols_NilFactory(t *testing.T) {
	extractor := &DEXExtractor{factory: nil}
	assert.Nil(t, extractor.GetSupportedProtocols())
}

func TestDEXExtractor_GetSupportedChains(t *testing.T) {
	extractor := &DEXExtractor{
		supportedChains: []types.ChainType{types.ChainTypeBSC, types.ChainTypeEthereum},
	}
	chains := extractor.GetSupportedChains()
	assert.Equal(t, []types.ChainType{types.ChainTypeBSC, types.ChainTypeEthereum}, chains)
}

func TestDEXExtractor_ExtractDexData_EmptyBlocks(t *testing.T) {
	mockExt := new(mocks.MockDexExtractors)
	mockExt.On("SupportsBlock", mock.Anything).Return(false)
	mockExt.On("GetSupportedProtocols").Return([]string{"test"})

	extractor := newTestDEXExtractor(map[string]types.DexExtractors{"test": mockExt})

	result, err := extractor.ExtractDexData(context.Background(), []types.UnifiedBlock{})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Pools)
	assert.Empty(t, result.Transactions)
	assert.Empty(t, result.Tokens)
}

func TestDEXExtractor_ExtractDexData_SingleExtractor(t *testing.T) {
	ctx := context.Background()
	block := testdata.BSCSwapBlock(100)

	expectedData := &types.DexData{
		Pools: []model.Pool{
			{Addr: "0xpool", Protocol: "pancakeswap", Tokens: map[int]string{0: "0xa", 1: "0xb"}, Fee: 25},
		},
		Transactions: []model.Transaction{
			{Pool: "0xpool", Hash: "0xbsctx1", From: "0xuser1", Side: "buy", Amount: big.NewInt(100)},
		},
		Liquidities: []model.Liquidity{},
		Reserves:    []model.Reserve{},
		Tokens:      []model.Token{},
	}

	mockExt := new(mocks.MockDexExtractors)
	mockExt.On("SupportsBlock", &block).Return(true)
	mockExt.On("ExtractDexData", ctx, []types.UnifiedBlock{block}).Return(expectedData, nil)

	extractor := newTestDEXExtractor(map[string]types.DexExtractors{"pancake": mockExt})

	result, err := extractor.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Len(t, result.Pools, 1)
	assert.Len(t, result.Transactions, 1)
	assert.Equal(t, "0xpool", result.Pools[0].Addr)

	mockExt.AssertExpectations(t)
}

func TestDEXExtractor_ExtractDexData_MultipleExtractors(t *testing.T) {
	ctx := context.Background()
	bscBlock := testdata.BSCSwapBlock(100)
	ethBlock := testdata.EthereumSwapBlock(200)
	blocks := []types.UnifiedBlock{bscBlock, ethBlock}

	bscData := &types.DexData{
		Pools:        []model.Pool{{Addr: "0xbscpool", Protocol: "pancake"}},
		Transactions: []model.Transaction{},
		Liquidities:  []model.Liquidity{},
		Reserves:     []model.Reserve{},
		Tokens:       []model.Token{},
	}
	ethData := &types.DexData{
		Pools:        []model.Pool{{Addr: "0xethpool", Protocol: "uniswap"}},
		Transactions: []model.Transaction{},
		Liquidities:  []model.Liquidity{},
		Reserves:     []model.Reserve{},
		Tokens:       []model.Token{},
	}

	bscMock := new(mocks.MockDexExtractors)
	bscMock.On("SupportsBlock", &bscBlock).Return(true)
	bscMock.On("SupportsBlock", &ethBlock).Return(false)
	bscMock.On("ExtractDexData", ctx, []types.UnifiedBlock{bscBlock}).Return(bscData, nil)

	ethMock := new(mocks.MockDexExtractors)
	ethMock.On("SupportsBlock", &bscBlock).Return(false)
	ethMock.On("SupportsBlock", &ethBlock).Return(true)
	ethMock.On("ExtractDexData", ctx, []types.UnifiedBlock{ethBlock}).Return(ethData, nil)

	extractor := newTestDEXExtractor(map[string]types.DexExtractors{
		"pancake": bscMock,
		"uniswap": ethMock,
	})

	result, err := extractor.ExtractDexData(ctx, blocks)
	require.NoError(t, err)
	assert.Len(t, result.Pools, 2)

	bscMock.AssertExpectations(t)
	ethMock.AssertExpectations(t)
}

func TestDEXExtractor_ExtractDexData_ExtractorError(t *testing.T) {
	ctx := context.Background()
	block := testdata.BSCSwapBlock(100)

	mockExt := new(mocks.MockDexExtractors)
	mockExt.On("SupportsBlock", &block).Return(true)
	mockExt.On("ExtractDexData", ctx, []types.UnifiedBlock{block}).Return(nil, fmt.Errorf("extraction failed"))

	extractor := newTestDEXExtractor(map[string]types.DexExtractors{"failing": mockExt})

	// Should not return error - extractor errors are logged and skipped
	result, err := extractor.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Empty(t, result.Pools)
	assert.Empty(t, result.Transactions)

	mockExt.AssertExpectations(t)
}

func TestDEXExtractor_ExtractDexData_NoSupportedBlocks(t *testing.T) {
	ctx := context.Background()
	block := testdata.EmptyBlock(types.ChainTypeSolana, 500)

	mockExt := new(mocks.MockDexExtractors)
	mockExt.On("SupportsBlock", &block).Return(false)

	extractor := newTestDEXExtractor(map[string]types.DexExtractors{"test": mockExt})

	result, err := extractor.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Empty(t, result.Pools)

	// ExtractDexData should NOT be called since no blocks are supported
	mockExt.AssertNotCalled(t, "ExtractDexData")
}

func TestDEXExtractor_SupportsBlock(t *testing.T) {
	bscBlock := testdata.BSCSwapBlock(100)
	ethBlock := testdata.EthereumSwapBlock(200)

	mockExt := new(mocks.MockDexExtractors)
	mockExt.On("SupportsBlock", &bscBlock).Return(true)
	mockExt.On("SupportsBlock", &ethBlock).Return(false)

	extractor := newTestDEXExtractor(map[string]types.DexExtractors{"test": mockExt})

	assert.True(t, extractor.SupportsBlock(&bscBlock))
	assert.False(t, extractor.SupportsBlock(&ethBlock))
}

func TestDEXExtractor_SupportsBlock_NilFactory(t *testing.T) {
	extractor := &DEXExtractor{factory: nil}
	block := testdata.BSCSwapBlock(100)
	assert.False(t, extractor.SupportsBlock(&block))
}

func TestDEXExtractor_ExtractDexData_MergesResults(t *testing.T) {
	ctx := context.Background()
	block := testdata.BSCSwapBlock(100)

	data1 := &types.DexData{
		Pools:        []model.Pool{{Addr: "pool1"}},
		Transactions: []model.Transaction{{Hash: "tx1"}},
		Liquidities:  []model.Liquidity{{Hash: "liq1"}},
		Reserves:     []model.Reserve{{Addr: "res1"}},
		Tokens:       []model.Token{{Addr: "tok1"}},
	}
	data2 := &types.DexData{
		Pools:        []model.Pool{{Addr: "pool2"}},
		Transactions: []model.Transaction{{Hash: "tx2"}},
		Liquidities:  []model.Liquidity{{Hash: "liq2"}},
		Reserves:     []model.Reserve{{Addr: "res2"}},
		Tokens:       []model.Token{{Addr: "tok2"}},
	}

	mock1 := new(mocks.MockDexExtractors)
	mock1.On("SupportsBlock", &block).Return(true)
	mock1.On("ExtractDexData", ctx, []types.UnifiedBlock{block}).Return(data1, nil)

	mock2 := new(mocks.MockDexExtractors)
	mock2.On("SupportsBlock", &block).Return(true)
	mock2.On("ExtractDexData", ctx, []types.UnifiedBlock{block}).Return(data2, nil)

	extractor := newTestDEXExtractor(map[string]types.DexExtractors{
		"ext1": mock1,
		"ext2": mock2,
	})

	result, err := extractor.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Len(t, result.Pools, 2)
	assert.Len(t, result.Transactions, 2)
	assert.Len(t, result.Liquidities, 2)
	assert.Len(t, result.Reserves, 2)
	assert.Len(t, result.Tokens, 2)
}
