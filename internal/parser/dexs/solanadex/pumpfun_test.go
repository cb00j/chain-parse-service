package solanadex

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math/big"
	"testing"
	"time"

	"unified-tx-parser/internal/types"

	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Test helpers for PumpFun ---

// makePubkeyBytes returns 32 bytes for a deterministic fake pubkey based on seed.
func makePubkeyBytes(seed byte) []byte {
	b := make([]byte, 32)
	for i := range b {
		b[i] = seed
	}
	return b
}

// pubkeyString returns the base58-encoded string for the deterministic pubkey.
func pubkeyString(seed byte) string {
	return base58.Encode(makePubkeyBytes(seed))
}

// putU64LE writes a uint64 in little-endian to a byte slice at the given offset.
func putU64LE(buf []byte, off int, v uint64) int {
	binary.LittleEndian.PutUint64(buf[off:off+8], v)
	return off + 8
}

// putI64LE writes an int64 in little-endian to a byte slice at the given offset.
func putI64LE(buf []byte, off int, v int64) int {
	binary.LittleEndian.PutUint64(buf[off:off+8], uint64(v))
	return off + 8
}

// putBool writes a bool (1 byte) at the given offset.
func putBool(buf []byte, off int, v bool) int {
	if v {
		buf[off] = 1
	} else {
		buf[off] = 0
	}
	return off + 1
}

// putPubkey writes 32 bytes of a pubkey at the given offset.
func putPubkey(buf []byte, off int, seed byte) int {
	copy(buf[off:off+32], makePubkeyBytes(seed))
	return off + 32
}

// putString writes a Borsh-encoded string (4-byte LE length prefix + UTF-8 data).
func putString(buf []byte, off int, s string) int {
	binary.LittleEndian.PutUint32(buf[off:off+4], uint32(len(s)))
	off += 4
	copy(buf[off:], []byte(s))
	return off + len(s)
}

// buildPumpFunTradeEventData builds a valid PumpFun trade event payload (with discriminator).
func buildPumpFunTradeEventData(mint byte, solAmount, tokenAmount uint64, isBuy bool, user byte, timestamp int64) []byte {
	// discriminator(8) + mint(32) + sol_amount(8) + token_amount(8) + is_buy(1) + user(32) + timestamp(8) = 97
	// + optional: virtual_sol_reserves(8) + virtual_token_reserves(8) + real_sol_reserves(8) + real_token_reserves(8) = 32
	buf := make([]byte, 8+89+32)
	copy(buf[:8], pumpFunTradeDiscriminator)
	off := 8
	off = putPubkey(buf, off, mint)
	off = putU64LE(buf, off, solAmount)
	off = putU64LE(buf, off, tokenAmount)
	off = putBool(buf, off, isBuy)
	off = putPubkey(buf, off, user)
	off = putI64LE(buf, off, timestamp)
	// optional reserves (zeros)
	_ = off
	return buf
}

// buildPumpFunCreateEventData builds a valid PumpFun create event payload (with discriminator).
func buildPumpFunCreateEventData(name, symbol, uri string, mint, bondingCurve, user byte) []byte {
	// discriminator(8) + 3 strings (4+len each) + 3 pubkeys (32 each)
	size := 8 + (4 + len(name)) + (4 + len(symbol)) + (4 + len(uri)) + 32*3
	buf := make([]byte, size)
	copy(buf[:8], pumpFunCreateDiscriminator)
	off := 8
	off = putString(buf, off, name)
	off = putString(buf, off, symbol)
	off = putString(buf, off, uri)
	off = putPubkey(buf, off, mint)
	off = putPubkey(buf, off, bondingCurve)
	off = putPubkey(buf, off, user)
	_ = off
	return buf
}

// buildPumpFunCompleteEventData builds a valid PumpFun complete event payload (with discriminator).
func buildPumpFunCompleteEventData(user, mint, bondingCurve byte, timestamp int64) []byte {
	// discriminator(8) + user(32) + mint(32) + bonding_curve(32) + timestamp(8) = 112
	buf := make([]byte, 8+104)
	copy(buf[:8], pumpFunCompleteDiscriminator)
	off := 8
	off = putPubkey(buf, off, user)
	off = putPubkey(buf, off, mint)
	off = putPubkey(buf, off, bondingCurve)
	off = putI64LE(buf, off, timestamp)
	_ = off
	return buf
}

// solanaBlock creates a Solana UnifiedBlock with transactions whose RawData contains
// "Program data: " log messages built from the given event payloads.
func solanaBlock(blockNum int64, txs []solanaTx) types.UnifiedBlock {
	unifiedTxs := make([]types.UnifiedTransaction, len(txs))
	for i, stx := range txs {
		logs := make([]any, len(stx.events))
		for j, ev := range stx.events {
			logs[j] = "Program data: " + base64.StdEncoding.EncodeToString(ev)
		}
		unifiedTxs[i] = types.UnifiedTransaction{
			TxHash:      stx.hash,
			ChainType:   types.ChainTypeSolana,
			ChainID:     "solana",
			BlockNumber: big.NewInt(blockNum),
			TxIndex:     i,
			Status:      types.TransactionStatusSuccess,
			Timestamp:   time.Unix(1700000000, 0),
			RawData: map[string]any{
				"log_messages": logs,
			},
		}
	}
	return types.UnifiedBlock{
		BlockNumber:  big.NewInt(blockNum),
		BlockHash:    "blockhash123",
		ChainType:    types.ChainTypeSolana,
		ChainID:      "solana",
		Timestamp:    time.Unix(1700000000, 0),
		TxCount:      len(txs),
		Transactions: unifiedTxs,
	}
}

type solanaTx struct {
	hash   string
	events [][]byte
}

// --- PumpFun Tests ---

func TestNewPumpFunExtractor(t *testing.T) {
	ext := NewPumpFunExtractor()
	require.NotNil(t, ext)
	assert.NotNil(t, ext.SolanaDexExtractor)
	assert.Equal(t, []string{"pumpfun"}, ext.GetSupportedProtocols())
	assert.Equal(t, []types.ChainType{types.ChainTypeSolana}, ext.GetSupportedChains())
}

func TestPumpFun_SupportsBlock_SolanaChain(t *testing.T) {
	ext := NewPumpFunExtractor()

	tradeEvent := buildPumpFunTradeEventData(0x01, 1000000000, 500000, true, 0x02, 1700000000)
	block := solanaBlock(100, []solanaTx{{hash: "tx1", events: [][]byte{tradeEvent}}})

	assert.True(t, ext.SupportsBlock(&block))
}

func TestPumpFun_SupportsBlock_WrongChain(t *testing.T) {
	ext := NewPumpFunExtractor()

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

func TestPumpFun_SupportsBlock_NoMatchingEvents(t *testing.T) {
	ext := NewPumpFunExtractor()

	// Create a block with an unknown discriminator
	unknownEvent := make([]byte, 50)
	copy(unknownEvent[:8], []byte{99, 99, 99, 99, 99, 99, 99, 99})
	block := solanaBlock(100, []solanaTx{{hash: "tx1", events: [][]byte{unknownEvent}}})

	assert.False(t, ext.SupportsBlock(&block))
}

func TestPumpFun_SupportsBlock_EmptyBlock(t *testing.T) {
	ext := NewPumpFunExtractor()

	block := types.UnifiedBlock{
		BlockNumber:  big.NewInt(100),
		ChainType:    types.ChainTypeSolana,
		Transactions: []types.UnifiedTransaction{},
	}
	assert.False(t, ext.SupportsBlock(&block))
}

func TestPumpFun_SupportsBlock_AllDiscriminators(t *testing.T) {
	ext := NewPumpFunExtractor()

	tests := []struct {
		name  string
		event []byte
	}{
		{"TradeEvent", buildPumpFunTradeEventData(0x01, 1000, 500, true, 0x02, 1700000000)},
		{"CreateEvent", buildPumpFunCreateEventData("Test", "TST", "https://example.com", 0x01, 0x02, 0x03)},
		{"CompleteEvent", buildPumpFunCompleteEventData(0x01, 0x02, 0x03, 1700000000)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			block := solanaBlock(100, []solanaTx{{hash: "tx1", events: [][]byte{tt.event}}})
			assert.True(t, ext.SupportsBlock(&block))
		})
	}
}

func TestPumpFun_ExtractDexData_TradeEvent_Buy(t *testing.T) {
	ext := NewPumpFunExtractor()
	ctx := context.Background()

	solAmount := uint64(1_000_000_000)   // 1 SOL
	tokenAmount := uint64(1_000_000_000) // 1B tokens (6 decimals = 1000 tokens)
	timestamp := int64(1700000500)

	event := buildPumpFunTradeEventData(0x01, solAmount, tokenAmount, true, 0x02, timestamp)
	block := solanaBlock(200, []solanaTx{{hash: "txhash_buy", events: [][]byte{event}}})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Transactions, 1)

	tx := result.Transactions[0]
	assert.Equal(t, pubkeyString(0x01), tx.Addr)
	assert.Equal(t, pumpFunProgramID, tx.Router)
	assert.Equal(t, pumpFunProgramID, tx.Factory)
	assert.Equal(t, pubkeyString(0x01), tx.Pool) // PumpFun uses mint as pool
	assert.Equal(t, "txhash_buy", tx.Hash)
	assert.Equal(t, pubkeyString(0x02), tx.From)
	assert.Equal(t, "buy", tx.Side)
	assert.Equal(t, new(big.Int).SetUint64(solAmount), tx.Amount)
	assert.Equal(t, uint64(timestamp), tx.Time)
	assert.Equal(t, int64(0), tx.EventIndex)
	assert.Equal(t, int64(0), tx.SwapIndex)
	assert.Equal(t, int64(200), tx.BlockNumber)

	// Price: (1e9 / 1e9) / 1e3 = 0.001
	expectedPrice := float64(solAmount) / float64(tokenAmount) / 1e3
	assert.InDelta(t, expectedPrice, tx.Price, 1e-15)

	// Value: lamports to SOL = 1.0
	assert.InDelta(t, 1.0, tx.Value, 1e-10)

	require.NotNil(t, tx.Extra)
	assert.Equal(t, "swap", tx.Extra.Type)
	assert.Equal(t, 6, tx.Extra.TokenDecimals)
}

func TestPumpFun_ExtractDexData_TradeEvent_Sell(t *testing.T) {
	ext := NewPumpFunExtractor()
	ctx := context.Background()

	solAmount := uint64(500_000_000) // 0.5 SOL
	tokenAmount := uint64(2_000_000) // 2 tokens (6 decimals)

	event := buildPumpFunTradeEventData(0x01, solAmount, tokenAmount, false, 0x02, 1700000000)
	block := solanaBlock(201, []solanaTx{{hash: "txhash_sell", events: [][]byte{event}}})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Transactions, 1)

	tx := result.Transactions[0]
	assert.Equal(t, "sell", tx.Side)
	assert.Equal(t, new(big.Int).SetUint64(solAmount), tx.Amount)

	// Price: (500_000_000 / 2_000_000) / 1e3 = 250 / 1000 = 0.25
	expectedPrice := float64(solAmount) / float64(tokenAmount) / 1e3
	assert.InDelta(t, expectedPrice, tx.Price, 1e-15)
}

func TestPumpFun_ExtractDexData_TradeEvent_ZeroTokenAmount(t *testing.T) {
	ext := NewPumpFunExtractor()
	ctx := context.Background()

	event := buildPumpFunTradeEventData(0x01, 1_000_000_000, 0, true, 0x02, 1700000000)
	block := solanaBlock(202, []solanaTx{{hash: "tx_zero_token", events: [][]byte{event}}})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Transactions, 1)

	// When tokenAmount is 0, price should be 0
	assert.Equal(t, float64(0), result.Transactions[0].Price)
}

func TestPumpFun_ExtractDexData_TradeEvent_TimestampFallback(t *testing.T) {
	ext := NewPumpFunExtractor()
	ctx := context.Background()

	// When event timestamp is 0, should use tx.Timestamp
	event := buildPumpFunTradeEventData(0x01, 1_000_000_000, 1_000_000, true, 0x02, 0)
	block := solanaBlock(203, []solanaTx{{hash: "tx_no_ts", events: [][]byte{event}}})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Transactions, 1)

	// Should fall back to tx timestamp (1700000000)
	assert.Equal(t, uint64(1700000000), result.Transactions[0].Time)
}

func TestPumpFun_ExtractDexData_CreateEvent(t *testing.T) {
	ext := NewPumpFunExtractor()
	ctx := context.Background()

	event := buildPumpFunCreateEventData("MyToken", "MTK", "https://arweave.net/abc123", 0x10, 0x20, 0x30)
	block := solanaBlock(300, []solanaTx{{hash: "txhash_create", events: [][]byte{event}}})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)

	// Should produce a Pool and a Token
	require.Len(t, result.Pools, 1)
	require.Len(t, result.Tokens, 1)

	pool := result.Pools[0]
	assert.Equal(t, pubkeyString(0x20), pool.Addr) // bonding_curve as pool addr
	assert.Equal(t, pumpFunProgramID, pool.Factory)
	assert.Equal(t, "pumpfun", pool.Protocol)
	assert.Equal(t, 100, pool.Fee)
	assert.Equal(t, pubkeyString(0x10), pool.Tokens[0]) // mint
	assert.Equal(t, "So11111111111111111111111111111111", pool.Tokens[1])
	require.NotNil(t, pool.Extra)
	assert.Equal(t, "txhash_create", pool.Extra.Hash)
	assert.Equal(t, pubkeyString(0x30), pool.Extra.From)

	// URI should be in Args
	require.NotNil(t, pool.Args)
	assert.Equal(t, "https://arweave.net/abc123", pool.Args["uri"])

	token := result.Tokens[0]
	assert.Equal(t, pubkeyString(0x10), token.Addr)
	assert.Equal(t, "MyToken", token.Name)
	assert.Equal(t, "MTK", token.Symbol)
	assert.Equal(t, 6, token.Decimals)
	assert.NotEmpty(t, token.CreatedAt)
}

func TestPumpFun_ExtractDexData_CreateEvent_EmptyURI(t *testing.T) {
	ext := NewPumpFunExtractor()
	ctx := context.Background()

	event := buildPumpFunCreateEventData("Token", "TKN", "", 0x10, 0x20, 0x30)
	block := solanaBlock(301, []solanaTx{{hash: "tx_no_uri", events: [][]byte{event}}})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Pools, 1)

	// Empty URI should not produce Args
	assert.Nil(t, result.Pools[0].Args)
}

func TestPumpFun_ExtractDexData_CompleteEvent(t *testing.T) {
	ext := NewPumpFunExtractor()
	ctx := context.Background()

	timestamp := int64(1700001000)
	event := buildPumpFunCompleteEventData(0x40, 0x50, 0x60, timestamp)
	block := solanaBlock(400, []solanaTx{{hash: "txhash_complete", events: [][]byte{event}}})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Liquidities, 1)

	liq := result.Liquidities[0]
	assert.Equal(t, pubkeyString(0x60), liq.Addr)
	assert.Equal(t, pumpFunProgramID, liq.Router)
	assert.Equal(t, pumpFunProgramID, liq.Factory)
	assert.Equal(t, pubkeyString(0x60), liq.Pool)
	assert.Equal(t, "txhash_complete", liq.Hash)
	assert.Equal(t, pubkeyString(0x40), liq.From)
	assert.Equal(t, "graduate", liq.Side)
	assert.Equal(t, big.NewInt(0), liq.Amount)
	assert.Equal(t, float64(0), liq.Value)
	assert.Equal(t, uint64(timestamp), liq.Time)

	expectedKey := fmt.Sprintf("%s_graduate_%d", "txhash_complete", 0)
	assert.Equal(t, expectedKey, liq.Key)
	require.NotNil(t, liq.Extra)
	assert.Equal(t, expectedKey, liq.Extra.Key)
	assert.Equal(t, uint64(timestamp), liq.Extra.Time)
}

func TestPumpFun_ExtractDexData_CompleteEvent_TimestampFallback(t *testing.T) {
	ext := NewPumpFunExtractor()
	ctx := context.Background()

	event := buildPumpFunCompleteEventData(0x40, 0x50, 0x60, 0)
	block := solanaBlock(401, []solanaTx{{hash: "tx_complete_no_ts", events: [][]byte{event}}})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Liquidities, 1)

	assert.Equal(t, uint64(1700000000), result.Liquidities[0].Time)
}

func TestPumpFun_ExtractDexData_MultipleEventsInOneTx(t *testing.T) {
	ext := NewPumpFunExtractor()
	ctx := context.Background()

	trade1 := buildPumpFunTradeEventData(0x01, 1_000_000_000, 500_000, true, 0x02, 1700000000)
	trade2 := buildPumpFunTradeEventData(0x03, 2_000_000_000, 1_000_000, false, 0x04, 1700000001)

	block := solanaBlock(500, []solanaTx{{hash: "tx_multi", events: [][]byte{trade1, trade2}}})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Transactions, 2)

	// Verify swap indices increment
	assert.Equal(t, int64(0), result.Transactions[0].SwapIndex)
	assert.Equal(t, int64(1), result.Transactions[1].SwapIndex)

	// Verify event indices match log position
	assert.Equal(t, int64(0), result.Transactions[0].EventIndex)
	assert.Equal(t, int64(1), result.Transactions[1].EventIndex)
}

func TestPumpFun_ExtractDexData_MixedEvents(t *testing.T) {
	ext := NewPumpFunExtractor()
	ctx := context.Background()

	create := buildPumpFunCreateEventData("Mixed", "MIX", "https://uri", 0x10, 0x20, 0x30)
	trade := buildPumpFunTradeEventData(0x10, 1_000_000_000, 500_000, true, 0x02, 1700000000)
	complete := buildPumpFunCompleteEventData(0x02, 0x10, 0x20, 1700001000)

	block := solanaBlock(600, []solanaTx{{hash: "tx_mixed", events: [][]byte{create, trade, complete}}})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)

	assert.Len(t, result.Pools, 1)
	assert.Len(t, result.Tokens, 1)
	assert.Len(t, result.Transactions, 1)
	assert.Len(t, result.Liquidities, 1)
}

func TestPumpFun_ExtractDexData_UnsupportedChainSkipped(t *testing.T) {
	ext := NewPumpFunExtractor()
	ctx := context.Background()

	event := buildPumpFunTradeEventData(0x01, 1_000_000_000, 500_000, true, 0x02, 1700000000)
	block := solanaBlock(100, []solanaTx{{hash: "tx1", events: [][]byte{event}}})
	block.ChainType = types.ChainTypeBSC // Override to wrong chain

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Empty(t, result.Transactions)
}

func TestPumpFun_ExtractDexData_EmptyBlocks(t *testing.T) {
	ext := NewPumpFunExtractor()
	ctx := context.Background()

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{})
	require.NoError(t, err)
	assert.Empty(t, result.Transactions)
	assert.Empty(t, result.Pools)
	assert.Empty(t, result.Tokens)
	assert.Empty(t, result.Liquidities)
}

func TestPumpFun_ExtractDexData_NilBlocks(t *testing.T) {
	ext := NewPumpFunExtractor()
	ctx := context.Background()

	result, err := ext.ExtractDexData(ctx, nil)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Empty(t, result.Transactions)
}

func TestPumpFun_ExtractDexData_TradeEvent_DataTooShort(t *testing.T) {
	ext := NewPumpFunExtractor()
	ctx := context.Background()

	tests := []struct {
		name string
		size int
	}{
		{"Empty after discriminator", 8},
		{"Only mint", 8 + 32},
		{"Missing user and timestamp", 8 + 32 + 8 + 8 + 1},
		{"One byte short", 8 + 89 - 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eventData := make([]byte, tt.size)
			copy(eventData[:8], pumpFunTradeDiscriminator)
			block := solanaBlock(700, []solanaTx{{hash: "tx_short", events: [][]byte{eventData}}})

			result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
			require.NoError(t, err)
			assert.Empty(t, result.Transactions, "should skip trade event with %d bytes", tt.size)
		})
	}
}

func TestPumpFun_ExtractDexData_CreateEvent_DataTooShort(t *testing.T) {
	ext := NewPumpFunExtractor()
	ctx := context.Background()

	// Need at least 108 bytes after discriminator: 3 strings (4+0 each) + 3 pubkeys (32 each)
	eventData := make([]byte, 8+50) // way too short
	copy(eventData[:8], pumpFunCreateDiscriminator)
	block := solanaBlock(701, []solanaTx{{hash: "tx_short_create", events: [][]byte{eventData}}})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Empty(t, result.Pools)
	assert.Empty(t, result.Tokens)
}

func TestPumpFun_ExtractDexData_CompleteEvent_DataTooShort(t *testing.T) {
	ext := NewPumpFunExtractor()
	ctx := context.Background()

	// Need at least 104 bytes after discriminator
	eventData := make([]byte, 8+50) // way too short
	copy(eventData[:8], pumpFunCompleteDiscriminator)
	block := solanaBlock(702, []solanaTx{{hash: "tx_short_complete", events: [][]byte{eventData}}})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Empty(t, result.Liquidities)
}

func TestPumpFun_ExtractDexData_EventTooShortForDiscriminator(t *testing.T) {
	ext := NewPumpFunExtractor()
	ctx := context.Background()

	// Events shorter than 8 bytes should be skipped entirely.
	// However, ExtractSolanaEventData already filters events < 8 bytes at decode time.
	// Construct a log message that base64-decodes to exactly 7 bytes.
	shortData := make([]byte, 7)
	shortLog := "Program data: " + base64.StdEncoding.EncodeToString(shortData)
	block := types.UnifiedBlock{
		BlockNumber: big.NewInt(703),
		ChainType:   types.ChainTypeSolana,
		Transactions: []types.UnifiedTransaction{
			{
				TxHash:    "tx_tiny",
				ChainType: types.ChainTypeSolana,
				Timestamp: time.Unix(1700000000, 0),
				RawData: map[string]any{
					"log_messages": []any{shortLog},
				},
			},
		},
	}

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Empty(t, result.Transactions)
}

func TestPumpFun_ExtractDexData_UnknownDiscriminator(t *testing.T) {
	ext := NewPumpFunExtractor()
	ctx := context.Background()

	unknownEvent := make([]byte, 100)
	copy(unknownEvent[:8], []byte{1, 2, 3, 4, 5, 6, 7, 8})
	block := solanaBlock(704, []solanaTx{{hash: "tx_unknown", events: [][]byte{unknownEvent}}})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Empty(t, result.Transactions)
	assert.Empty(t, result.Pools)
	assert.Empty(t, result.Liquidities)
}

func TestPumpFun_ExtractDexData_NoLogMessages(t *testing.T) {
	ext := NewPumpFunExtractor()
	ctx := context.Background()

	block := types.UnifiedBlock{
		BlockNumber: big.NewInt(705),
		ChainType:   types.ChainTypeSolana,
		Transactions: []types.UnifiedTransaction{
			{
				TxHash:    "tx_no_logs",
				ChainType: types.ChainTypeSolana,
				Timestamp: time.Unix(1700000000, 0),
				RawData:   map[string]any{},
			},
		},
	}

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Empty(t, result.Transactions)
}

func TestPumpFun_ExtractDexData_NilRawData(t *testing.T) {
	ext := NewPumpFunExtractor()
	ctx := context.Background()

	block := types.UnifiedBlock{
		BlockNumber: big.NewInt(706),
		ChainType:   types.ChainTypeSolana,
		Transactions: []types.UnifiedTransaction{
			{
				TxHash:    "tx_nil_raw",
				ChainType: types.ChainTypeSolana,
				Timestamp: time.Unix(1700000000, 0),
				RawData:   nil,
			},
		},
	}

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	assert.Empty(t, result.Transactions)
}

func TestPumpFun_ExtractDexData_SwapIndexResetPerTx(t *testing.T) {
	ext := NewPumpFunExtractor()
	ctx := context.Background()

	trade1 := buildPumpFunTradeEventData(0x01, 1_000_000_000, 500_000, true, 0x02, 1700000000)
	trade2 := buildPumpFunTradeEventData(0x03, 2_000_000_000, 1_000_000, false, 0x04, 1700000001)

	// Two separate transactions, each with one trade
	block := solanaBlock(800, []solanaTx{
		{hash: "tx_a", events: [][]byte{trade1}},
		{hash: "tx_b", events: [][]byte{trade2}},
	})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
	require.NoError(t, err)
	require.Len(t, result.Transactions, 2)

	// SwapIndex should reset to 0 for each transaction
	assert.Equal(t, int64(0), result.Transactions[0].SwapIndex)
	assert.Equal(t, int64(0), result.Transactions[1].SwapIndex)
}

func TestPumpFun_DiscriminatorMatching_TableDriven(t *testing.T) {
	tests := []struct {
		name     string
		disc     []byte
		expected string
	}{
		{"TradeDiscriminator", pumpFunTradeDiscriminator, "trade"},
		{"CreateDiscriminator", pumpFunCreateDiscriminator, "create"},
		{"CompleteDiscriminator", pumpFunCompleteDiscriminator, "complete"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Len(t, tt.disc, 8, "discriminator should be 8 bytes")

			// Should not match other discriminators
			for _, other := range tests {
				if other.name == tt.name {
					continue
				}
				matched := true
				for i := range tt.disc {
					if tt.disc[i] != other.disc[i] {
						matched = false
						break
					}
				}
				assert.False(t, matched, "%s should not match %s", tt.name, other.name)
			}
		})
	}
}

func TestPumpFun_ExtractDexData_PriceCalculation_TableDriven(t *testing.T) {
	ext := NewPumpFunExtractor()
	ctx := context.Background()

	tests := []struct {
		name          string
		solAmount     uint64
		tokenAmount   uint64
		expectedPrice float64
	}{
		{
			name:          "1 SOL for 1000 tokens",
			solAmount:     1_000_000_000,
			tokenAmount:   1_000_000_000,
			expectedPrice: 1.0 / 1e3,
		},
		{
			name:          "0.1 SOL for 100 tokens",
			solAmount:     100_000_000,
			tokenAmount:   100_000_000,
			expectedPrice: 1.0 / 1e3,
		},
		{
			name:          "Zero SOL amount",
			solAmount:     0,
			tokenAmount:   1_000_000,
			expectedPrice: 0,
		},
		{
			name:          "Large amounts",
			solAmount:     100_000_000_000, // 100 SOL
			tokenAmount:   50_000_000_000,  // 50000 tokens
			expectedPrice: 100_000_000_000.0 / 50_000_000_000.0 / 1e3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := buildPumpFunTradeEventData(0x01, tt.solAmount, tt.tokenAmount, true, 0x02, 1700000000)
			block := solanaBlock(900, []solanaTx{{hash: "tx_price_" + tt.name, events: [][]byte{event}}})

			result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block})
			require.NoError(t, err)

			if tt.tokenAmount == 0 || tt.solAmount == 0 {
				if len(result.Transactions) > 0 {
					assert.Equal(t, float64(0), result.Transactions[0].Price)
				}
			} else {
				require.Len(t, result.Transactions, 1)
				assert.InDelta(t, tt.expectedPrice, result.Transactions[0].Price, 1e-15)
			}
		})
	}
}

func TestPumpFun_ExtractDexData_MultipleBlocks(t *testing.T) {
	ext := NewPumpFunExtractor()
	ctx := context.Background()

	trade1 := buildPumpFunTradeEventData(0x01, 1_000_000_000, 500_000, true, 0x02, 1700000000)
	trade2 := buildPumpFunTradeEventData(0x03, 2_000_000_000, 1_000_000, false, 0x04, 1700000001)

	block1 := solanaBlock(100, []solanaTx{{hash: "tx_block1", events: [][]byte{trade1}}})
	block2 := solanaBlock(101, []solanaTx{{hash: "tx_block2", events: [][]byte{trade2}}})

	result, err := ext.ExtractDexData(ctx, []types.UnifiedBlock{block1, block2})
	require.NoError(t, err)
	require.Len(t, result.Transactions, 2)

	assert.Equal(t, int64(100), result.Transactions[0].BlockNumber)
	assert.Equal(t, int64(101), result.Transactions[1].BlockNumber)
}
