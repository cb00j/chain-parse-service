package solanadex

import (
	"context"
	"fmt"
	"math/big"

	"unified-tx-parser/internal/model"
	dex "unified-tx-parser/internal/parser/dexs"
	"unified-tx-parser/internal/types"
)

const (
	pumpSwapProgramID = "pAMMBay6oceH9fJKBRHGP5D4bD4sWpmSwMn52FMfXEA"
)

// PumpSwap event discriminators (first 8 bytes of Anchor event data)
var (
	pumpSwapBuyDiscriminator        = []byte{103, 244, 82, 31, 44, 245, 119, 119}
	pumpSwapSellDiscriminator       = []byte{62, 47, 55, 10, 165, 3, 220, 42}
	pumpSwapCreatePoolDiscriminator = []byte{177, 49, 12, 210, 160, 118, 167, 116}
	pumpSwapDepositDiscriminator    = []byte{120, 248, 61, 83, 31, 142, 107, 144}
	pumpSwapWithdrawDiscriminator   = []byte{22, 9, 133, 26, 160, 44, 71, 192}
)

// PumpSwapExtractor parses PumpSwap AMM events on Solana.
type PumpSwapExtractor struct {
	*dex.SolanaDexExtractor
}

// NewPumpSwapExtractor creates a PumpSwap extractor with the Solana base class.
func NewPumpSwapExtractor() *PumpSwapExtractor {
	cfg := &dex.BaseDexExtractorConfig{
		Protocols:        []string{"pumpswap"},
		SupportedChains:  []types.ChainType{types.ChainTypeSolana},
		LoggerModuleName: "dex-pumpswap",
	}
	return &PumpSwapExtractor{
		SolanaDexExtractor: dex.NewSolanaDexExtractor(cfg),
	}
}

// ExtractDexData extracts PumpSwap DEX data from unified blocks.
func (ps *PumpSwapExtractor) ExtractDexData(ctx context.Context, blocks []types.UnifiedBlock) (*types.DexData, error) {
	dexData := &types.DexData{
		Pools:        make([]model.Pool, 0),
		Transactions: make([]model.Transaction, 0),
		Liquidities:  make([]model.Liquidity, 0),
		Reserves:     make([]model.Reserve, 0),
		Tokens:       make([]model.Token, 0),
	}

	for _, block := range blocks {
		if !ps.IsChainSupported(block.ChainType) {
			continue
		}

		for _, tx := range block.Transactions {
			events := dex.ExtractSolanaEventData(&tx)
			if len(events) == 0 {
				continue
			}

			swapIdx := int64(0)
			for eventIdx, eventData := range events {
				if len(eventData) < 8 {
					continue
				}
				disc := eventData[:8]

				switch {
				case dex.MatchDiscriminatorBytes(disc, pumpSwapBuyDiscriminator):
					if modelTx := ps.parseBuyEvent(eventData[8:], &tx, int64(eventIdx), swapIdx); modelTx != nil {
						dexData.Transactions = append(dexData.Transactions, *modelTx)
						swapIdx++
					}

				case dex.MatchDiscriminatorBytes(disc, pumpSwapSellDiscriminator):
					if modelTx := ps.parseSellEvent(eventData[8:], &tx, int64(eventIdx), swapIdx); modelTx != nil {
						dexData.Transactions = append(dexData.Transactions, *modelTx)
						swapIdx++
					}

				case dex.MatchDiscriminatorBytes(disc, pumpSwapCreatePoolDiscriminator):
					if pool := ps.parseCreatePoolEvent(eventData[8:], &tx); pool != nil {
						dexData.Pools = append(dexData.Pools, *pool)
					}

				case dex.MatchDiscriminatorBytes(disc, pumpSwapDepositDiscriminator):
					if liq := ps.parseDepositEvent(eventData[8:], &tx, int64(eventIdx)); liq != nil {
						dexData.Liquidities = append(dexData.Liquidities, *liq)
					}

				case dex.MatchDiscriminatorBytes(disc, pumpSwapWithdrawDiscriminator):
					if liq := ps.parseWithdrawEvent(eventData[8:], &tx, int64(eventIdx)); liq != nil {
						dexData.Liquidities = append(dexData.Liquidities, *liq)
					}
				}
			}
		}
	}

	return dexData, nil
}

// SupportsBlock checks if any transaction in the block contains PumpSwap events.
func (ps *PumpSwapExtractor) SupportsBlock(block *types.UnifiedBlock) bool {
	if !ps.IsChainSupported(block.ChainType) {
		return false
	}
	for _, tx := range block.Transactions {
		events := dex.ExtractSolanaEventData(&tx)
		for _, eventData := range events {
			if len(eventData) < 8 {
				continue
			}
			disc := eventData[:8]
			if dex.MatchDiscriminatorBytes(disc, pumpSwapBuyDiscriminator) ||
				dex.MatchDiscriminatorBytes(disc, pumpSwapSellDiscriminator) ||
				dex.MatchDiscriminatorBytes(disc, pumpSwapCreatePoolDiscriminator) ||
				dex.MatchDiscriminatorBytes(disc, pumpSwapDepositDiscriminator) ||
				dex.MatchDiscriminatorBytes(disc, pumpSwapWithdrawDiscriminator) {
				return true
			}
		}
	}
	return false
}

// parseBuyEvent parses a PumpSwap BuyEvent from Borsh-encoded data (after discriminator).
//
// Layout:
//
//	base_amount_out:    u64
//	quote_amount_in:    u64
//	lp_fee:             u64
//	protocol_fee:       u64
//	pool:               Pubkey (32)
//	user:               Pubkey (32)
//	base_mint:          Pubkey (32)
//	quote_mint:         Pubkey (32)
func (ps *PumpSwapExtractor) parseBuyEvent(data []byte, tx *types.UnifiedTransaction, eventIdx, swapIdx int64) *model.Transaction {
	// Minimum: 4*u64(32) + 4*Pubkey(128) = 160 bytes
	if len(data) < 160 {
		ps.GetLogger().WithField("tx_hash", tx.TxHash).Debug("PumpSwap buy: data too short")
		return nil
	}

	off := 0

	var baseAmountOut, quoteAmountIn, lpFee, protocolFee uint64
	baseAmountOut, off = dex.ParseU64LE(data, off)
	quoteAmountIn, off = dex.ParseU64LE(data, off)
	lpFee, off = dex.ParseU64LE(data, off)
	protocolFee, off = dex.ParseU64LE(data, off)
	_ = lpFee
	_ = protocolFee

	var pool, user, baseMint, quoteMint string
	pool, off = dex.ParsePubkey(data, off)
	user, off = dex.ParsePubkey(data, off)
	baseMint, off = dex.ParsePubkey(data, off)
	quoteMint, off = dex.ParsePubkey(data, off)
	_ = off

	if pool == "" || baseMint == "" {
		ps.GetLogger().WithField("tx_hash", tx.TxHash).Debug("PumpSwap buy: failed to parse pool or base_mint")
		return nil
	}

	// Price: SOL per base token (normalize: quote=SOL 9 decimals, base=token 6 decimals)
	// price = (quoteAmountIn / 1e9) / (baseAmountOut / 1e6) = quoteAmountIn / baseAmountOut / 1e3
	var price float64
	if baseAmountOut > 0 {
		price = float64(quoteAmountIn) / float64(baseAmountOut) / 1e3
	}

	quoteAmountBig := new(big.Int).SetUint64(quoteAmountIn)
	value := dex.LamportsToSOL(quoteAmountIn)

	return &model.Transaction{
		Addr:        baseMint,
		Router:      pumpSwapProgramID,
		Factory:     pumpSwapProgramID,
		Pool:        pool,
		Hash:        tx.TxHash,
		From:        user,
		Side:        "buy",
		Amount:      quoteAmountBig,
		Price:       price,
		Value:       value,
		Time:        uint64(tx.Timestamp.Unix()),
		EventIndex:  eventIdx,
		TxIndex:     int64(tx.TxIndex),
		SwapIndex:   swapIdx,
		BlockNumber: dex.GetBlockNumber(tx),
		Extra: &model.TransactionExtra{
			QuoteAddr:     quoteMint,
			QuotePrice:    fmt.Sprintf("%.18f", price),
			Type:          "swap",
			TokenDecimals: 6,
		},
	}
}

// parseSellEvent parses a PumpSwap SellEvent from Borsh-encoded data (after discriminator).
//
// Layout:
//
//	base_amount_in:     u64
//	quote_amount_out:   u64
//	lp_fee:             u64
//	protocol_fee:       u64
//	pool:               Pubkey (32)
//	user:               Pubkey (32)
//	base_mint:          Pubkey (32)
//	quote_mint:         Pubkey (32)
func (ps *PumpSwapExtractor) parseSellEvent(data []byte, tx *types.UnifiedTransaction, eventIdx, swapIdx int64) *model.Transaction {
	// Minimum: 4*u64(32) + 4*Pubkey(128) = 160 bytes
	if len(data) < 160 {
		ps.GetLogger().WithField("tx_hash", tx.TxHash).Debug("PumpSwap sell: data too short")
		return nil
	}

	off := 0

	var baseAmountIn, quoteAmountOut, lpFee, protocolFee uint64
	baseAmountIn, off = dex.ParseU64LE(data, off)
	quoteAmountOut, off = dex.ParseU64LE(data, off)
	lpFee, off = dex.ParseU64LE(data, off)
	protocolFee, off = dex.ParseU64LE(data, off)
	_ = lpFee
	_ = protocolFee

	var pool, user, baseMint, quoteMint string
	pool, off = dex.ParsePubkey(data, off)
	user, off = dex.ParsePubkey(data, off)
	baseMint, off = dex.ParsePubkey(data, off)
	quoteMint, off = dex.ParsePubkey(data, off)
	_ = off

	if pool == "" || baseMint == "" {
		ps.GetLogger().WithField("tx_hash", tx.TxHash).Debug("PumpSwap sell: failed to parse pool or base_mint")
		return nil
	}

	// Price: SOL per base token (normalize: quote=SOL 9 decimals, base=token 6 decimals)
	// price = (quoteAmountOut / 1e9) / (baseAmountIn / 1e6) = quoteAmountOut / baseAmountIn / 1e3
	var price float64
	if baseAmountIn > 0 {
		price = float64(quoteAmountOut) / float64(baseAmountIn) / 1e3
	}

	quoteAmountBig := new(big.Int).SetUint64(quoteAmountOut)
	value := dex.LamportsToSOL(quoteAmountOut)

	return &model.Transaction{
		Addr:        baseMint,
		Router:      pumpSwapProgramID,
		Factory:     pumpSwapProgramID,
		Pool:        pool,
		Hash:        tx.TxHash,
		From:        user,
		Side:        "sell",
		Amount:      quoteAmountBig,
		Price:       price,
		Value:       value,
		Time:        uint64(tx.Timestamp.Unix()),
		EventIndex:  eventIdx,
		TxIndex:     int64(tx.TxIndex),
		SwapIndex:   swapIdx,
		BlockNumber: dex.GetBlockNumber(tx),
		Extra: &model.TransactionExtra{
			QuoteAddr:     quoteMint,
			QuotePrice:    fmt.Sprintf("%.18f", price),
			Type:          "swap",
			TokenDecimals: 6,
		},
	}
}

// parseCreatePoolEvent parses a PumpSwap CreatePoolEvent from Borsh-encoded data (after discriminator).
//
// Layout:
//
//	creator:              Pubkey (32)
//	base_mint:            Pubkey (32)
//	quote_mint:           Pubkey (32)
//	lp_token_amount_out:  u64
//	pool:                 Pubkey (32)
//	lp_mint:              Pubkey (32)
//	base_amount_in:       u64
//	quote_amount_in:      u64
func (ps *PumpSwapExtractor) parseCreatePoolEvent(data []byte, tx *types.UnifiedTransaction) *model.Pool {
	// Minimum: 3*Pubkey(96) + u64(8) + 2*Pubkey(64) + 2*u64(16) = 184 bytes
	if len(data) < 184 {
		ps.GetLogger().WithField("tx_hash", tx.TxHash).Debug("PumpSwap create pool: data too short")
		return nil
	}

	off := 0

	var creator, baseMint, quoteMint string
	creator, off = dex.ParsePubkey(data, off)
	baseMint, off = dex.ParsePubkey(data, off)
	quoteMint, off = dex.ParsePubkey(data, off)

	var lpTokenAmountOut uint64
	lpTokenAmountOut, off = dex.ParseU64LE(data, off)
	_ = lpTokenAmountOut

	var pool, lpMint string
	pool, off = dex.ParsePubkey(data, off)
	lpMint, off = dex.ParsePubkey(data, off)
	_ = lpMint

	var baseAmountIn, quoteAmountIn uint64
	baseAmountIn, off = dex.ParseU64LE(data, off)
	quoteAmountIn, off = dex.ParseU64LE(data, off)
	_ = baseAmountIn
	_ = quoteAmountIn
	_ = off

	if pool == "" || baseMint == "" || quoteMint == "" {
		ps.GetLogger().WithField("tx_hash", tx.TxHash).Debug("PumpSwap create pool: failed to parse required fields")
		return nil
	}

	return &model.Pool{
		Addr:     pool,
		Factory:  pumpSwapProgramID,
		Protocol: "pumpswap",
		Tokens:   map[int]string{0: baseMint, 1: quoteMint},
		Fee:      30, // PumpSwap default 0.3% (30 bps)
		Extra: &model.PoolExtra{
			Hash: tx.TxHash,
			From: creator,
			Time: uint64(tx.Timestamp.Unix()),
		},
	}
}

// parseDepositEvent parses a PumpSwap DepositEvent from Borsh-encoded data (after discriminator).
//
// Layout:
//
//	base_amount_in:       u64
//	quote_amount_in:      u64
//	lp_token_amount_out:  u64
//	pool:                 Pubkey (32)
//	user:                 Pubkey (32)
func (ps *PumpSwapExtractor) parseDepositEvent(data []byte, tx *types.UnifiedTransaction, eventIdx int64) *model.Liquidity {
	// Minimum: 3*u64(24) + 2*Pubkey(64) = 88 bytes
	if len(data) < 88 {
		ps.GetLogger().WithField("tx_hash", tx.TxHash).Debug("PumpSwap deposit: data too short")
		return nil
	}

	off := 0

	var quoteAmountIn, lpTokenAmountOut uint64
	_, off = dex.ParseU64LE(data, off) // base_amount_in (skip, unknown decimals)
	quoteAmountIn, off = dex.ParseU64LE(data, off)
	lpTokenAmountOut, off = dex.ParseU64LE(data, off)
	_ = lpTokenAmountOut

	var pool, user string
	pool, off = dex.ParsePubkey(data, off)
	user, off = dex.ParsePubkey(data, off)
	_ = off

	if pool == "" {
		ps.GetLogger().WithField("tx_hash", tx.TxHash).Debug("PumpSwap deposit: failed to parse pool")
		return nil
	}

	// Use quote (SOL) amount for Value since base token value is unknown without price oracle
	quoteAmountBig := new(big.Int).SetUint64(quoteAmountIn)
	quoteValue := dex.LamportsToSOL(quoteAmountIn)

	key := fmt.Sprintf("%s_add_%d", tx.TxHash, eventIdx)

	return &model.Liquidity{
		Addr:    pool,
		Router:  pumpSwapProgramID,
		Factory: pumpSwapProgramID,
		Pool:    pool,
		Hash:    tx.TxHash,
		From:    user,
		Side:    "add",
		Amount:  quoteAmountBig,
		Value:   quoteValue * 2, // Approximate: assume equal value on both sides
		Time:    uint64(tx.Timestamp.Unix()),
		Key:     key,
		Extra: &model.LiquidityExtra{
			Key:     key,
			Amounts: new(big.Int).SetUint64(quoteAmountIn),
			Values:  []float64{0, quoteValue}, // base value unknown, quote in SOL
			Time:    uint64(tx.Timestamp.Unix()),
		},
	}
}

// parseWithdrawEvent parses a PumpSwap WithdrawEvent from Borsh-encoded data (after discriminator).
//
// Layout:
//
//	lp_token_amount_in:   u64
//	base_amount_out:      u64
//	quote_amount_out:     u64
//	pool:                 Pubkey (32)
//	user:                 Pubkey (32)
func (ps *PumpSwapExtractor) parseWithdrawEvent(data []byte, tx *types.UnifiedTransaction, eventIdx int64) *model.Liquidity {
	// Minimum: 3*u64(24) + 2*Pubkey(64) = 88 bytes
	if len(data) < 88 {
		ps.GetLogger().WithField("tx_hash", tx.TxHash).Debug("PumpSwap withdraw: data too short")
		return nil
	}

	off := 0

	var quoteAmountOut uint64
	_, off = dex.ParseU64LE(data, off) // lp_token_amount_in (skip)
	_, off = dex.ParseU64LE(data, off) // base_amount_out (skip, unknown decimals)
	quoteAmountOut, off = dex.ParseU64LE(data, off)

	var pool, user string
	pool, off = dex.ParsePubkey(data, off)
	user, off = dex.ParsePubkey(data, off)
	_ = off

	if pool == "" {
		ps.GetLogger().WithField("tx_hash", tx.TxHash).Debug("PumpSwap withdraw: failed to parse pool")
		return nil
	}

	// Use quote (SOL) amount for Value since base token value is unknown without price oracle
	quoteAmountBig := new(big.Int).SetUint64(quoteAmountOut)
	quoteValue := dex.LamportsToSOL(quoteAmountOut)

	key := fmt.Sprintf("%s_remove_%d", tx.TxHash, eventIdx)

	return &model.Liquidity{
		Addr:    pool,
		Router:  pumpSwapProgramID,
		Factory: pumpSwapProgramID,
		Pool:    pool,
		Hash:    tx.TxHash,
		From:    user,
		Side:    "remove",
		Amount:  quoteAmountBig,
		Value:   quoteValue * 2, // Approximate: assume equal value on both sides
		Time:    uint64(tx.Timestamp.Unix()),
		Key:     key,
		Extra: &model.LiquidityExtra{
			Key:     key,
			Amounts: new(big.Int).SetUint64(quoteAmountOut),
			Values:  []float64{0, quoteValue}, // base value unknown, quote in SOL
			Time:    uint64(tx.Timestamp.Unix()),
		},
	}
}
