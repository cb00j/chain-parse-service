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

func TestNewFourMemeExtractor(t *testing.T) {
	ext := NewFourMemeExtractor()
	require.NotNil(t, ext)
}

func TestFourMeme_GetSupportedProtocols(t *testing.T) {
	ext := NewFourMemeExtractor()
	protocols := ext.GetSupportedProtocols()
	assert.Equal(t, []string{"fourmeme"}, protocols)
}

func TestFourMeme_GetSupportedChains(t *testing.T) {
	ext := NewFourMemeExtractor()
	chains := ext.GetSupportedChains()
	assert.Equal(t, []types.ChainType{types.ChainTypeBSC}, chains)
}

func TestFourMeme_SetQuoteAssets(t *testing.T) {
	ext := NewFourMemeExtractor()
	assets := map[string]int{"0xWBNB": 50}
	ext.SetQuoteAssets(assets)
	assert.Equal(t, assets, ext.GetQuoteAssets())

	ext.SetQuoteAssets(map[string]int{})
	assert.Equal(t, assets, ext.GetQuoteAssets())
}

func TestFourMeme_SupportsBlock_BSC(t *testing.T) {
	ext := NewFourMemeExtractor()

	purchaseLog := testdata.MakeEVMLog(
		testdata.FourMemeV2Addr,
		testdata.FourMemeV2TokenPurchaseSig,
		nil,
		testdata.FourMemeV2PurchaseLogData(
			"0xTokenAddr",
			"0xAccountAddr",
			big.NewInt(1e15),  // price
			big.NewInt(1e18),  // amount
			big.NewInt(1e16),  // cost
			big.NewInt(1e14),                                        // fee
			new(big.Int).Mul(big.NewInt(1e10), big.NewInt(1e10)),    // offers (1e20)
			new(big.Int).Mul(big.NewInt(1e10), big.NewInt(1e9)),     // funds (1e19)
		),
	)
	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xtx1", 100, []map[string]interface{}{purchaseLog})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 100, []types.UnifiedTransaction{tx})

	assert.True(t, ext.SupportsBlock(&block))
}

func TestFourMeme_SupportsBlock_WrongChain(t *testing.T) {
	ext := NewFourMemeExtractor()

	block := testdata.EmptyBlock(types.ChainTypeEthereum, 100)
	assert.False(t, ext.SupportsBlock(&block))

	block2 := testdata.EmptyBlock(types.ChainTypeSolana, 200)
	assert.False(t, ext.SupportsBlock(&block2))
}

func TestFourMeme_SupportsBlock_EmptyBlock(t *testing.T) {
	ext := NewFourMemeExtractor()
	block := testdata.EmptyBlock(types.ChainTypeBSC, 100)
	assert.False(t, ext.SupportsBlock(&block))
}

func TestFourMeme_SupportsBlock_NonFourMemeLogs(t *testing.T) {
	ext := NewFourMemeExtractor()

	// PancakeSwap log on BSC - should not match FourMeme
	pancakeLog := testdata.MakeEVMLog(
		"0xSomePancakePair",
		testdata.SwapV2EventSig,
		[]string{
			"0x0000000000000000000000001111111111111111111111111111111111111111",
			"0x0000000000000000000000002222222222222222222222222222222222222222",
		},
		testdata.V2SwapLogData(big.NewInt(1e18), big.NewInt(0), big.NewInt(0), big.NewInt(2e18)),
	)
	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xtx1", 100, []map[string]interface{}{pancakeLog})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 100, []types.UnifiedTransaction{tx})

	assert.False(t, ext.SupportsBlock(&block))
}

func TestFourMeme_ExtractDexData_EmptyBlocks(t *testing.T) {
	ext := NewFourMemeExtractor()
	ctx := context.Background()

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Transactions)
	assert.Empty(t, result.Pools)
	assert.Empty(t, result.Liquidities)
}

func TestFourMeme_ExtractDexData_V2Purchase(t *testing.T) {
	ext := NewFourMemeExtractor()
	ctx := context.Background()

	tokenAddr := "0x1234567890AbcdEF1234567890aBcDeF12345678"
	accountAddr := "0xaBcDeFaBcDeFaBcDeFaBcDeFaBcDeFaBcDeFaBcD"

	purchaseLog := testdata.MakeEVMLog(
		testdata.FourMemeV2Addr,
		testdata.FourMemeV2TokenPurchaseSig,
		nil,
		testdata.FourMemeV2PurchaseLogData(
			tokenAddr,
			accountAddr,
			big.NewInt(1000000000000000), // price: 0.001 ETH
			big.NewInt(1000000000000000000), // amount: 1e18
			big.NewInt(500000000000000),    // cost
			big.NewInt(10000000000000),     // fee
			big.NewInt(9000000000000000000), // offers
			big.NewInt(1000000000000000000), // funds
		),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xpurchasetx", 45000000, []map[string]interface{}{purchaseLog})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 45000000, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Transactions, 1)

	purchase := result.Transactions[0]
	assert.Equal(t, "0xpurchasetx", purchase.Hash)
	assert.Equal(t, "buy", purchase.Side)
	assert.Equal(t, fourMemeV2Addr, purchase.Factory)
	assert.Equal(t, fourMemeV2Addr, purchase.Router)
	assert.NotNil(t, purchase.Amount)
	assert.True(t, purchase.Amount.Cmp(big.NewInt(0)) > 0)
	assert.Equal(t, int64(45000000), purchase.BlockNumber)
	require.NotNil(t, purchase.Extra)
	assert.Equal(t, "buy", purchase.Extra.Type)
}

func TestFourMeme_ExtractDexData_V2Sale(t *testing.T) {
	ext := NewFourMemeExtractor()
	ctx := context.Background()

	saleLog := testdata.MakeEVMLog(
		testdata.FourMemeV2Addr,
		testdata.FourMemeV2TokenSaleSig,
		nil,
		testdata.FourMemeV2PurchaseLogData( // same layout as purchase
			"0xTokenAddr",
			"0xSellerAddr",
			big.NewInt(1000000000000000),
			big.NewInt(500000000000000000),
			big.NewInt(400000000000000),
			big.NewInt(5000000000000),
			new(big.Int).Mul(big.NewInt(95), big.NewInt(1e17)),
			big.NewInt(600000000000000000),
		),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xsaletx", 45000001, []map[string]interface{}{saleLog})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 45000001, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Transactions, 1)

	sale := result.Transactions[0]
	assert.Equal(t, "sell", sale.Side)
	assert.Equal(t, fourMemeV2Addr, sale.Factory)
	require.NotNil(t, sale.Extra)
	assert.Equal(t, "sell", sale.Extra.Type)
}

func TestFourMeme_ExtractDexData_V1Purchase(t *testing.T) {
	ext := NewFourMemeExtractor()
	ctx := context.Background()

	v1PurchaseLog := testdata.MakeEVMLog(
		testdata.FourMemeV1Addr,
		testdata.FourMemeV1TokenPurchaseSig,
		nil,
		testdata.FourMemeV1PurchaseLogData(
			"0xV1Token",
			"0xBuyer",
			big.NewInt(1000000000000000000), // tokenAmount
			big.NewInt(500000000000000000),  // etherAmount
		),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xv1buy", 30000000, []map[string]interface{}{v1PurchaseLog})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 30000000, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Transactions, 1)

	purchase := result.Transactions[0]
	assert.Equal(t, "buy", purchase.Side)
	assert.Equal(t, fourMemeV1Addr, purchase.Factory)
	assert.Equal(t, fourMemeV1Addr, purchase.Router)
}

func TestFourMeme_ExtractDexData_V1Sale(t *testing.T) {
	ext := NewFourMemeExtractor()
	ctx := context.Background()

	v1SaleLog := testdata.MakeEVMLog(
		testdata.FourMemeV1Addr,
		testdata.FourMemeV1TokenSaleSig,
		nil,
		testdata.FourMemeV1PurchaseLogData( // same layout
			"0xV1Token",
			"0xSeller",
			big.NewInt(500000000000000000),
			big.NewInt(200000000000000000),
		),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xv1sell", 30000001, []map[string]interface{}{v1SaleLog})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 30000001, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Transactions, 1)

	sale := result.Transactions[0]
	assert.Equal(t, "sell", sale.Side)
	assert.Equal(t, fourMemeV1Addr, sale.Factory)
}

func TestFourMeme_ExtractDexData_V2TokenCreate(t *testing.T) {
	ext := NewFourMemeExtractor()
	ctx := context.Background()

	createLog := testdata.MakeEVMLog(
		testdata.FourMemeV2Addr,
		testdata.FourMemeV2TokenCreateSig,
		nil,
		testdata.FourMemeV2TokenCreateLogData("0xCreatorAddr", "0xNewTokenAddr"),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xcreatetx", 45000010, []map[string]interface{}{createLog})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 45000010, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Pools, 1)

	pool := result.Pools[0]
	assert.Equal(t, "fourmeme", pool.Protocol)
	assert.Equal(t, fourMemeV2Addr, pool.Factory)
	assert.NotNil(t, pool.Extra)
	assert.Equal(t, "0xcreatetx", pool.Extra.Hash)
	assert.Contains(t, pool.Args, "version")
	assert.Equal(t, 2, pool.Args["version"])
}

func TestFourMeme_ExtractDexData_V2LiquidityAdded(t *testing.T) {
	ext := NewFourMemeExtractor()
	ctx := context.Background()

	// LiquidityAdded(address base, uint256 offers, address quote, uint256 funds)
	data := make([]byte, 0, 128)
	data = append(data, testdata.MakeAddressData("0xBaseToken")...)
	offersBytes := big.NewInt(1000000000000000000).Bytes()
	offersPadded := make([]byte, 32)
	copy(offersPadded[32-len(offersBytes):], offersBytes)
	data = append(data, offersPadded...)
	data = append(data, testdata.MakeAddressData("0x0000000000000000000000000000000000000000")...) // quote = BNB
	fundsBytes := big.NewInt(500000000000000000).Bytes()
	fundsPadded := make([]byte, 32)
	copy(fundsPadded[32-len(fundsBytes):], fundsBytes)
	data = append(data, fundsPadded...)

	liqLog := testdata.MakeEVMLog(
		testdata.FourMemeV2Addr,
		testdata.FourMemeV2LiquidityAddSig,
		nil,
		data,
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xliqtx", 45000020, []map[string]interface{}{liqLog})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 45000020, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Liquidities, 1)

	liq := result.Liquidities[0]
	assert.Equal(t, "add", liq.Side)
	assert.Equal(t, fourMemeV2Addr, liq.Factory)
	assert.Equal(t, "0xliqtx", liq.Hash)
	assert.NotNil(t, liq.Amount)
	assert.True(t, liq.Amount.Cmp(big.NewInt(0)) > 0)
}

func TestFourMeme_ExtractDexData_V1TokenCreate(t *testing.T) {
	ext := NewFourMemeExtractor()
	ctx := context.Background()

	createLog := testdata.MakeEVMLog(
		testdata.FourMemeV1Addr,
		testdata.FourMemeV1TokenCreateSig,
		nil,
		testdata.FourMemeV1TokenCreateLogData("0xV1Creator", "0xV1NewToken"),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xv1createtx", 30000010, []map[string]interface{}{createLog})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 30000010, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Pools, 1)

	pool := result.Pools[0]
	assert.Equal(t, "fourmeme", pool.Protocol)
	assert.Equal(t, fourMemeV1Addr, pool.Factory)
	assert.NotNil(t, pool.Extra)
	assert.Equal(t, "0xv1createtx", pool.Extra.Hash)
	assert.Contains(t, pool.Args, "version")
	assert.Equal(t, 1, pool.Args["version"])
}

func TestFourMeme_ExtractDexData_V1TokenCreate_ShortData(t *testing.T) {
	ext := NewFourMemeExtractor()
	ctx := context.Background()

	// V1 TokenCreate needs >= 224 bytes, provide only 128
	shortLog := testdata.MakeEVMLog(
		testdata.FourMemeV1Addr,
		testdata.FourMemeV1TokenCreateSig,
		nil,
		make([]byte, 128),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xv1shortcreate", 100, []map[string]interface{}{shortLog})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 100, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Empty(t, result.Pools, "should skip V1 TokenCreate with short data")
}

func TestFourMeme_ExtractDexData_V2TokenCreate_ShortData(t *testing.T) {
	ext := NewFourMemeExtractor()
	ctx := context.Background()

	// V2 TokenCreate needs >= 256 bytes, provide only 128
	shortLog := testdata.MakeEVMLog(
		testdata.FourMemeV2Addr,
		testdata.FourMemeV2TokenCreateSig,
		nil,
		make([]byte, 128),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xv2shortcreate", 100, []map[string]interface{}{shortLog})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 100, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Empty(t, result.Pools, "should skip V2 TokenCreate with short data")
}

func TestFourMeme_ExtractDexData_V1Purchase_ShortData(t *testing.T) {
	ext := NewFourMemeExtractor()
	ctx := context.Background()

	// V1 purchase needs >= 128 bytes, provide only 64
	shortLog := testdata.MakeEVMLog(
		testdata.FourMemeV1Addr,
		testdata.FourMemeV1TokenPurchaseSig,
		nil,
		make([]byte, 64),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xv1shortbuy", 100, []map[string]interface{}{shortLog})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 100, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Empty(t, result.Transactions, "should skip V1 purchase with short data")
}

func TestFourMeme_ExtractDexData_V2LiquidityAdded_ShortData(t *testing.T) {
	ext := NewFourMemeExtractor()
	ctx := context.Background()

	// LiquidityAdded needs >= 128 bytes, provide only 64
	shortLog := testdata.MakeEVMLog(
		testdata.FourMemeV2Addr,
		testdata.FourMemeV2LiquidityAddSig,
		nil,
		make([]byte, 64),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xshortliq", 100, []map[string]interface{}{shortLog})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 100, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Empty(t, result.Liquidities, "should skip LiquidityAdded with short data")
}

func TestFourMeme_isFourMemeLog(t *testing.T) {
	ext := NewFourMemeExtractor()

	tests := []struct {
		name     string
		addr     string
		topic0   string
		expected bool
	}{
		{"V2 Purchase", testdata.FourMemeV2Addr, testdata.FourMemeV2TokenPurchaseSig, true},
		{"V2 Sale", testdata.FourMemeV2Addr, testdata.FourMemeV2TokenSaleSig, true},
		{"V2 TokenCreate", testdata.FourMemeV2Addr, testdata.FourMemeV2TokenCreateSig, true},
		{"V2 LiquidityAdded", testdata.FourMemeV2Addr, testdata.FourMemeV2LiquidityAddSig, true},
		{"V1 Purchase", testdata.FourMemeV1Addr, testdata.FourMemeV1TokenPurchaseSig, true},
		{"V1 Sale", testdata.FourMemeV1Addr, testdata.FourMemeV1TokenSaleSig, true},
		{"V1 TokenCreate", testdata.FourMemeV1Addr, testdata.FourMemeV1TokenCreateSig, true},
		{"Wrong addr", "0xUnknownContract", testdata.FourMemeV2TokenPurchaseSig, false},
		{"Wrong topic", testdata.FourMemeV2Addr, "0xdeadbeef00000000000000000000000000000000000000000000000000000000", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := makeMinimalEthLogWithAddr(tt.addr, tt.topic0)
			assert.Equal(t, tt.expected, ext.isFourMemeLog(log))
		})
	}

	t.Run("No topics", func(t *testing.T) {
		log := makeMinimalEthLogWithAddr(testdata.FourMemeV2Addr, "")
		log.Topics = nil
		assert.False(t, ext.isFourMemeLog(log))
	})
}

func TestFourMeme_ExtractDexData_AllV2EventTypes(t *testing.T) {
	ext := NewFourMemeExtractor()
	ctx := context.Background()

	// All V2 event types in a single transaction
	purchaseLog := testdata.MakeEVMLog(
		testdata.FourMemeV2Addr,
		testdata.FourMemeV2TokenPurchaseSig,
		nil,
		testdata.FourMemeV2PurchaseLogData(
			"0xToken1", "0xBuyer",
			big.NewInt(1e15), big.NewInt(1e18), big.NewInt(1e16),
			big.NewInt(1e14), big.NewInt(9e18), big.NewInt(1e18),
		),
	)
	saleLog := testdata.MakeEVMLog(
		testdata.FourMemeV2Addr,
		testdata.FourMemeV2TokenSaleSig,
		nil,
		testdata.FourMemeV2PurchaseLogData(
			"0xToken1", "0xSeller",
			big.NewInt(1e15), big.NewInt(5e17), big.NewInt(5e15),
			big.NewInt(5e13), new(big.Int).Mul(big.NewInt(95), big.NewInt(1e17)), big.NewInt(6e17),
		),
	)
	createLog := testdata.MakeEVMLog(
		testdata.FourMemeV2Addr,
		testdata.FourMemeV2TokenCreateSig,
		nil,
		testdata.FourMemeV2TokenCreateLogData("0xCreator", "0xToken1"),
	)

	// LiquidityAdded data
	liqData := make([]byte, 0, 128)
	liqData = append(liqData, testdata.MakeAddressData("0xToken1")...)
	offersBytes := big.NewInt(1e18).Bytes()
	offersPadded := make([]byte, 32)
	copy(offersPadded[32-len(offersBytes):], offersBytes)
	liqData = append(liqData, offersPadded...)
	liqData = append(liqData, testdata.MakeAddressData("0x0000000000000000000000000000000000000000")...)
	fundsBytes := big.NewInt(5e17).Bytes()
	fundsPadded := make([]byte, 32)
	copy(fundsPadded[32-len(fundsBytes):], fundsBytes)
	liqData = append(liqData, fundsPadded...)

	liqLog := testdata.MakeEVMLog(
		testdata.FourMemeV2Addr,
		testdata.FourMemeV2LiquidityAddSig,
		nil,
		liqData,
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xallevents", 46000100, []map[string]interface{}{
		purchaseLog, saleLog, createLog, liqLog,
	})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 46000100, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Len(t, result.Transactions, 2, "should have 1 purchase + 1 sale")
	assert.Len(t, result.Pools, 1, "should have 1 token create")
	assert.Len(t, result.Liquidities, 1, "should have 1 liquidity add")
}

func TestFourMeme_ExtractDexData_V2Purchase_ShortData(t *testing.T) {
	ext := NewFourMemeExtractor()
	ctx := context.Background()

	// V2 purchase needs 256 bytes, provide only 128
	shortLog := testdata.MakeEVMLog(
		testdata.FourMemeV2Addr,
		testdata.FourMemeV2TokenPurchaseSig,
		nil,
		make([]byte, 128),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xshort", 100, []map[string]interface{}{shortLog})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 100, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Empty(t, result.Transactions, "should skip purchase with short data")
}

func TestFourMeme_ExtractDexData_UnsupportedChain(t *testing.T) {
	ext := NewFourMemeExtractor()
	ctx := context.Background()

	block := testdata.EmptyBlock(types.ChainTypeEthereum, 100)
	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Empty(t, result.Transactions)
}

func TestFourMeme_ExtractDexData_NoRawData(t *testing.T) {
	ext := NewFourMemeExtractor()
	ctx := context.Background()

	tx := types.UnifiedTransaction{
		TxHash:    "0xnil",
		ChainType: types.ChainTypeBSC,
		ChainID:   "56",
		RawData:   nil,
	}
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 100, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Empty(t, result.Transactions)
}

func TestFourMeme_getContractVersion(t *testing.T) {
	ext := NewFourMemeExtractor()

	tests := []struct {
		name    string
		addr    string
		version int
	}{
		{"V1 contract", testdata.FourMemeV1Addr, 1},
		{"V2 contract", testdata.FourMemeV2Addr, 2},
		{"unknown contract", "0xUnknownAddr", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := makeMinimalEthLogWithAddr(tt.addr, testdata.FourMemeV2TokenPurchaseSig)
			assert.Equal(t, tt.version, ext.getContractVersion(log))
		})
	}
}

// Test: Mixed V1+V2 events in a single transaction
func TestFourMeme_ExtractDexData_MixedV1V2Events(t *testing.T) {
	ext := NewFourMemeExtractor()
	ctx := context.Background()

	// V2 purchase log
	v2PurchaseLog := testdata.MakeEVMLog(
		testdata.FourMemeV2Addr,
		testdata.FourMemeV2TokenPurchaseSig,
		nil,
		testdata.FourMemeV2PurchaseLogData(
			"0xV2Token", "0xBuyerV2",
			big.NewInt(1e15), big.NewInt(1e18), big.NewInt(1e16),
			big.NewInt(1e14), big.NewInt(9e18), big.NewInt(1e18),
		),
	)

	// V1 purchase log
	v1PurchaseLog := testdata.MakeEVMLog(
		testdata.FourMemeV1Addr,
		testdata.FourMemeV1TokenPurchaseSig,
		nil,
		testdata.FourMemeV1PurchaseLogData(
			"0xV1Token", "0xBuyerV1",
			big.NewInt(1e18), big.NewInt(5e17),
		),
	)

	// V2 sale log
	v2SaleLog := testdata.MakeEVMLog(
		testdata.FourMemeV2Addr,
		testdata.FourMemeV2TokenSaleSig,
		nil,
		testdata.FourMemeV2PurchaseLogData(
			"0xV2Token", "0xSellerV2",
			big.NewInt(1e15), big.NewInt(5e17), big.NewInt(5e15),
			big.NewInt(5e13), new(big.Int).Mul(big.NewInt(95), big.NewInt(1e17)), big.NewInt(6e17),
		),
	)

	// V1 sale log
	v1SaleLog := testdata.MakeEVMLog(
		testdata.FourMemeV1Addr,
		testdata.FourMemeV1TokenSaleSig,
		nil,
		testdata.FourMemeV1PurchaseLogData(
			"0xV1Token", "0xSellerV1",
			big.NewInt(5e17), big.NewInt(2e17),
		),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xmixedv1v2", 46500000, []map[string]interface{}{
		v2PurchaseLog, v1PurchaseLog, v2SaleLog, v1SaleLog,
	})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 46500000, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Transactions, 4, "should have 2 V2 + 2 V1 transactions")

	// V2 purchase
	assert.Equal(t, "buy", result.Transactions[0].Side)
	assert.Equal(t, fourMemeV2Addr, result.Transactions[0].Factory)

	// V1 purchase
	assert.Equal(t, "buy", result.Transactions[1].Side)
	assert.Equal(t, fourMemeV1Addr, result.Transactions[1].Factory)

	// V2 sale
	assert.Equal(t, "sell", result.Transactions[2].Side)
	assert.Equal(t, fourMemeV2Addr, result.Transactions[2].Factory)

	// V1 sale
	assert.Equal(t, "sell", result.Transactions[3].Side)
	assert.Equal(t, fourMemeV1Addr, result.Transactions[3].Factory)

	// SwapIndex should increment across all swap-type events: 0, 1, 2, 3
	assert.Equal(t, int64(0), result.Transactions[0].SwapIndex)
	assert.Equal(t, int64(1), result.Transactions[1].SwapIndex)
	assert.Equal(t, int64(2), result.Transactions[2].SwapIndex)
	assert.Equal(t, int64(3), result.Transactions[3].SwapIndex)

	// EventIndex should match logIdx: 0, 1, 2, 3
	assert.Equal(t, int64(0), result.Transactions[0].EventIndex)
	assert.Equal(t, int64(1), result.Transactions[1].EventIndex)
	assert.Equal(t, int64(2), result.Transactions[2].EventIndex)
	assert.Equal(t, int64(3), result.Transactions[3].EventIndex)
}

// Test: FourMeme SwapIndex increments correctly for multiple purchases
func TestFourMeme_ExtractDexData_MultipleSwaps_SwapIndexIncrement(t *testing.T) {
	ext := NewFourMemeExtractor()
	ctx := context.Background()

	purchase1 := testdata.MakeEVMLog(
		testdata.FourMemeV2Addr,
		testdata.FourMemeV2TokenPurchaseSig,
		nil,
		testdata.FourMemeV2PurchaseLogData(
			"0xToken1", "0xBuyer1",
			big.NewInt(1e15), big.NewInt(1e18), big.NewInt(1e16),
			big.NewInt(1e14), big.NewInt(9e18), big.NewInt(1e18),
		),
	)
	purchase2 := testdata.MakeEVMLog(
		testdata.FourMemeV2Addr,
		testdata.FourMemeV2TokenPurchaseSig,
		nil,
		testdata.FourMemeV2PurchaseLogData(
			"0xToken2", "0xBuyer2",
			big.NewInt(2e15), big.NewInt(2e18), big.NewInt(2e16),
			big.NewInt(2e14), big.NewInt(8e18), big.NewInt(2e18),
		),
	)
	purchase3 := testdata.MakeEVMLog(
		testdata.FourMemeV2Addr,
		testdata.FourMemeV2TokenPurchaseSig,
		nil,
		testdata.FourMemeV2PurchaseLogData(
			"0xToken3", "0xBuyer3",
			big.NewInt(3e15), big.NewInt(3e18), big.NewInt(3e16),
			big.NewInt(3e14), big.NewInt(7e18), big.NewInt(3e18),
		),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xfmswapidx", 46600000, []map[string]interface{}{
		purchase1, purchase2, purchase3,
	})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 46600000, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Transactions, 3)

	assert.Equal(t, int64(0), result.Transactions[0].SwapIndex)
	assert.Equal(t, int64(1), result.Transactions[1].SwapIndex)
	assert.Equal(t, int64(2), result.Transactions[2].SwapIndex)
}

func TestFourMeme_ExtractDexData_MultipleEventsInTx(t *testing.T) {
	ext := NewFourMemeExtractor()
	ctx := context.Background()

	purchaseLog := testdata.MakeEVMLog(
		testdata.FourMemeV2Addr,
		testdata.FourMemeV2TokenPurchaseSig,
		nil,
		testdata.FourMemeV2PurchaseLogData(
			"0xToken1",
			"0xBuyer1",
			big.NewInt(1e15), big.NewInt(1e18), big.NewInt(1e16),
			big.NewInt(1e14), big.NewInt(9e18), big.NewInt(1e18),
		),
	)
	saleLog := testdata.MakeEVMLog(
		testdata.FourMemeV2Addr,
		testdata.FourMemeV2TokenSaleSig,
		nil,
		testdata.FourMemeV2PurchaseLogData(
			"0xToken2",
			"0xSeller1",
			big.NewInt(1e15), big.NewInt(5e17), big.NewInt(5e15),
			big.NewInt(5e13), new(big.Int).Mul(big.NewInt(95), big.NewInt(1e17)), big.NewInt(6e17),
		),
	)

	tx := testdata.TxWithEVMLogs(types.ChainTypeBSC, "0xmultitx", 46000000, []map[string]interface{}{purchaseLog, saleLog})
	block := testdata.BlockWithTxs(types.ChainTypeBSC, 46000000, []types.UnifiedTransaction{tx})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Len(t, result.Transactions, 2)

	assert.Equal(t, "buy", result.Transactions[0].Side)
	assert.Equal(t, "sell", result.Transactions[1].Side)
}
