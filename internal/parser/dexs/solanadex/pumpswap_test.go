package solanadex

import (
	"context"
	"fmt"
	"math/big"
	"testing"

	"unified-tx-parser/internal/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Test helpers for PumpSwap ---

// buildPumpSwapBuyEventData builds a valid PumpSwap buy event payload (with discriminator).
// Layout: base_amount_out(8) + quote_amount_in(8) + lp_fee(8) + protocol_fee(8) + pool(32) + user(32) + base_mint(32) + quote_mint(32) = 160
func buildPumpSwapBuyEventData(baseAmountOut, quoteAmountIn, lpFee, protocolFee uint64, pool, user, baseMint, quoteMint byte) []byte {
	buf := make([]byte, 8+160)
	copy(buf[:8], pumpSwapBuyDiscriminator)
	off := 8
	off = putU64LE(buf, off, baseAmountOut)
	off = putU64LE(buf, off, quoteAmountIn)
	off = putU64LE(buf, off, lpFee)
	off = putU64LE(buf, off, protocolFee)
	off = putPubkey(buf, off, pool)
	off = putPubkey(buf, off, user)
	off = putPubkey(buf, off, baseMint)
	off = putPubkey(buf, off, quoteMint)
	_ = off
	return buf
}

// buildPumpSwapSellEventData builds a valid PumpSwap sell event payload (with discriminator).
// Layout: base_amount_in(8) + quote_amount_out(8) + lp_fee(8) + protocol_fee(8) + pool(32) + user(32) + base_mint(32) + quote_mint(32) = 160
func buildPumpSwapSellEventData(baseAmountIn, quoteAmountOut, lpFee, protocolFee uint64, pool, user, baseMint, quoteMint byte) []byte {
	buf := make([]byte, 8+160)
	copy(buf[:8], pumpSwapSellDiscriminator)
	off := 8
	off = putU64LE(buf, off, baseAmountIn)
	off = putU64LE(buf, off, quoteAmountOut)
	off = putU64LE(buf, off, lpFee)
	off = putU64LE(buf, off, protocolFee)
	off = putPubkey(buf, off, pool)
	off = putPubkey(buf, off, user)
	off = putPubkey(buf, off, baseMint)
	off = putPubkey(buf, off, quoteMint)
	_ = off
	return buf
}

// buildPumpSwapCreatePoolEventData builds a valid PumpSwap create pool event payload (with discriminator).
// Layout: creator(32) + base_mint(32) + quote_mint(32) + lp_token_amount_out(8) + pool(32) + lp_mint(32) + base_amount_in(8) + quote_amount_in(8) = 184
func buildPumpSwapCreatePoolEventData(creator, baseMint, quoteMint byte, lpTokenAmountOut uint64, pool, lpMint byte, baseAmountIn, quoteAmountIn uint64) []byte {
	buf := make([]byte, 8+184)
	copy(buf[:8], pumpSwapCreatePoolDiscriminator)
	off := 8
	off = putPubkey(buf, off, creator)
	off = putPubkey(buf, off, baseMint)
	off = putPubkey(buf, off, quoteMint)
	off = putU64LE(buf, off, lpTokenAmountOut)
	off = putPubkey(buf, off, pool)
	off = putPubkey(buf, off, lpMint)
	off = putU64LE(buf, off, baseAmountIn)
	off = putU64LE(buf, off, quoteAmountIn)
	_ = off
	return buf
}

// buildPumpSwapDepositEventData builds a valid PumpSwap deposit event payload (with discriminator).
// Layout: base_amount_in(8) + quote_amount_in(8) + lp_token_amount_out(8) + pool(32) + user(32) = 88
func buildPumpSwapDepositEventData(baseAmountIn, quoteAmountIn, lpTokenAmountOut uint64, pool, user byte) []byte {
	buf := make([]byte, 8+88)
	copy(buf[:8], pumpSwapDepositDiscriminator)
	off := 8
	off = putU64LE(buf, off, baseAmountIn)
	off = putU64LE(buf, off, quoteAmountIn)
	off = putU64LE(buf, off, lpTokenAmountOut)
	off = putPubkey(buf, off, pool)
	off = putPubkey(buf, off, user)
	_ = off
	return buf
}

// buildPumpSwapWithdrawEventData builds a valid PumpSwap withdraw event payload (with discriminator).
// Layout: lp_token_amount_in(8) + base_amount_out(8) + quote_amount_out(8) + pool(32) + user(32) = 88
func buildPumpSwapWithdrawEventData(lpTokenAmountIn, baseAmountOut, quoteAmountOut uint64, pool, user byte) []byte {
	buf := make([]byte, 8+88)
	copy(buf[:8], pumpSwapWithdrawDiscriminator)
	off := 8
	off = putU64LE(buf, off, lpTokenAmountIn)
	off = putU64LE(buf, off, baseAmountOut)
	off = putU64LE(buf, off, quoteAmountOut)
	off = putPubkey(buf, off, pool)
	off = putPubkey(buf, off, user)
	_ = off
	return buf
}

// --- PumpSwap Tests ---

func TestNewPumpSwapExtractor(t *testing.T) {
	ext := NewPumpSwapExtractor()
	require.NotNil(t, ext)
	assert.NotNil(t, ext.SolanaDexExtractor)
	assert.Equal(t, []string{"pumpswap"}, ext.GetSupportedProtocols())
	assert.Equal(t, []types.ChainType{types.ChainTypeSolana}, ext.GetSupportedChains())
}

func TestPumpSwap_SupportsBlock_SolanaChain(t *testing.T) {
	ext := NewPumpSwapExtractor()

	event := buildPumpSwapBuyEventData(500_000, 1_000_000_000, 1000, 500, 0x10, 0x20, 0x30, 0x40)
	block := solanaBlock(100, []solanaTx{{hash: "tx1", events: [][]byte{event}}})

	assert.True(t, ext.SupportsBlock(&block))
}

func TestPumpSwap_SupportsBlock_WrongChain(t *testing.T) {
	ext := NewPumpSwapExtractor()

	tests := []struct {
		name      string
		chainType types.ChainType
	}{
		{"BSC", types.ChainTypeBSC},
		{"Ethereum", types.ChainTypeEthereum},
		{"Sui", types.ChainTypeSui},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			block := types.UnifiedBlock{
				BlockNumber:  big.NewInt(100),
				ChainType:    tt.chainType,
				Transactions: []types.UnifiedTransaction{},
			}
			assert.False(t, ext.SupportsBlock(&block))
		})
	}
}

func TestPumpSwap_SupportsBlock_AllDiscriminators(t *testing.T) {
	ext := NewPumpSwapExtractor()

	tests := []struct {
		name  string
		event []byte
	}{
		{"BuyEvent", buildPumpSwapBuyEventData(100, 200, 1, 1, 0x10, 0x20, 0x30, 0x40)},
		{"SellEvent", buildPumpSwapSellEventData(100, 200, 1, 1, 0x10, 0x20, 0x30, 0x40)},
		{"CreatePoolEvent", buildPumpSwapCreatePoolEventData(0x01, 0x02, 0x03, 100, 0x04, 0x05, 200, 300)},
		{"DepositEvent", buildPumpSwapDepositEventData(100, 200, 300, 0x10, 0x20)},
		{"WithdrawEvent", buildPumpSwapWithdrawEventData(100, 200, 300, 0x10, 0x20)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			block := solanaBlock(100, []solanaTx{{hash: "tx1", events: [][]byte{tt.event}}})
			assert.True(t, ext.SupportsBlock(&block))
		})
	}
}

func TestPumpSwap_SupportsBlock_NoMatchingEvents(t *testing.T) {
	ext := NewPumpSwapExtractor()

	unknownEvent := make([]byte, 50)
	copy(unknownEvent[:8], []byte{99, 99, 99, 99, 99, 99, 99, 99})
	block := solanaBlock(100, []solanaTx{{hash: "tx1", events: [][]byte{unknownEvent}}})

	assert.False(t, ext.SupportsBlock(&block))
}

func TestPumpSwap_SupportsBlock_EmptyBlock(t *testing.T) {
	ext := NewPumpSwapExtractor()

	block := types.UnifiedBlock{
		BlockNumber:  big.NewInt(100),
		ChainType:    types.ChainTypeSolana,
		Transactions: []types.UnifiedTransaction{},
	}
	assert.False(t, ext.SupportsBlock(&block))
}

func TestPumpSwap_ExtractDexData_BuyEvent(t *testing.T) {
	ext := NewPumpSwapExtractor()
	ctx := context.Background()

	baseAmountOut := uint64(1_000_000)     // 1 token (6 decimals)
	quoteAmountIn := uint64(1_000_000_000) // 1 SOL
	lpFee := uint64(3_000_000)             // 0.003 SOL
	protocolFee := uint64(1_000_000)       // 0.001 SOL

	event := buildPumpSwapBuyEventData(baseAmountOut, quoteAmountIn, lpFee, protocolFee, 0x10, 0x20, 0x30, 0x40)
	block := solanaBlock(200, []solanaTx{{hash: "tx_buy", events: [][]byte{event}}})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Transactions, 1)

	tx := result.Transactions[0]
	assert.Equal(t, pubkeyString(0x30), tx.Addr)           // baseMint
	assert.Equal(t, pumpSwapProgramID, tx.Router)
	assert.Equal(t, pumpSwapProgramID, tx.Factory)
	assert.Equal(t, pubkeyString(0x10), tx.Pool)
	assert.Equal(t, "tx_buy", tx.Hash)
	assert.Equal(t, pubkeyString(0x20), tx.From)
	assert.Equal(t, "buy", tx.Side)
	assert.Equal(t, new(big.Int).SetUint64(quoteAmountIn), tx.Amount)
	assert.Equal(t, int64(0), tx.EventIndex)
	assert.Equal(t, int64(0), tx.SwapIndex)
	assert.Equal(t, int64(200), tx.BlockNumber)

	// Price: quoteAmountIn / baseAmountOut / 1e3 = 1e9 / 1e6 / 1e3 = 1.0
	expectedPrice := float64(quoteAmountIn) / float64(baseAmountOut) / 1e3
	assert.InDelta(t, expectedPrice, tx.Price, 1e-15)

	// Value: lamports to SOL = 1.0
	assert.InDelta(t, 1.0, tx.Value, 1e-10)

	require.NotNil(t, tx.Extra)
	assert.Equal(t, "swap", tx.Extra.Type)
	assert.Equal(t, 6, tx.Extra.TokenDecimals)
	assert.Equal(t, pubkeyString(0x40), tx.Extra.QuoteAddr)
	assert.Contains(t, tx.Extra.QuotePrice, "1.0")
}

func TestPumpSwap_ExtractDexData_SellEvent(t *testing.T) {
	ext := NewPumpSwapExtractor()
	ctx := context.Background()

	baseAmountIn := uint64(2_000_000)      // 2 tokens
	quoteAmountOut := uint64(500_000_000)  // 0.5 SOL
	lpFee := uint64(1_500_000)
	protocolFee := uint64(500_000)

	event := buildPumpSwapSellEventData(baseAmountIn, quoteAmountOut, lpFee, protocolFee, 0x10, 0x20, 0x30, 0x40)
	block := solanaBlock(201, []solanaTx{{hash: "tx_sell", events: [][]byte{event}}})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Transactions, 1)

	tx := result.Transactions[0]
	assert.Equal(t, "sell", tx.Side)
	assert.Equal(t, pubkeyString(0x30), tx.Addr) // baseMint
	assert.Equal(t, new(big.Int).SetUint64(quoteAmountOut), tx.Amount)

	// Price: quoteAmountOut / baseAmountIn / 1e3 = 5e8 / 2e6 / 1e3 = 0.25
	expectedPrice := float64(quoteAmountOut) / float64(baseAmountIn) / 1e3
	assert.InDelta(t, expectedPrice, tx.Price, 1e-15)

	// Value: 0.5 SOL
	assert.InDelta(t, 0.5, tx.Value, 1e-10)
}

func TestPumpSwap_ExtractDexData_BuyEvent_ZeroBaseAmount(t *testing.T) {
	ext := NewPumpSwapExtractor()
	ctx := context.Background()

	event := buildPumpSwapBuyEventData(0, 1_000_000_000, 0, 0, 0x10, 0x20, 0x30, 0x40)
	block := solanaBlock(202, []solanaTx{{hash: "tx_zero_base", events: [][]byte{event}}})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Transactions, 1)

	assert.Equal(t, float64(0), result.Transactions[0].Price)
}

func TestPumpSwap_ExtractDexData_SellEvent_ZeroBaseAmount(t *testing.T) {
	ext := NewPumpSwapExtractor()
	ctx := context.Background()

	event := buildPumpSwapSellEventData(0, 500_000_000, 0, 0, 0x10, 0x20, 0x30, 0x40)
	block := solanaBlock(203, []solanaTx{{hash: "tx_zero_base_sell", events: [][]byte{event}}})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Transactions, 1)

	assert.Equal(t, float64(0), result.Transactions[0].Price)
}

func TestPumpSwap_ExtractDexData_CreatePoolEvent(t *testing.T) {
	ext := NewPumpSwapExtractor()
	ctx := context.Background()

	event := buildPumpSwapCreatePoolEventData(
		0x01,           // creator
		0x02,           // baseMint
		0x03,           // quoteMint
		1_000_000_000,  // lpTokenAmountOut
		0x04,           // pool
		0x05,           // lpMint
		500_000_000,    // baseAmountIn
		1_000_000_000,  // quoteAmountIn
	)
	block := solanaBlock(300, []solanaTx{{hash: "tx_create_pool", events: [][]byte{event}}})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Pools, 1)

	pool := result.Pools[0]
	assert.Equal(t, pubkeyString(0x04), pool.Addr)
	assert.Equal(t, pumpSwapProgramID, pool.Factory)
	assert.Equal(t, "pumpswap", pool.Protocol)
	assert.Equal(t, 30, pool.Fee) // 0.3% = 30 bps
	assert.Equal(t, pubkeyString(0x02), pool.Tokens[0]) // baseMint
	assert.Equal(t, pubkeyString(0x03), pool.Tokens[1]) // quoteMint
	require.NotNil(t, pool.Extra)
	assert.Equal(t, "tx_create_pool", pool.Extra.Hash)
	assert.Equal(t, pubkeyString(0x01), pool.Extra.From)
}

func TestPumpSwap_ExtractDexData_DepositEvent(t *testing.T) {
	ext := NewPumpSwapExtractor()
	ctx := context.Background()

	baseAmountIn := uint64(1_000_000)
	quoteAmountIn := uint64(2_000_000_000) // 2 SOL
	lpTokenAmountOut := uint64(500_000_000)

	event := buildPumpSwapDepositEventData(baseAmountIn, quoteAmountIn, lpTokenAmountOut, 0x10, 0x20)
	block := solanaBlock(400, []solanaTx{{hash: "tx_deposit", events: [][]byte{event}}})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Liquidities, 1)

	liq := result.Liquidities[0]
	assert.Equal(t, pubkeyString(0x10), liq.Addr)
	assert.Equal(t, pumpSwapProgramID, liq.Router)
	assert.Equal(t, pumpSwapProgramID, liq.Factory)
	assert.Equal(t, pubkeyString(0x10), liq.Pool)
	assert.Equal(t, "tx_deposit", liq.Hash)
	assert.Equal(t, pubkeyString(0x20), liq.From)
	assert.Equal(t, "add", liq.Side)
	assert.Equal(t, new(big.Int).SetUint64(quoteAmountIn), liq.Amount)

	// Value = quoteValue * 2 (approximate equal value on both sides)
	quoteValue := float64(quoteAmountIn) / 1e9 // 2.0 SOL
	assert.InDelta(t, quoteValue*2, liq.Value, 1e-10)

	expectedKey := fmt.Sprintf("%s_add_%d", "tx_deposit", 0)
	assert.Equal(t, expectedKey, liq.Key)

	require.NotNil(t, liq.Extra)
	assert.Equal(t, expectedKey, liq.Extra.Key)
	assert.Equal(t, new(big.Int).SetUint64(quoteAmountIn), liq.Extra.Amounts)
	require.Len(t, liq.Extra.Values, 2)
	assert.Equal(t, float64(0), liq.Extra.Values[0])       // base value unknown
	assert.InDelta(t, quoteValue, liq.Extra.Values[1], 1e-10)
}

func TestPumpSwap_ExtractDexData_WithdrawEvent(t *testing.T) {
	ext := NewPumpSwapExtractor()
	ctx := context.Background()

	lpTokenAmountIn := uint64(500_000_000)
	baseAmountOut := uint64(1_000_000)
	quoteAmountOut := uint64(3_000_000_000) // 3 SOL

	event := buildPumpSwapWithdrawEventData(lpTokenAmountIn, baseAmountOut, quoteAmountOut, 0x10, 0x20)
	block := solanaBlock(401, []solanaTx{{hash: "tx_withdraw", events: [][]byte{event}}})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Liquidities, 1)

	liq := result.Liquidities[0]
	assert.Equal(t, pubkeyString(0x10), liq.Addr)
	assert.Equal(t, pubkeyString(0x10), liq.Pool)
	assert.Equal(t, "tx_withdraw", liq.Hash)
	assert.Equal(t, pubkeyString(0x20), liq.From)
	assert.Equal(t, "remove", liq.Side)
	assert.Equal(t, new(big.Int).SetUint64(quoteAmountOut), liq.Amount)

	quoteValue := float64(quoteAmountOut) / 1e9 // 3.0 SOL
	assert.InDelta(t, quoteValue*2, liq.Value, 1e-10)

	expectedKey := fmt.Sprintf("%s_remove_%d", "tx_withdraw", 0)
	assert.Equal(t, expectedKey, liq.Key)

	require.NotNil(t, liq.Extra)
	assert.Equal(t, expectedKey, liq.Extra.Key)
}

func TestPumpSwap_ExtractDexData_EmptyBlocks(t *testing.T) {
	ext := NewPumpSwapExtractor()
	ctx := context.Background()

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{})
	require.NoError(t, err)
	assert.Empty(t, result.Transactions)
	assert.Empty(t, result.Pools)
	assert.Empty(t, result.Liquidities)
}

func TestPumpSwap_ExtractDexData_NilBlocks(t *testing.T) {
	ext := NewPumpSwapExtractor()
	ctx := context.Background()

	result, err := ext.ExtractDexData(ctx, nil)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestPumpSwap_ExtractDexData_UnsupportedChainSkipped(t *testing.T) {
	ext := NewPumpSwapExtractor()
	ctx := context.Background()

	event := buildPumpSwapBuyEventData(1_000_000, 1_000_000_000, 0, 0, 0x10, 0x20, 0x30, 0x40)
	block := solanaBlock(100, []solanaTx{{hash: "tx1", events: [][]byte{event}}})
	block.ChainType = types.ChainTypeEthereum

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Empty(t, result.Transactions)
}

func TestPumpSwap_ExtractDexData_BuyEvent_DataTooShort(t *testing.T) {
	ext := NewPumpSwapExtractor()
	ctx := context.Background()

	tests := []struct {
		name string
		size int
	}{
		{"Empty after discriminator", 8},
		{"Only amounts", 8 + 32},
		{"Missing last pubkey", 8 + 32 + 96},
		{"One byte short", 8 + 160 - 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eventData := make([]byte, tt.size)
			copy(eventData[:8], pumpSwapBuyDiscriminator)
			block := solanaBlock(700, []solanaTx{{hash: "tx_short_buy", events: [][]byte{eventData}}})

			result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
			require.NoError(t, err)
			assert.Empty(t, result.Transactions)
		})
	}
}

func TestPumpSwap_ExtractDexData_SellEvent_DataTooShort(t *testing.T) {
	ext := NewPumpSwapExtractor()
	ctx := context.Background()

	eventData := make([]byte, 8+100)
	copy(eventData[:8], pumpSwapSellDiscriminator)
	block := solanaBlock(701, []solanaTx{{hash: "tx_short_sell", events: [][]byte{eventData}}})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Empty(t, result.Transactions)
}

func TestPumpSwap_ExtractDexData_CreatePoolEvent_DataTooShort(t *testing.T) {
	ext := NewPumpSwapExtractor()
	ctx := context.Background()

	eventData := make([]byte, 8+100)
	copy(eventData[:8], pumpSwapCreatePoolDiscriminator)
	block := solanaBlock(702, []solanaTx{{hash: "tx_short_create", events: [][]byte{eventData}}})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Empty(t, result.Pools)
}

func TestPumpSwap_ExtractDexData_DepositEvent_DataTooShort(t *testing.T) {
	ext := NewPumpSwapExtractor()
	ctx := context.Background()

	eventData := make([]byte, 8+50)
	copy(eventData[:8], pumpSwapDepositDiscriminator)
	block := solanaBlock(703, []solanaTx{{hash: "tx_short_deposit", events: [][]byte{eventData}}})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Empty(t, result.Liquidities)
}

func TestPumpSwap_ExtractDexData_WithdrawEvent_DataTooShort(t *testing.T) {
	ext := NewPumpSwapExtractor()
	ctx := context.Background()

	eventData := make([]byte, 8+50)
	copy(eventData[:8], pumpSwapWithdrawDiscriminator)
	block := solanaBlock(704, []solanaTx{{hash: "tx_short_withdraw", events: [][]byte{eventData}}})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Empty(t, result.Liquidities)
}

func TestPumpSwap_ExtractDexData_UnknownDiscriminator(t *testing.T) {
	ext := NewPumpSwapExtractor()
	ctx := context.Background()

	unknownEvent := make([]byte, 200)
	copy(unknownEvent[:8], []byte{1, 2, 3, 4, 5, 6, 7, 8})
	block := solanaBlock(705, []solanaTx{{hash: "tx_unknown", events: [][]byte{unknownEvent}}})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Empty(t, result.Transactions)
	assert.Empty(t, result.Pools)
	assert.Empty(t, result.Liquidities)
}

func TestPumpSwap_ExtractDexData_MultipleEventsInOneTx(t *testing.T) {
	ext := NewPumpSwapExtractor()
	ctx := context.Background()

	buy := buildPumpSwapBuyEventData(1_000_000, 1_000_000_000, 0, 0, 0x10, 0x20, 0x30, 0x40)
	sell := buildPumpSwapSellEventData(2_000_000, 500_000_000, 0, 0, 0x10, 0x20, 0x30, 0x40)

	block := solanaBlock(500, []solanaTx{{hash: "tx_multi", events: [][]byte{buy, sell}}})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Transactions, 2)

	assert.Equal(t, "buy", result.Transactions[0].Side)
	assert.Equal(t, "sell", result.Transactions[1].Side)

	// Swap indices increment
	assert.Equal(t, int64(0), result.Transactions[0].SwapIndex)
	assert.Equal(t, int64(1), result.Transactions[1].SwapIndex)

	// Event indices match log position
	assert.Equal(t, int64(0), result.Transactions[0].EventIndex)
	assert.Equal(t, int64(1), result.Transactions[1].EventIndex)
}

func TestPumpSwap_ExtractDexData_MixedEvents(t *testing.T) {
	ext := NewPumpSwapExtractor()
	ctx := context.Background()

	createPool := buildPumpSwapCreatePoolEventData(0x01, 0x02, 0x03, 100, 0x04, 0x05, 200, 300)
	buy := buildPumpSwapBuyEventData(1_000_000, 1_000_000_000, 0, 0, 0x10, 0x20, 0x30, 0x40)
	deposit := buildPumpSwapDepositEventData(500_000, 1_000_000_000, 100_000, 0x10, 0x20)
	withdraw := buildPumpSwapWithdrawEventData(100_000, 200_000, 500_000_000, 0x10, 0x20)

	block := solanaBlock(600, []solanaTx{{hash: "tx_mixed", events: [][]byte{createPool, buy, deposit, withdraw}}})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)

	assert.Len(t, result.Pools, 1)
	assert.Len(t, result.Transactions, 1)
	assert.Len(t, result.Liquidities, 2) // deposit + withdraw
}

func TestPumpSwap_ExtractDexData_SwapIndexResetPerTx(t *testing.T) {
	ext := NewPumpSwapExtractor()
	ctx := context.Background()

	buy1 := buildPumpSwapBuyEventData(1_000_000, 1_000_000_000, 0, 0, 0x10, 0x20, 0x30, 0x40)
	buy2 := buildPumpSwapBuyEventData(2_000_000, 2_000_000_000, 0, 0, 0x11, 0x21, 0x31, 0x41)

	block := solanaBlock(800, []solanaTx{
		{hash: "tx_a", events: [][]byte{buy1}},
		{hash: "tx_b", events: [][]byte{buy2}},
	})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Transactions, 2)

	assert.Equal(t, int64(0), result.Transactions[0].SwapIndex)
	assert.Equal(t, int64(0), result.Transactions[1].SwapIndex)
}

func TestPumpSwap_ExtractDexData_PriceCalculation_TableDriven(t *testing.T) {
	ext := NewPumpSwapExtractor()
	ctx := context.Background()

	tests := []struct {
		name          string
		side          string
		baseAmount    uint64
		quoteAmount   uint64
		expectedPrice float64
	}{
		{
			name:          "Buy: 1 SOL for 1000 tokens",
			side:          "buy",
			baseAmount:    1_000_000_000, // 1000 tokens
			quoteAmount:   1_000_000_000, // 1 SOL
			expectedPrice: 1.0 / 1e3,
		},
		{
			name:          "Sell: 500 tokens for 0.5 SOL",
			side:          "sell",
			baseAmount:    500_000,       // 0.5 tokens
			quoteAmount:   500_000_000,   // 0.5 SOL
			expectedPrice: 500_000_000.0 / 500_000.0 / 1e3,
		},
		{
			name:          "Buy: zero base amount",
			side:          "buy",
			baseAmount:    0,
			quoteAmount:   1_000_000_000,
			expectedPrice: 0,
		},
		{
			name:          "Sell: zero base amount",
			side:          "sell",
			baseAmount:    0,
			quoteAmount:   500_000_000,
			expectedPrice: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var event []byte
			if tt.side == "buy" {
				event = buildPumpSwapBuyEventData(tt.baseAmount, tt.quoteAmount, 0, 0, 0x10, 0x20, 0x30, 0x40)
			} else {
				event = buildPumpSwapSellEventData(tt.baseAmount, tt.quoteAmount, 0, 0, 0x10, 0x20, 0x30, 0x40)
			}
			block := solanaBlock(900, []solanaTx{{hash: "tx_price", events: [][]byte{event}}})

			result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
			require.NoError(t, err)
			require.Len(t, result.Transactions, 1)
			assert.InDelta(t, tt.expectedPrice, result.Transactions[0].Price, 1e-15)
		})
	}
}

func TestPumpSwap_ExtractDexData_MultipleBlocks(t *testing.T) {
	ext := NewPumpSwapExtractor()
	ctx := context.Background()

	buy := buildPumpSwapBuyEventData(1_000_000, 1_000_000_000, 0, 0, 0x10, 0x20, 0x30, 0x40)
	sell := buildPumpSwapSellEventData(2_000_000, 500_000_000, 0, 0, 0x11, 0x21, 0x31, 0x41)

	block1 := solanaBlock(100, []solanaTx{{hash: "tx_block1", events: [][]byte{buy}}})
	block2 := solanaBlock(101, []solanaTx{{hash: "tx_block2", events: [][]byte{sell}}})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block1, block2})
	require.NoError(t, err)
	require.Len(t, result.Transactions, 2)

	assert.Equal(t, int64(100), result.Transactions[0].BlockNumber)
	assert.Equal(t, int64(101), result.Transactions[1].BlockNumber)
}

func TestPumpSwap_ExtractDexData_NilRawData(t *testing.T) {
	ext := NewPumpSwapExtractor()
	ctx := context.Background()

	block := types.UnifiedBlock{
		BlockNumber: big.NewInt(706),
		ChainType:   types.ChainTypeSolana,
		Transactions: []types.UnifiedTransaction{
			{
				TxHash:    "tx_nil_raw",
				ChainType: types.ChainTypeSolana,
				RawData:   nil,
			},
		},
	}

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Empty(t, result.Transactions)
}

func TestPumpSwap_DiscriminatorMatching_TableDriven(t *testing.T) {
	discs := []struct {
		name string
		disc []byte
	}{
		{"Buy", pumpSwapBuyDiscriminator},
		{"Sell", pumpSwapSellDiscriminator},
		{"CreatePool", pumpSwapCreatePoolDiscriminator},
		{"Deposit", pumpSwapDepositDiscriminator},
		{"Withdraw", pumpSwapWithdrawDiscriminator},
	}

	for _, d := range discs {
		t.Run(d.name+"_Length", func(t *testing.T) {
			assert.Len(t, d.disc, 8, "discriminator should be 8 bytes")
		})
	}

	// Verify all discriminators are unique
	t.Run("AllUnique", func(t *testing.T) {
		seen := make(map[string]string)
		for _, d := range discs {
			key := fmt.Sprintf("%v", d.disc)
			if existing, ok := seen[key]; ok {
				t.Errorf("discriminator %s conflicts with %s", d.name, existing)
			}
			seen[key] = d.name
		}
	})
}

func TestPumpSwap_ExtractDexData_DepositEvent_LiquidityKeyFormat(t *testing.T) {
	ext := NewPumpSwapExtractor()
	ctx := context.Background()

	event := buildPumpSwapDepositEventData(500_000, 1_000_000_000, 100_000, 0x10, 0x20)
	block := solanaBlock(400, []solanaTx{{hash: "txhash123", events: [][]byte{event}}})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Liquidities, 1)

	// Key format: "{txhash}_add_{eventIdx}"
	assert.Equal(t, "txhash123_add_0", result.Liquidities[0].Key)
}

func TestPumpSwap_ExtractDexData_WithdrawEvent_LiquidityKeyFormat(t *testing.T) {
	ext := NewPumpSwapExtractor()
	ctx := context.Background()

	event := buildPumpSwapWithdrawEventData(100_000, 200_000, 500_000_000, 0x10, 0x20)
	block := solanaBlock(401, []solanaTx{{hash: "txhash456", events: [][]byte{event}}})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Liquidities, 1)

	// Key format: "{txhash}_remove_{eventIdx}"
	assert.Equal(t, "txhash456_remove_0", result.Liquidities[0].Key)
}

func TestPumpSwap_ExtractDexData_DepositEvent_EmptyPoolAddr(t *testing.T) {
	ext := NewPumpSwapExtractor()
	ctx := context.Background()

	// Build event data where pool is all zeros (will parse as valid base58 "1111..." not empty)
	// Actually, ParsePubkey returns base58 of 32 zero bytes which is "11111111111111111111111111111111"
	// This is a valid address, so the event should not be skipped.
	// The only way to get empty pool is if data is too short (already tested).
	// This test verifies that all-zero pubkey is accepted.
	event := buildPumpSwapDepositEventData(100, 200, 300, 0x00, 0x00)
	block := solanaBlock(800, []solanaTx{{hash: "tx_zero_pool", events: [][]byte{event}}})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	// All-zero pubkey is still a valid pubkey string, so event should be parsed
	require.Len(t, result.Liquidities, 1)
}
