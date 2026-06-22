package bsc

import (
	"context"
	"math/big"
	"testing"

	"unified-tx-parser/internal/parser/dexs/testdata"
	"unified-tx-parser/internal/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPancakeSwapExtractor(t *testing.T) {
	ext := NewPancakeSwapExtractor()
	require.NotNil(t, ext)
}

func TestPancakeSwap_GetSupportedProtocols(t *testing.T) {
	ext := NewPancakeSwapExtractor()
	protocols := ext.GetSupportedProtocols()
	assert.ElementsMatch(t, []string{"pancakeswap", "pancakeswap-v2", "pancakeswap-v3"}, protocols)
}

func TestPancakeSwap_GetSupportedChains(t *testing.T) {
	ext := NewPancakeSwapExtractor()
	chains := ext.GetSupportedChains()
	assert.ElementsMatch(t, []types.ChainType{types.ChainTypeBSC}, chains)
}

func TestPancakeSwap_SetQuoteAssets(t *testing.T) {
	ext := NewPancakeSwapExtractor()
	assets := map[string]int{"0xWBNB": 50, "0xUSDT": 100}
	ext.SetQuoteAssets(assets)
	assert.Equal(t, assets, ext.GetQuoteAssets())

	// empty map should not overwrite
	ext.SetQuoteAssets(map[string]int{})
	assert.Equal(t, assets, ext.GetQuoteAssets())
}

func TestPancakeSwap_SupportsBlock_CorrectChain(t *testing.T) {
	ext := NewPancakeSwapExtractor()

	// BSC block with PancakeSwap V2 swap log
	swapLog := testdata.MakeEVMLog(
		"0xPancakePair",
		testdata.SwapV2EventSig,
		[]string{"0x0000000000000000000000000000000000000000000000000000000000000001", "0x0000000000000000000000000000000000000000000000000000000000000002"},
		testdata.V2SwapLogData(big.NewInt(1e18), big.NewInt(0), big.NewInt(0), big.NewInt(2e18)),
	)
	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xtx1", 100, []map[string]interface{}{swapLog})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 100, []types.UnifiedTransaction{tx})

	assert.True(t, ext.SupportsBlock(&block))
}

func TestPancakeSwap_SupportsBlock_WrongChain(t *testing.T) {
	ext := NewPancakeSwapExtractor()

	block := testdata.EmptyBlock(types.ChainTypeSolana, 100)
	assert.False(t, ext.SupportsBlock(&block))

	block2 := testdata.EmptyBlock(types.ChainTypeSui, 200)
	assert.False(t, ext.SupportsBlock(&block2))
}

func TestPancakeSwap_SupportsBlock_NoRelevantLogs(t *testing.T) {
	ext := NewPancakeSwapExtractor()

	// BSC block with a non-PancakeSwap log (random topic)
	irrelevantLog := testdata.MakeEVMLog(
		"0xSomeContract",
		"0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		nil,
		[]byte{0x01, 0x02},
	)
	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xtx1", 100, []map[string]interface{}{irrelevantLog})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 100, []types.UnifiedTransaction{tx})

	assert.False(t, ext.SupportsBlock(&block))
}

func TestPancakeSwap_SupportsBlock_EmptyBlock(t *testing.T) {
	ext := NewPancakeSwapExtractor()
	block := testdata.EmptyBlock(types.ChainTypeBSC, 100)
	assert.False(t, ext.SupportsBlock(&block))
}

func TestPancakeSwap_ExtractDexData_EmptyBlocks(t *testing.T) {
	ext := NewPancakeSwapExtractor()
	ctx := context.Background()

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Transactions)
	assert.Empty(t, result.Pools)
	assert.Empty(t, result.Liquidities)
}

func TestPancakeSwap_ExtractDexData_V2Swap(t *testing.T) {
	ext := NewPancakeSwapExtractor()
	ctx := context.Background()

	amount0In := big.NewInt(1000000000000000000)  // 1e18
	amount1In := big.NewInt(0)
	amount0Out := big.NewInt(0)
	amount1Out := big.NewInt(2000000000000000000) // 2e18

	swapLog := testdata.MakeEVMLog(
		"0x0eD7e52944161450477ee417DE9Cd3a859b14fD0", // pool address
		testdata.SwapV2EventSig,
		[]string{
			"0x0000000000000000000000001234567890abcdef1234567890abcdef12345678", // sender
			"0x000000000000000000000000abcdefabcdefabcdefabcdefabcdefabcdefabcd", // to
		},
		testdata.V2SwapLogData(amount0In, amount1In, amount0Out, amount1Out),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xswaptx", 12345, []map[string]interface{}{swapLog})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 12345, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Transactions, 1)

	swap := result.Transactions[0]
	assert.Equal(t, "0xswaptx", swap.Hash)
	assert.Equal(t, "swap", swap.Side)
	assert.Equal(t, pancakeSwapV2FactoryAddr, swap.Factory)
	assert.NotNil(t, swap.Amount)
	assert.True(t, swap.Amount.Cmp(big.NewInt(0)) > 0)
	assert.Equal(t, int64(12345), swap.BlockNumber)
	assert.NotNil(t, swap.Extra)
	assert.Equal(t, "swap", swap.Extra.Type)
}

func TestPancakeSwap_ExtractDexData_V2Swap_ShortData(t *testing.T) {
	ext := NewPancakeSwapExtractor()
	ctx := context.Background()

	// Data too short for V2 swap (< 128 bytes)
	shortLog := testdata.MakeEVMLog(
		"0xPair",
		testdata.SwapV2EventSig,
		[]string{"0x0000000000000000000000000000000000000000000000000000000000000001", "0x0000000000000000000000000000000000000000000000000000000000000002"},
		make([]byte, 64), // only 64 bytes, need 128
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xshort", 100, []map[string]interface{}{shortLog})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 100, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Empty(t, result.Transactions, "should skip swap with short data")
}

func TestPancakeSwap_ExtractDexData_V2PairCreated(t *testing.T) {
	ext := NewPancakeSwapExtractor()
	ctx := context.Background()

	token0 := "0x0000000000000000000000000000000000000000000000000000000000000aaa"
	token1 := "0x0000000000000000000000000000000000000000000000000000000000000bbb"
	pairAddr := "0xNewPairAddress000000000000000000000000ab"

	pairCreatedLog := testdata.MakeEVMLog(
		pancakeSwapV2FactoryAddr,
		testdata.PairCreatedEventSig,
		[]string{token0, token1},
		testdata.PairCreatedLogData(pairAddr, big.NewInt(42)),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xpairtx", 200, []map[string]interface{}{pairCreatedLog})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 200, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Pools, 1)

	pool := result.Pools[0]
	assert.Equal(t, "pancakeswap", pool.Protocol)
	assert.Equal(t, pancakeSwapV2FactoryAddr, pool.Factory)
	assert.Equal(t, 2500, pool.Fee)
	assert.Len(t, pool.Tokens, 2)
	assert.NotNil(t, pool.Extra)
	assert.Equal(t, "0xpairtx", pool.Extra.Hash)
}

func TestPancakeSwap_ExtractDexData_V2Mint(t *testing.T) {
	ext := NewPancakeSwapExtractor()
	ctx := context.Background()

	amount0 := big.NewInt(5000000000000000000) // 5e18
	amount1 := big.NewInt(3000000000000000000) // 3e18

	mintLog := testdata.MakeEVMLog(
		"0xPancakePairAddr",
		testdata.MintV2EventSig,
		[]string{"0x0000000000000000000000001234567890abcdef1234567890abcdef12345678"}, // sender
		testdata.MakeEVMLogData(amount0, amount1),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xminttx", 300, []map[string]interface{}{mintLog})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 300, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Liquidities, 1)

	liq := result.Liquidities[0]
	assert.Equal(t, "add", liq.Side)
	assert.Equal(t, "0xminttx", liq.Hash)
	assert.NotNil(t, liq.Amount)
	assert.True(t, liq.Amount.Cmp(big.NewInt(0)) > 0)
}

func TestPancakeSwap_ExtractDexData_V2Burn(t *testing.T) {
	ext := NewPancakeSwapExtractor()
	ctx := context.Background()

	amount0 := big.NewInt(2000000000000000000)
	amount1 := big.NewInt(1000000000000000000)

	burnLog := testdata.MakeEVMLog(
		"0xPancakePairAddr",
		testdata.BurnV2EventSig,
		[]string{
			"0x0000000000000000000000001234567890abcdef1234567890abcdef12345678", // sender
			"0x000000000000000000000000abcdefabcdefabcdefabcdefabcdefabcdefabcd", // to
		},
		testdata.MakeEVMLogData(amount0, amount1),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xburntx", 400, []map[string]interface{}{burnLog})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 400, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Liquidities, 1)

	liq := result.Liquidities[0]
	assert.Equal(t, "remove", liq.Side)
	assert.Equal(t, "0xburntx", liq.Hash)
}

func TestPancakeSwap_ExtractDexData_UnsupportedChainSkipped(t *testing.T) {
	ext := NewPancakeSwapExtractor()
	ctx := context.Background()

	// Solana block should be completely skipped
	block := testdata.EmptyBlock(types.ChainTypeSolana, 100)
	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Empty(t, result.Transactions)
	assert.Empty(t, result.Pools)
}

func TestPancakeSwap_ExtractDexData_MultipleEventsInBlock(t *testing.T) {
	ext := NewPancakeSwapExtractor()
	ctx := context.Background()

	swapLog := testdata.MakeEVMLog(
		"0xPair1",
		testdata.SwapV2EventSig,
		[]string{
			"0x0000000000000000000000001111111111111111111111111111111111111111",
			"0x0000000000000000000000002222222222222222222222222222222222222222",
		},
		testdata.V2SwapLogData(big.NewInt(1e18), big.NewInt(0), big.NewInt(0), big.NewInt(2e18)),
	)

	mintLog := testdata.MakeEVMLog(
		"0xPair2",
		testdata.MintV2EventSig,
		[]string{"0x0000000000000000000000003333333333333333333333333333333333333333"},
		testdata.MakeEVMLogData(big.NewInt(5e18), big.NewInt(3e18)),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xmultitx", 500, []map[string]interface{}{swapLog, mintLog})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 500, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Len(t, result.Transactions, 1, "should have 1 swap")
	assert.Len(t, result.Liquidities, 1, "should have 1 mint")
}

func TestPancakeSwap_ExtractDexData_NoRawData(t *testing.T) {
	ext := NewPancakeSwapExtractor()
	ctx := context.Background()

	// Transaction with nil RawData
	tx := types.UnifiedTransaction{
		TxHash:    "0xnilraw",
		ChainType: types.ChainTypeBSC,
		ChainID:   "56",
		RawData:   nil,
	}
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 600, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Empty(t, result.Transactions)
}

func TestPancakeSwap_ExtractDexData_V3Swap(t *testing.T) {
	ext := NewPancakeSwapExtractor()
	ctx := context.Background()

	// V3 Swap: (int256 amount0, int256 amount1, uint160 sqrtPriceX96, uint128 liquidity, int24 tick)
	// amount0 positive = user pays token0, amount1 negative = user receives token1
	amount0 := big.NewInt(500000000000000000)  // 0.5e18 (positive = in)
	amount1 := big.NewInt(-1500000000000000000) // -1.5e18 (negative = out) -- will be encoded as two's complement
	// For signed encoding: negative values need two's complement in 256-bit
	amount1TwosComplement := new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 256), amount1)
	sqrtPriceX96 := new(big.Int).Mul(big.NewInt(79228162514264337), big.NewInt(1000000)) // ~79228162514264337000000
	liquidity := big.NewInt(1000000000000000000)
	tick := int32(-887220)

	v3SwapLog := testdata.MakeEVMLog(
		"0xPancakeV3Pool",
		testdata.SwapV3EventSig,
		[]string{
			"0x0000000000000000000000001111111111111111111111111111111111111111", // sender
			"0x0000000000000000000000002222222222222222222222222222222222222222", // recipient
		},
		testdata.V3SwapLogData(amount0, amount1TwosComplement, sqrtPriceX96, liquidity, tick),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xv3swaptx", 42000000, []map[string]interface{}{v3SwapLog})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 42000000, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Transactions, 1)

	swap := result.Transactions[0]
	assert.Equal(t, "0xv3swaptx", swap.Hash)
	assert.Equal(t, "swap", swap.Side)
	assert.Equal(t, pancakeSwapV3FactoryAddr, swap.Factory)
	assert.NotNil(t, swap.Amount)
	assert.True(t, swap.Amount.Cmp(big.NewInt(0)) > 0)
	assert.Equal(t, int64(42000000), swap.BlockNumber)
	assert.True(t, swap.Price > 0, "V3 price should be positive")
}

func TestPancakeSwap_ExtractDexData_V3Swap_ShortData(t *testing.T) {
	ext := NewPancakeSwapExtractor()
	ctx := context.Background()

	// V3 swap needs >= 160 bytes, provide only 128
	shortLog := testdata.MakeEVMLog(
		"0xV3Pool",
		testdata.SwapV3EventSig,
		[]string{
			"0x0000000000000000000000001111111111111111111111111111111111111111",
			"0x0000000000000000000000002222222222222222222222222222222222222222",
		},
		make([]byte, 128),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xv3short", 100, []map[string]interface{}{shortLog})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 100, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Empty(t, result.Transactions, "should skip V3 swap with short data")
}

func TestPancakeSwap_ExtractDexData_V3PoolCreated(t *testing.T) {
	ext := NewPancakeSwapExtractor()
	ctx := context.Background()

	token0 := "0x0000000000000000000000000000000000000000000000000000000000000aaa"
	token1 := "0x0000000000000000000000000000000000000000000000000000000000000bbb"
	feeHash := "0x0000000000000000000000000000000000000000000000000000000000000bb8" // fee=3000

	poolCreatedLog := testdata.MakeEVMLog(
		pancakeSwapV3FactoryAddr,
		testdata.PoolCreatedEventSig,
		[]string{token0, token1, feeHash},
		testdata.PoolCreatedV3LogData(60, "0xNewV3PoolAddr"),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xpoolcreatetx", 42000010, []map[string]interface{}{poolCreatedLog})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 42000010, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Pools, 1)

	pool := result.Pools[0]
	assert.Equal(t, "pancakeswap", pool.Protocol)
	assert.Equal(t, pancakeSwapV3FactoryAddr, pool.Factory)
	assert.Len(t, pool.Tokens, 2)
	assert.Equal(t, 3000, pool.Fee)
}

func TestPancakeSwap_ExtractDexData_V3Mint(t *testing.T) {
	ext := NewPancakeSwapExtractor()
	ctx := context.Background()

	// V3 Mint: (address sender, uint128 amount, uint256 amount0, uint256 amount1)
	amount := big.NewInt(1000000000000000000)
	amount0 := big.NewInt(2000000000000000000)
	amount1 := big.NewInt(3000000000000000000)

	mintLog := testdata.MakeEVMLog(
		"0xV3PoolAddr",
		testdata.MintV3EventSig,
		[]string{
			"0x0000000000000000000000001234567890abcdef1234567890abcdef12345678", // owner
			"0x000000000000000000000000000000000000000000000000000000000000fffe", // tickLower
			"0x0000000000000000000000000000000000000000000000000000000000010000", // tickUpper
		},
		testdata.V3MintLogData("0xSender", amount, amount0, amount1),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xv3minttx", 42000020, []map[string]interface{}{mintLog})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 42000020, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Liquidities, 1)

	liq := result.Liquidities[0]
	assert.Equal(t, "add", liq.Side)
	assert.Equal(t, "0xv3minttx", liq.Hash)
	assert.Equal(t, pancakeSwapV3FactoryAddr, liq.Factory)
	assert.NotNil(t, liq.Amount)
	assert.True(t, liq.Amount.Cmp(big.NewInt(0)) > 0)
}

func TestPancakeSwap_ExtractDexData_V3Burn(t *testing.T) {
	ext := NewPancakeSwapExtractor()
	ctx := context.Background()

	// V3 Burn: (uint128 amount, uint256 amount0, uint256 amount1)
	amount := big.NewInt(500000000000000000)
	amount0 := big.NewInt(1000000000000000000)
	amount1 := big.NewInt(1500000000000000000)

	burnLog := testdata.MakeEVMLog(
		"0xV3PoolAddr",
		testdata.BurnV3EventSig,
		[]string{
			"0x0000000000000000000000001234567890abcdef1234567890abcdef12345678", // owner
			"0x000000000000000000000000000000000000000000000000000000000000fffe", // tickLower
			"0x0000000000000000000000000000000000000000000000000000000000010000", // tickUpper
		},
		testdata.V3BurnLogData(amount, amount0, amount1),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xv3burntx", 42000030, []map[string]interface{}{burnLog})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 42000030, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Liquidities, 1)

	liq := result.Liquidities[0]
	assert.Equal(t, "remove", liq.Side)
	assert.Equal(t, "0xv3burntx", liq.Hash)
	assert.Equal(t, pancakeSwapV3FactoryAddr, liq.Factory)
	assert.NotNil(t, liq.Amount)
	assert.True(t, liq.Amount.Cmp(big.NewInt(0)) > 0)
}

func TestPancakeSwap_isPancakeSwapLog(t *testing.T) {
	ext := NewPancakeSwapExtractor()

	tests := []struct {
		name     string
		topic0   string
		expected bool
	}{
		{"V2 Swap", pancakeSwapV2EventSig, true},
		{"V3 Swap", pancakeSwapV3EventSig, true},
		{"V2 Mint", pancakeMintV2EventSig, true},
		{"V3 Mint", pancakeMintV3EventSig, true},
		{"V2 Burn", pancakeBurnV2EventSig, true},
		{"V3 Burn", pancakeBurnV3EventSig, true},
		{"PairCreated", pancakePairCreatedEventSig, true},
		{"PoolCreated", pancakePoolCreatedEventSig, true},
		{"Unknown", "0xdeadbeef00000000000000000000000000000000000000000000000000000000", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := makeMinimalEthLog(tt.topic0)
			assert.Equal(t, tt.expected, ext.isPancakeSwapLog(log))
		})
	}

	// No topics
	t.Run("No topics", func(t *testing.T) {
		log := makeMinimalEthLog("")
		log.Topics = nil
		assert.False(t, ext.isPancakeSwapLog(log))
	})
}

func TestPancakeSwap_ExtractDexData_MixedV2V3Events(t *testing.T) {
	ext := NewPancakeSwapExtractor()
	ctx := context.Background()

	// V2 Swap
	v2SwapLog := testdata.MakeEVMLog(
		"0xV2Pair",
		testdata.SwapV2EventSig,
		[]string{
			"0x0000000000000000000000001111111111111111111111111111111111111111",
			"0x0000000000000000000000002222222222222222222222222222222222222222",
		},
		testdata.V2SwapLogData(big.NewInt(1e18), big.NewInt(0), big.NewInt(0), big.NewInt(2e18)),
	)

	// V3 Swap
	sqrtPriceX96 := new(big.Int).Mul(big.NewInt(79228162514264337), big.NewInt(1000000))
	v3SwapLog := testdata.MakeEVMLog(
		"0xV3Pool",
		testdata.SwapV3EventSig,
		[]string{
			"0x0000000000000000000000001111111111111111111111111111111111111111",
			"0x0000000000000000000000002222222222222222222222222222222222222222",
		},
		testdata.V3SwapLogData(big.NewInt(1e18), big.NewInt(2e18), sqrtPriceX96, big.NewInt(1e18), 100),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xmixedtx", 42100000, []map[string]interface{}{v2SwapLog, v3SwapLog})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 42100000, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Transactions, 2, "should have both V2 and V3 swaps")

	assert.Equal(t, pancakeSwapV2FactoryAddr, result.Transactions[0].Factory)
	assert.Equal(t, pancakeSwapV3FactoryAddr, result.Transactions[1].Factory)
}

func TestPancakeSwap_getLogType(t *testing.T) {
	ext := NewPancakeSwapExtractor()

	tests := []struct {
		name     string
		topic0   string
		expected string
	}{
		{"V2 swap", pancakeSwapV2EventSig, "swap_v2"},
		{"V3 swap", pancakeSwapV3EventSig, "swap_v3"},
		{"V2 mint", pancakeMintV2EventSig, "mint"},
		{"V3 mint", pancakeMintV3EventSig, "mint"},
		{"V2 burn", pancakeBurnV2EventSig, "burn"},
		{"V3 burn", pancakeBurnV3EventSig, "burn"},
		{"pair created", pancakePairCreatedEventSig, "pair_created"},
		{"pool created", pancakePoolCreatedEventSig, "pool_created"},
		{"unknown", "0xunknown", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := makeMinimalEthLog(tt.topic0)
			assert.Equal(t, tt.expected, ext.getLogType(log))
		})
	}
}

func TestPancakeSwap_getLogType_NoTopics(t *testing.T) {
	ext := NewPancakeSwapExtractor()

	log := makeMinimalEthLog("")
	log.Topics = nil
	assert.Equal(t, "", ext.getLogType(log))
}

// Regression test: V3 negative amount0 must be handled via toSignedInt256 (same as Uniswap Bug #1)
func TestPancakeSwap_ExtractDexData_V3Swap_NegativeAmount0(t *testing.T) {
	ext := NewPancakeSwapExtractor()
	ctx := context.Background()

	// amount0 is negative (user receives token0), amount1 is positive (user pays token1)
	amount0Raw := big.NewInt(-2000000000000000000) // -2e18
	amount0TwosComplement := new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 256), amount0Raw)
	amount1 := big.NewInt(6000000000000000000) // 6e18 positive
	sqrtPriceX96 := new(big.Int).Mul(big.NewInt(79228162514264337), big.NewInt(1000000))
	liquidity := big.NewInt(1000000000000000000)

	v3SwapLog := testdata.MakeEVMLog(
		"0xPancakeV3PoolNeg",
		testdata.SwapV3EventSig,
		[]string{
			"0x0000000000000000000000001111111111111111111111111111111111111111",
			"0x0000000000000000000000002222222222222222222222222222222222222222",
		},
		testdata.V3SwapLogData(amount0TwosComplement, amount1, sqrtPriceX96, liquidity, 0),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xpcsv3neg", 42000050, []map[string]interface{}{v3SwapLog})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 42000050, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Transactions, 1)

	swap := result.Transactions[0]
	// amount0 is negative (user receives), amount1 is positive (user pays)
	// amountIn should be abs(amount1) = 6e18 (the positive side = what user paid)
	assert.Equal(t, big.NewInt(6000000000000000000), swap.Amount,
		"V3 swap should use the positive amount (amount1) as amountIn when amount0 is negative")
	assert.Equal(t, pancakeSwapV3FactoryAddr, swap.Factory)
}

// Regression test: SwapIndex must increment for multiple swaps in same tx (same as Uniswap Bug #2)
func TestPancakeSwap_ExtractDexData_MultipleSwaps_SwapIndexIncrement(t *testing.T) {
	ext := NewPancakeSwapExtractor()
	ctx := context.Background()

	// Three V2 swap logs in a single transaction (multi-hop)
	swap1 := testdata.MakeEVMLog(
		"0xPcsPairA",
		testdata.SwapV2EventSig,
		[]string{
			"0x0000000000000000000000001111111111111111111111111111111111111111",
			"0x0000000000000000000000002222222222222222222222222222222222222222",
		},
		testdata.V2SwapLogData(big.NewInt(1e18), big.NewInt(0), big.NewInt(0), big.NewInt(2e18)),
	)
	swap2 := testdata.MakeEVMLog(
		"0xPcsPairB",
		testdata.SwapV2EventSig,
		[]string{
			"0x0000000000000000000000003333333333333333333333333333333333333333",
			"0x0000000000000000000000004444444444444444444444444444444444444444",
		},
		testdata.V2SwapLogData(big.NewInt(2e18), big.NewInt(0), big.NewInt(0), big.NewInt(5e18)),
	)
	swap3 := testdata.MakeEVMLog(
		"0xPcsPairC",
		testdata.SwapV2EventSig,
		[]string{
			"0x0000000000000000000000005555555555555555555555555555555555555555",
			"0x0000000000000000000000006666666666666666666666666666666666666666",
		},
		testdata.V2SwapLogData(big.NewInt(5e18), big.NewInt(0), big.NewInt(0), new(big.Int).Mul(big.NewInt(1e18), big.NewInt(10))),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xpcsmultihop", 42000060, []map[string]interface{}{swap1, swap2, swap3})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 42000060, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Transactions, 3)

	// SwapIndex must increment: 0, 1, 2
	assert.Equal(t, int64(0), result.Transactions[0].SwapIndex)
	assert.Equal(t, int64(1), result.Transactions[1].SwapIndex)
	assert.Equal(t, int64(2), result.Transactions[2].SwapIndex)

	// EventIndex should match logIdx: 0, 1, 2
	assert.Equal(t, int64(0), result.Transactions[0].EventIndex)
	assert.Equal(t, int64(1), result.Transactions[1].EventIndex)
	assert.Equal(t, int64(2), result.Transactions[2].EventIndex)
}

// Regression test: V2 PairCreated pool address from data, not factory (Bug #5 equivalent)
func TestPancakeSwap_ExtractDexData_V2PairCreated_PoolAddrFromData(t *testing.T) {
	ext := NewPancakeSwapExtractor()
	ctx := context.Background()

	token0 := "0x0000000000000000000000000000000000000000000000000000000000000aaa"
	token1 := "0x0000000000000000000000000000000000000000000000000000000000000bbb"

	pairLog := testdata.MakeEVMLog(
		pancakeSwapV2FactoryAddr,
		testdata.PairCreatedEventSig,
		[]string{token0, token1},
		testdata.PairCreatedLogData("0xNewPcsPairAddr", big.NewInt(1)),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xpcspairtx", 42000070, []map[string]interface{}{pairLog})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 42000070, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Pools, 1)

	pool := result.Pools[0]
	assert.Equal(t, pancakeSwapV2FactoryAddr, pool.Factory)
	// Pool.Addr must be the pair address from data, NOT the factory address
	assert.NotEqual(t, pancakeSwapV2FactoryAddr, pool.Addr, "pool.Addr must not be the factory address")
}

// Regression test: V3 PoolCreated pool address from data, not factory (Bug #5 equivalent)
func TestPancakeSwap_ExtractDexData_V3PoolCreated_PoolAddrFromData(t *testing.T) {
	ext := NewPancakeSwapExtractor()
	ctx := context.Background()

	token0 := "0x0000000000000000000000000000000000000000000000000000000000000aaa"
	token1 := "0x0000000000000000000000000000000000000000000000000000000000000bbb"
	feeHash := "0x0000000000000000000000000000000000000000000000000000000000000bb8"

	poolCreatedLog := testdata.MakeEVMLog(
		pancakeSwapV3FactoryAddr,
		testdata.PoolCreatedEventSig,
		[]string{token0, token1, feeHash},
		testdata.PoolCreatedV3LogData(60, "0xNewPcsV3Pool"),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xpcspoolcreatetx", 42000080, []map[string]interface{}{poolCreatedLog})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 42000080, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Pools, 1)

	pool := result.Pools[0]
	assert.Equal(t, pancakeSwapV3FactoryAddr, pool.Factory)
	// Pool.Addr must be from data[32:64], NOT log.Address (factory)
	assert.NotEqual(t, pancakeSwapV3FactoryAddr, pool.Addr, "pool.Addr must not be the factory address")
}

func TestPancakeSwap_getFactoryAddress(t *testing.T) {
	ext := NewPancakeSwapExtractor()

	v2Log := makeMinimalEthLog(pancakeSwapV2EventSig)
	assert.Equal(t, pancakeSwapV2FactoryAddr, ext.getFactoryAddress(v2Log))

	v3Log := makeMinimalEthLog(pancakeSwapV3EventSig)
	assert.Equal(t, pancakeSwapV3FactoryAddr, ext.getFactoryAddress(v3Log))

	unknownLog := makeMinimalEthLog("0xunknown")
	assert.Equal(t, "", ext.getFactoryAddress(unknownLog))

	noTopicsLog := makeMinimalEthLog("")
	noTopicsLog.Topics = nil
	assert.Equal(t, "", ext.getFactoryAddress(noTopicsLog))
}
