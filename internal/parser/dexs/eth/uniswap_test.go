package eth

import (
	"context"
	"math/big"
	"testing"

	"unified-tx-parser/internal/parser/dexs/testdata"
	"unified-tx-parser/internal/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewUniswapExtractor(t *testing.T) {
	ext := NewUniswapExtractor()
	require.NotNil(t, ext)
	require.NotNil(t, ext.EVMDexExtractor)
}

func TestUniswap_GetSupportedProtocols(t *testing.T) {
	ext := NewUniswapExtractor()
	protocols := ext.GetSupportedProtocols()
	assert.ElementsMatch(t, []string{"uniswap", "uniswap-v2", "uniswap-v3"}, protocols)
}

func TestUniswap_GetSupportedChains(t *testing.T) {
	ext := NewUniswapExtractor()
	chains := ext.GetSupportedChains()
	assert.ElementsMatch(t, []types.ChainType{types.ChainTypeEthereum}, chains)
}

func TestUniswap_SetQuoteAssets(t *testing.T) {
	ext := NewUniswapExtractor()
	assets := map[string]int{"0xWETH": 100}
	ext.SetQuoteAssets(assets)
	assert.Equal(t, assets, ext.GetQuoteAssets())

	ext.SetQuoteAssets(map[string]int{})
	assert.Equal(t, assets, ext.GetQuoteAssets())
}

func TestUniswap_SupportsBlock_Ethereum(t *testing.T) {
	ext := NewUniswapExtractor()

	swapLog := testdata.MakeEVMLog(
		"0xUniswapPair",
		testdata.SwapV2EventSig,
		[]string{
			"0x0000000000000000000000001234567890abcdef1234567890abcdef12345678",
			"0x000000000000000000000000abcdefabcdefabcdefabcdefabcdefabcdefabcd",
		},
		testdata.V2SwapLogData(big.NewInt(1e18), big.NewInt(0), big.NewInt(0), big.NewInt(5e18)),
	)
	tx := testdata.TxWithEVMLogs(types.ChainTypeEthereum, "0xethtx", 100, []map[string]interface{}{swapLog})
	block := testdata.BlockWithTxs(types.ChainTypeEthereum, 100, []types.UnifiedTransaction{tx})

	assert.True(t, ext.SupportsBlock(&block))
}

func TestUniswap_SupportsBlock_BSC(t *testing.T) {
	ext := NewUniswapExtractor()

	swapLog := testdata.MakeEVMLog(
		"0xPair",
		testdata.SwapV2EventSig,
		[]string{
			"0x0000000000000000000000001234567890abcdef1234567890abcdef12345678",
			"0x000000000000000000000000abcdefabcdefabcdefabcdefabcdefabcdefabcd",
		},
		testdata.V2SwapLogData(big.NewInt(1e18), big.NewInt(0), big.NewInt(0), big.NewInt(2e18)),
	)
	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xbsctx", 100, []map[string]interface{}{swapLog})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 100, []types.UnifiedTransaction{tx})

	// Uniswap is now Ethereum-only, should NOT support BSC blocks
	assert.False(t, ext.SupportsBlock(&block))
}

func TestUniswap_SupportsBlock_WrongChain(t *testing.T) {
	ext := NewUniswapExtractor()

	block := testdata.EmptyBlock(types.ChainTypeSolana, 100)
	assert.False(t, ext.SupportsBlock(&block))

	block2 := testdata.EmptyBlock(types.ChainTypeSui, 200)
	assert.False(t, ext.SupportsBlock(&block2))
}

func TestUniswap_SupportsBlock_EmptyBlock(t *testing.T) {
	ext := NewUniswapExtractor()
	block := testdata.EmptyBlock(types.ChainTypeEthereum, 100)
	assert.False(t, ext.SupportsBlock(&block))
}

func TestUniswap_SupportsBlock_NoRelevantLogs(t *testing.T) {
	ext := NewUniswapExtractor()

	randomLog := testdata.MakeEVMLog(
		"0xSomeContract",
		"0xdeadbeef00000000000000000000000000000000000000000000000000000000",
		nil,
		[]byte{0x01},
	)
	tx := testdata.TxWithEVMLogs(types.ChainTypeEthereum, "0xtx1", 100, []map[string]interface{}{randomLog})
	block := testdata.BlockWithTxs(types.ChainTypeEthereum, 100, []types.UnifiedTransaction{tx})

	assert.False(t, ext.SupportsBlock(&block))
}

func TestUniswap_ExtractDexData_EmptyBlocks(t *testing.T) {
	ext := NewUniswapExtractor()
	ctx := context.Background()

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Transactions)
	assert.Empty(t, result.Pools)
	assert.Empty(t, result.Liquidities)
}

func TestUniswap_ExtractDexData_V2Swap(t *testing.T) {
	ext := NewUniswapExtractor()
	ctx := context.Background()

	amount0In := big.NewInt(1000000000000000000)
	amount1In := big.NewInt(0)
	amount0Out := big.NewInt(0)
	amount1Out := big.NewInt(3000000000000000000)

	swapLog := testdata.MakeEVMLog(
		"0xUniswapV2Pair",
		testdata.SwapV2EventSig,
		[]string{
			"0x0000000000000000000000001111111111111111111111111111111111111111",
			"0x0000000000000000000000002222222222222222222222222222222222222222",
		},
		testdata.V2SwapLogData(amount0In, amount1In, amount0Out, amount1Out),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeEthereum, "0xuniswaptx", 18000000, []map[string]interface{}{swapLog})
	block := testdata.BlockWithTxs(types.ChainTypeEthereum, 18000000, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Transactions, 1)

	swap := result.Transactions[0]
	assert.Equal(t, "0xuniswaptx", swap.Hash)
	assert.Equal(t, "swap", swap.Side)
	assert.Equal(t, uniswapV2FactoryAddr, swap.Factory)
	assert.NotNil(t, swap.Amount)
	assert.True(t, swap.Amount.Cmp(big.NewInt(0)) > 0)
	assert.Equal(t, int64(18000000), swap.BlockNumber)
	// FIX #2 verification: EventIndex should be logIdx (0 in this case, single log)
	assert.Equal(t, int64(0), swap.EventIndex)
	assert.Equal(t, int64(0), swap.SwapIndex)
}

func TestUniswap_ExtractDexData_V2Swap_ShortData(t *testing.T) {
	ext := NewUniswapExtractor()
	ctx := context.Background()

	shortLog := testdata.MakeEVMLog(
		"0xPair",
		testdata.SwapV2EventSig,
		[]string{
			"0x0000000000000000000000001111111111111111111111111111111111111111",
			"0x0000000000000000000000002222222222222222222222222222222222222222",
		},
		make([]byte, 64),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeEthereum, "0xshort", 100, []map[string]interface{}{shortLog})
	block := testdata.BlockWithTxs(types.ChainTypeEthereum, 100, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Empty(t, result.Transactions, "should skip V2 swap with short data")
}

func TestUniswap_ExtractDexData_V2Mint(t *testing.T) {
	ext := NewUniswapExtractor()
	ctx := context.Background()

	amount0 := big.NewInt(4000000000000000000)
	amount1 := big.NewInt(6000000000000000000)

	mintLog := testdata.MakeEVMLog(
		"0xUniswapPairMint",
		testdata.MintV2EventSig,
		[]string{"0x0000000000000000000000001234567890abcdef1234567890abcdef12345678"},
		testdata.MakeEVMLogData(amount0, amount1),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeEthereum, "0xminttx", 300, []map[string]interface{}{mintLog})
	block := testdata.BlockWithTxs(types.ChainTypeEthereum, 300, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Liquidities, 1)

	liq := result.Liquidities[0]
	assert.Equal(t, "add", liq.Side)
	assert.Equal(t, "0xminttx", liq.Hash)
	assert.Equal(t, uniswapV2FactoryAddr, liq.Factory)
	assert.Contains(t, liq.Key, "add_0")
}

func TestUniswap_ExtractDexData_V2Burn(t *testing.T) {
	ext := NewUniswapExtractor()
	ctx := context.Background()

	amount0 := big.NewInt(2000000000000000000)
	amount1 := big.NewInt(1000000000000000000)

	burnLog := testdata.MakeEVMLog(
		"0xUniswapPairBurn",
		testdata.BurnV2EventSig,
		[]string{
			"0x0000000000000000000000001234567890abcdef1234567890abcdef12345678",
			"0x000000000000000000000000abcdefabcdefabcdefabcdefabcdefabcdefabcd",
		},
		testdata.MakeEVMLogData(amount0, amount1),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeEthereum, "0xburntx", 400, []map[string]interface{}{burnLog})
	block := testdata.BlockWithTxs(types.ChainTypeEthereum, 400, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Liquidities, 1)

	liq := result.Liquidities[0]
	assert.Equal(t, "remove", liq.Side)
	assert.Equal(t, uniswapV2FactoryAddr, liq.Factory)
	assert.Contains(t, liq.Key, "remove_0")
}

// FIX #5 regression test: Pool address should come from data, not log.Address
func TestUniswap_ExtractDexData_PairCreated_PoolAddrFromData(t *testing.T) {
	ext := NewUniswapExtractor()
	ctx := context.Background()

	token0 := "0x0000000000000000000000000000000000000000000000000000000000000aaa"
	token1 := "0x0000000000000000000000000000000000000000000000000000000000000bbb"

	pairLog := testdata.MakeEVMLog(
		uniswapV2FactoryAddr, // log.Address = Factory address
		testdata.PairCreatedEventSig,
		[]string{token0, token1},
		testdata.PairCreatedLogData("0xNewPairAddr", big.NewInt(1)),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeEthereum, "0xpairtx", 500, []map[string]interface{}{pairLog})
	block := testdata.BlockWithTxs(types.ChainTypeEthereum, 500, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Pools, 1)

	pool := result.Pools[0]
	assert.Equal(t, "uniswap", pool.Protocol)
	assert.Equal(t, uniswapV2FactoryAddr, pool.Factory)
	assert.Len(t, pool.Tokens, 2)
	// FIX #5: pool.Addr should be the pair address from data, NOT the factory address
	assert.NotEqual(t, uniswapV2FactoryAddr, pool.Addr, "pool.Addr must not be the factory address")
}

func TestUniswap_ExtractDexData_V3Swap(t *testing.T) {
	ext := NewUniswapExtractor()
	ctx := context.Background()

	// V3 Swap: (int256 amount0, int256 amount1, uint160 sqrtPriceX96, uint128 liquidity, int24 tick)
	amount0 := big.NewInt(500000000000000000) // 0.5e18 positive = in
	// amount1 negative (two's complement for -1.5e18)
	amount1Raw := big.NewInt(-1500000000000000000)
	amount1TwosComplement := new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 256), amount1Raw)
	sqrtPriceX96 := new(big.Int).Mul(big.NewInt(79228162514264337), big.NewInt(1000000))
	liquidity := big.NewInt(1000000000000000000)
	tick := int32(-887220)

	v3SwapLog := testdata.MakeEVMLog(
		"0xUniswapV3Pool",
		testdata.SwapV3EventSig,
		[]string{
			"0x0000000000000000000000001111111111111111111111111111111111111111",
			"0x0000000000000000000000002222222222222222222222222222222222222222",
		},
		testdata.V3SwapLogData(amount0, amount1TwosComplement, sqrtPriceX96, liquidity, tick),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeEthereum, "0xv3swaptx", 19000000, []map[string]interface{}{v3SwapLog})
	block := testdata.BlockWithTxs(types.ChainTypeEthereum, 19000000, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Transactions, 1)

	swap := result.Transactions[0]
	assert.Equal(t, "0xv3swaptx", swap.Hash)
	assert.Equal(t, "swap", swap.Side)
	assert.Equal(t, uniswapV3FactoryAddr, swap.Factory)
	assert.NotNil(t, swap.Amount)
	assert.True(t, swap.Amount.Cmp(big.NewInt(0)) > 0)
	assert.Equal(t, int64(19000000), swap.BlockNumber)
	// V3 price should be calculated from sqrtPriceX96
	assert.True(t, swap.Price > 0, "V3 price should be positive from sqrtPriceX96")
}

// FIX #1 regression test: V3 negative amounts must be handled via toSignedInt256
func TestUniswap_ExtractDexData_V3Swap_NegativeAmount0(t *testing.T) {
	ext := NewUniswapExtractor()
	ctx := context.Background()

	// amount0 is negative (user receives token0), amount1 is positive (user pays token1)
	amount0Raw := big.NewInt(-2000000000000000000) // -2e18
	amount0TwosComplement := new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 256), amount0Raw)
	amount1 := big.NewInt(6000000000000000000) // 6e18 positive
	sqrtPriceX96 := new(big.Int).Mul(big.NewInt(79228162514264337), big.NewInt(1000000))
	liquidity := big.NewInt(1000000000000000000)

	v3SwapLog := testdata.MakeEVMLog(
		"0xV3Pool",
		testdata.SwapV3EventSig,
		[]string{
			"0x0000000000000000000000001111111111111111111111111111111111111111",
			"0x0000000000000000000000002222222222222222222222222222222222222222",
		},
		testdata.V3SwapLogData(amount0TwosComplement, amount1, sqrtPriceX96, liquidity, 0),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeEthereum, "0xv3neg", 19000001, []map[string]interface{}{v3SwapLog})
	block := testdata.BlockWithTxs(types.ChainTypeEthereum, 19000001, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Transactions, 1)

	swap := result.Transactions[0]
	// amount0 is negative (user receives), amount1 is positive (user pays)
	// amountIn should be abs(amount1) = 6e18 (the positive side = what user paid)
	assert.Equal(t, big.NewInt(6000000000000000000), swap.Amount,
		"V3 swap should use the positive amount (amount1) as amountIn when amount0 is negative")
}

func TestUniswap_ExtractDexData_V3Swap_ShortData(t *testing.T) {
	ext := NewUniswapExtractor()
	ctx := context.Background()

	shortLog := testdata.MakeEVMLog(
		"0xV3Pool",
		testdata.SwapV3EventSig,
		[]string{
			"0x0000000000000000000000001111111111111111111111111111111111111111",
			"0x0000000000000000000000002222222222222222222222222222222222222222",
		},
		make([]byte, 128),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeEthereum, "0xv3short", 100, []map[string]interface{}{shortLog})
	block := testdata.BlockWithTxs(types.ChainTypeEthereum, 100, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Empty(t, result.Transactions, "should skip V3 swap with short data")
}

// FIX #5 regression test for V3 PoolCreated
func TestUniswap_ExtractDexData_V3PoolCreated_PoolAddrFromData(t *testing.T) {
	ext := NewUniswapExtractor()
	ctx := context.Background()

	token0 := "0x0000000000000000000000000000000000000000000000000000000000000aaa"
	token1 := "0x0000000000000000000000000000000000000000000000000000000000000bbb"
	feeHash := "0x0000000000000000000000000000000000000000000000000000000000000bb8" // fee=3000

	poolCreatedLog := testdata.MakeEVMLog(
		uniswapV3FactoryAddr, // log.Address = Factory
		testdata.PoolCreatedEventSig,
		[]string{token0, token1, feeHash},
		testdata.PoolCreatedV3LogData(60, "0xNewUniV3Pool"),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeEthereum, "0xpoolcreatetx", 19000010, []map[string]interface{}{poolCreatedLog})
	block := testdata.BlockWithTxs(types.ChainTypeEthereum, 19000010, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Pools, 1)

	pool := result.Pools[0]
	assert.Equal(t, "uniswap", pool.Protocol)
	assert.Equal(t, uniswapV3FactoryAddr, pool.Factory)
	assert.Len(t, pool.Tokens, 2)
	assert.Equal(t, 3000, pool.Fee)
	// FIX #5: pool.Addr should be from data[32:64], NOT log.Address (factory)
	assert.NotEqual(t, uniswapV3FactoryAddr, pool.Addr, "pool.Addr must not be the factory address")
}

func TestUniswap_isUniswapLog(t *testing.T) {
	ext := NewUniswapExtractor()

	tests := []struct {
		name     string
		topic0   string
		expected bool
	}{
		{"V2 Swap", swapV2EventSig, true},
		{"V3 Swap", swapV3EventSig, true},
		{"V2 Mint", mintV2EventSig, true},
		{"V3 Mint", mintV3EventSig, true},
		{"V2 Burn", burnV2EventSig, true},
		{"V3 Burn", burnV3EventSig, true},
		{"PairCreated", pairCreatedEventSig, true},
		{"PoolCreated", poolCreatedEventSig, true},
		{"Unknown", "0xdeadbeef00000000000000000000000000000000000000000000000000000000", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := makeMinimalEthLog(tt.topic0)
			assert.Equal(t, tt.expected, ext.isUniswapLog(log))
		})
	}

	t.Run("No topics", func(t *testing.T) {
		log := makeMinimalEthLog("")
		log.Topics = nil
		assert.False(t, ext.isUniswapLog(log))
	})
}

func TestUniswap_ExtractDexData_UnsupportedChainSkipped(t *testing.T) {
	ext := NewUniswapExtractor()
	ctx := context.Background()

	block := testdata.EmptyBlock(types.ChainTypeSolana, 100)
	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Empty(t, result.Transactions)
}

func TestUniswap_ExtractDexData_NoRawData(t *testing.T) {
	ext := NewUniswapExtractor()
	ctx := context.Background()

	tx := types.UnifiedTransaction{
		TxHash:    "0xnilraw",
		ChainType: types.ChainTypeEthereum,
		ChainID:   "1",
		RawData:   nil,
	}
	block := testdata.BlockWithTxs(types.ChainTypeEthereum, 100, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Empty(t, result.Transactions)
}

func TestUniswap_getLogType(t *testing.T) {
	ext := NewUniswapExtractor()

	tests := []struct {
		name     string
		topic0   string
		expected string
	}{
		{"V2 swap", swapV2EventSig, "swap_v2"},
		{"V3 swap", swapV3EventSig, "swap_v3"},
		{"V2 mint", mintV2EventSig, "mint"},
		{"V3 mint", mintV3EventSig, "mint"},
		{"V2 burn", burnV2EventSig, "burn"},
		{"V3 burn", burnV3EventSig, "burn"},
		{"pair created", pairCreatedEventSig, "pair_created"},
		{"pool created", poolCreatedEventSig, "pool_created"},
		{"unknown", "0xunknown", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := makeMinimalEthLog(tt.topic0)
			assert.Equal(t, tt.expected, ext.getLogType(log))
		})
	}
}

func TestUniswap_getFactoryAddress(t *testing.T) {
	ext := NewUniswapExtractor()

	v2Log := makeMinimalEthLog(swapV2EventSig)
	assert.Equal(t, uniswapV2FactoryAddr, ext.getFactoryAddress(v2Log))

	v3Log := makeMinimalEthLog(swapV3EventSig)
	assert.Equal(t, uniswapV3FactoryAddr, ext.getFactoryAddress(v3Log))

	noTopics := makeMinimalEthLog("")
	noTopics.Topics = nil
	assert.Equal(t, "", ext.getFactoryAddress(noTopics))
}

// FIX #2 regression test: SwapIndex must increment for multiple swaps in same tx
func TestUniswap_ExtractDexData_MultipleSwaps_SwapIndexIncrement(t *testing.T) {
	ext := NewUniswapExtractor()
	ctx := context.Background()

	// Three V2 swap logs in a single transaction (multi-hop)
	swap1 := testdata.MakeEVMLog(
		"0xPairA",
		testdata.SwapV2EventSig,
		[]string{
			"0x0000000000000000000000001111111111111111111111111111111111111111",
			"0x0000000000000000000000002222222222222222222222222222222222222222",
		},
		testdata.V2SwapLogData(big.NewInt(1e18), big.NewInt(0), big.NewInt(0), big.NewInt(2e18)),
	)
	swap2 := testdata.MakeEVMLog(
		"0xPairB",
		testdata.SwapV2EventSig,
		[]string{
			"0x0000000000000000000000003333333333333333333333333333333333333333",
			"0x0000000000000000000000004444444444444444444444444444444444444444",
		},
		testdata.V2SwapLogData(big.NewInt(2e18), big.NewInt(0), big.NewInt(0), big.NewInt(5e18)),
	)
	swap3 := testdata.MakeEVMLog(
		"0xPairC",
		testdata.SwapV2EventSig,
		[]string{
			"0x0000000000000000000000005555555555555555555555555555555555555555",
			"0x0000000000000000000000006666666666666666666666666666666666666666",
		},
		testdata.V2SwapLogData(big.NewInt(5e18), big.NewInt(0), big.NewInt(0), new(big.Int).Mul(big.NewInt(1e18), big.NewInt(10))),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeEthereum, "0xmultihop", 18000001, []map[string]interface{}{swap1, swap2, swap3})
	block := testdata.BlockWithTxs(types.ChainTypeEthereum, 18000001, []types.UnifiedTransaction{tx})

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

func TestUniswap_ExtractDexData_MultipleEventTypes(t *testing.T) {
	ext := NewUniswapExtractor()
	ctx := context.Background()

	swapLog := testdata.MakeEVMLog(
		"0xPairA",
		testdata.SwapV2EventSig,
		[]string{
			"0x0000000000000000000000001111111111111111111111111111111111111111",
			"0x0000000000000000000000002222222222222222222222222222222222222222",
		},
		testdata.V2SwapLogData(big.NewInt(1e18), big.NewInt(0), big.NewInt(0), big.NewInt(2e18)),
	)
	mintLog := testdata.MakeEVMLog(
		"0xPairB",
		testdata.MintV2EventSig,
		[]string{"0x0000000000000000000000003333333333333333333333333333333333333333"},
		testdata.MakeEVMLogData(big.NewInt(5e18), big.NewInt(3e18)),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeEthereum, "0xmulti", 600, []map[string]interface{}{swapLog, mintLog})
	block := testdata.BlockWithTxs(types.ChainTypeEthereum, 600, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Len(t, result.Transactions, 1)
	assert.Len(t, result.Liquidities, 1)
}

// V3 Mint event test
func TestUniswap_ExtractDexData_V3Mint(t *testing.T) {
	ext := NewUniswapExtractor()
	ctx := context.Background()

	mintLog := testdata.MakeEVMLog(
		"0xV3Pool",
		testdata.MintV3EventSig,
		[]string{
			"0x0000000000000000000000001234567890abcdef1234567890abcdef12345678", // owner
			"0x000000000000000000000000000000000000000000000000000000000000c350", // tickLower
			"0x0000000000000000000000000000000000000000000000000000000000013880", // tickUpper
		},
		testdata.V3MintLogData("0xSender", big.NewInt(1e18), big.NewInt(2e18), big.NewInt(3e18)),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeEthereum, "0xv3mint", 19000020, []map[string]interface{}{mintLog})
	block := testdata.BlockWithTxs(types.ChainTypeEthereum, 19000020, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Liquidities, 1)

	liq := result.Liquidities[0]
	assert.Equal(t, "add", liq.Side)
	assert.Equal(t, uniswapV3FactoryAddr, liq.Factory)
	// V3 Mint: amount0 from offset 64, amount1 from offset 96
	expectedTotal := new(big.Int).Add(big.NewInt(2e18), big.NewInt(3e18))
	assert.Equal(t, expectedTotal, liq.Amount)
}

// V3 Burn event test
func TestUniswap_ExtractDexData_V3Burn(t *testing.T) {
	ext := NewUniswapExtractor()
	ctx := context.Background()

	burnLog := testdata.MakeEVMLog(
		"0xV3Pool",
		testdata.BurnV3EventSig,
		[]string{
			"0x0000000000000000000000001234567890abcdef1234567890abcdef12345678", // owner
			"0x000000000000000000000000000000000000000000000000000000000000c350", // tickLower
			"0x0000000000000000000000000000000000000000000000000000000000013880", // tickUpper
		},
		testdata.V3BurnLogData(big.NewInt(1e18), big.NewInt(4e18), big.NewInt(5e18)),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeEthereum, "0xv3burn", 19000021, []map[string]interface{}{burnLog})
	block := testdata.BlockWithTxs(types.ChainTypeEthereum, 19000021, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Liquidities, 1)

	liq := result.Liquidities[0]
	assert.Equal(t, "remove", liq.Side)
	assert.Equal(t, uniswapV3FactoryAddr, liq.Factory)
	// V3 Burn: amount0 from offset 32, amount1 from offset 64
	expectedTotal := new(big.Int).Add(big.NewInt(4e18), big.NewInt(5e18))
	assert.Equal(t, expectedTotal, liq.Amount)
}
