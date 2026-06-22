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
	pumpFunProgramID = "6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P"
)

// PumpFun event discriminators (first 8 bytes of Anchor event data)
var (
	pumpFunCreateDiscriminator   = []byte{27, 114, 169, 77, 222, 235, 99, 118}
	pumpFunTradeDiscriminator    = []byte{189, 219, 127, 211, 78, 230, 97, 238}
	pumpFunCompleteDiscriminator = []byte{95, 114, 97, 156, 212, 46, 152, 8}
)

// PumpFunExtractor parses PumpFun DEX events on Solana.
type PumpFunExtractor struct {
	*dex.SolanaDexExtractor
}

// NewPumpFunExtractor creates a PumpFun extractor with the Solana base class.
func NewPumpFunExtractor() *PumpFunExtractor {
	cfg := &dex.BaseDexExtractorConfig{
		Protocols:        []string{"pumpfun"},
		SupportedChains:  []types.ChainType{types.ChainTypeSolana},
		LoggerModuleName: "dex-pumpfun",
	}
	return &PumpFunExtractor{
		SolanaDexExtractor: dex.NewSolanaDexExtractor(cfg),
	}
}

// ExtractDexData extracts PumpFun DEX data from unified blocks.
func (p *PumpFunExtractor) ExtractDexData(ctx context.Context, blocks []types.UnifiedBlock) (*types.DexData, error) {
	dexData := &types.DexData{
		Pools:        make([]model.Pool, 0),
		Transactions: make([]model.Transaction, 0),
		Liquidities:  make([]model.Liquidity, 0),
		Reserves:     make([]model.Reserve, 0),
		Tokens:       make([]model.Token, 0),
	}

	for _, block := range blocks {
		if !p.IsChainSupported(block.ChainType) {
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
				case dex.MatchDiscriminatorBytes(disc, pumpFunTradeDiscriminator):
					if modelTx := p.parseTradeEvent(eventData[8:], &tx, int64(eventIdx), swapIdx); modelTx != nil {
						dexData.Transactions = append(dexData.Transactions, *modelTx)
						swapIdx++
					}

				case dex.MatchDiscriminatorBytes(disc, pumpFunCreateDiscriminator):
					pool, token := p.parseCreateEvent(eventData[8:], &tx)
					if pool != nil {
						dexData.Pools = append(dexData.Pools, *pool)
					}
					if token != nil {
						dexData.Tokens = append(dexData.Tokens, *token)
					}

				case dex.MatchDiscriminatorBytes(disc, pumpFunCompleteDiscriminator):
					if liq := p.parseCompleteEvent(eventData[8:], &tx, int64(eventIdx)); liq != nil {
						dexData.Liquidities = append(dexData.Liquidities, *liq)
					}
				}
			}
		}
	}

	return dexData, nil
}

// SupportsBlock checks if any transaction in the block contains PumpFun events.
func (p *PumpFunExtractor) SupportsBlock(block *types.UnifiedBlock) bool {
	if !p.IsChainSupported(block.ChainType) {
		return false
	}
	for _, tx := range block.Transactions {
		events := dex.ExtractSolanaEventData(&tx)
		for _, eventData := range events {
			if len(eventData) < 8 {
				continue
			}
			disc := eventData[:8]
			if dex.MatchDiscriminatorBytes(disc, pumpFunTradeDiscriminator) ||
				dex.MatchDiscriminatorBytes(disc, pumpFunCreateDiscriminator) ||
				dex.MatchDiscriminatorBytes(disc, pumpFunCompleteDiscriminator) {
				return true
			}
		}
	}
	return false
}

// parseTradeEvent parses a PumpFun TradeEvent from Borsh-encoded data (after discriminator).
//
// Layout:
//
//	mint:                    Pubkey (32)
//	sol_amount:              u64
//	token_amount:            u64
//	is_buy:                  bool (1)
//	user:                    Pubkey (32)
//	timestamp:               i64
//	virtual_sol_reserves:    u64
//	virtual_token_reserves:  u64
//	real_sol_reserves:       u64
//	real_token_reserves:     u64
//	--- optional fields (may not exist in older versions) ---
//	fee_recipient:           Pubkey (32)
//	fee_basis_points:        u64
//	fee:                     u64
//	creator:                 Pubkey (32)
//	creator_fee_basis_points: u64
//	creator_fee:             u64
func (p *PumpFunExtractor) parseTradeEvent(data []byte, tx *types.UnifiedTransaction, eventIdx, swapIdx int64) *model.Transaction {
	// Minimum required: mint(32) + sol_amount(8) + token_amount(8) + is_buy(1) + user(32) + timestamp(8) = 89 bytes
	if len(data) < 89 {
		p.GetLogger().WithField("tx_hash", tx.TxHash).Debug("PumpFun trade: data too short")
		return nil
	}

	off := 0

	var mint string
	mint, off = dex.ParsePubkey(data, off)

	var solAmount, tokenAmount uint64
	solAmount, off = dex.ParseU64LE(data, off)
	tokenAmount, off = dex.ParseU64LE(data, off)

	var isBuy bool
	isBuy, off = dex.ParseBool(data, off)

	var user string
	user, off = dex.ParsePubkey(data, off)

	var timestamp int64
	timestamp, off = dex.ParseI64LE(data, off)
	_ = off // remaining optional fields not needed for core transaction

	if mint == "" || user == "" {
		p.GetLogger().WithField("tx_hash", tx.TxHash).Debug("PumpFun trade: failed to parse mint or user")
		return nil
	}

	side := "sell"
	if isBuy {
		side = "buy"
	}

	// Price: SOL per token (normalize decimals: SOL has 9, PumpFun tokens have 6)
	// price = (solAmount / 1e9) / (tokenAmount / 1e6) = solAmount / tokenAmount / 1e3
	var price float64
	if tokenAmount > 0 {
		price = float64(solAmount) / float64(tokenAmount) / 1e3
	}

	solAmountBig := new(big.Int).SetUint64(solAmount)
	value := dex.LamportsToSOL(solAmount)

	txTime := uint64(tx.Timestamp.Unix())
	if timestamp > 0 {
		txTime = uint64(timestamp)
	}

	return &model.Transaction{
		Addr:        mint,
		Router:      pumpFunProgramID,
		Factory:     pumpFunProgramID,
		Pool:        mint, // PumpFun uses mint as the bonding curve identifier
		Hash:        tx.TxHash,
		From:        user,
		Side:        side,
		Amount:      solAmountBig,
		Price:       price,
		Value:       value,
		Time:        txTime,
		EventIndex:  eventIdx,
		TxIndex:     int64(tx.TxIndex),
		SwapIndex:   swapIdx,
		BlockNumber: dex.GetBlockNumber(tx),
		Extra: &model.TransactionExtra{
			QuotePrice:    fmt.Sprintf("%.18f", price),
			Type:          "swap",
			TokenDecimals: 6, // PumpFun token decimals
		},
	}
}

// parseCreateEvent parses a PumpFun CreateEvent from Borsh-encoded data (after discriminator).
//
// Layout:
//
//	name:                  String (4-byte len + UTF-8)
//	symbol:                String
//	uri:                   String
//	mint:                  Pubkey (32)
//	bonding_curve:         Pubkey (32)
//	user:                  Pubkey (32)
//	--- optional fields (may not exist in older versions) ---
//	creator:               Pubkey (32)
//	timestamp:             i64
//	virtual_token_reserves: u64
//	virtual_sol_reserves:  u64
//	real_token_reserves:   u64
//	token_total_supply:    u64
func (p *PumpFunExtractor) parseCreateEvent(data []byte, tx *types.UnifiedTransaction) (*model.Pool, *model.Token) {
	// Minimum: 3 strings (4+0 each minimum = 12) + 3 pubkeys (32 each = 96) = 108 bytes minimum
	if len(data) < 108 {
		p.GetLogger().WithField("tx_hash", tx.TxHash).Debug("PumpFun create: data too short")
		return nil, nil
	}

	off := 0

	var name, symbol, uri string
	name, off = dex.ParseString(data, off)
	symbol, off = dex.ParseString(data, off)
	uri, off = dex.ParseString(data, off)

	var mint, bondingCurve, user string
	mint, off = dex.ParsePubkey(data, off)
	bondingCurve, off = dex.ParsePubkey(data, off)
	user, off = dex.ParsePubkey(data, off)
	_ = off // remaining optional fields parsed only if needed

	if mint == "" || bondingCurve == "" {
		p.GetLogger().WithField("tx_hash", tx.TxHash).Debug("PumpFun create: failed to parse mint or bonding_curve")
		return nil, nil
	}

	pool := &model.Pool{
		Addr:     bondingCurve,
		Factory:  pumpFunProgramID,
		Protocol: "pumpfun",
		Tokens:   map[int]string{0: mint, 1: "So11111111111111111111111111111111"}, // SOL native mint
		Fee:      100,                                                               // PumpFun default 1%
		Extra: &model.PoolExtra{
			Hash: tx.TxHash,
			From: user,
			Time: uint64(tx.Timestamp.Unix()),
		},
	}

	token := &model.Token{
		Addr:      mint,
		Name:      name,
		Symbol:    symbol,
		Decimals:  6, // PumpFun tokens default to 6 decimals
		CreatedAt: tx.Timestamp.Format("2006-01-02T15:04:05Z"),
	}

	// Store URI in pool Args for reference
	if uri != "" {
		pool.Args = map[string]any{"uri": uri}
	}

	return pool, token
}

// parseCompleteEvent parses a PumpFun CompleteEvent from Borsh-encoded data (after discriminator).
//
// Layout:
//
//	user:          Pubkey (32)
//	mint:          Pubkey (32)
//	bonding_curve: Pubkey (32)
//	timestamp:     i64
func (p *PumpFunExtractor) parseCompleteEvent(data []byte, tx *types.UnifiedTransaction, eventIdx int64) *model.Liquidity {
	// Minimum: user(32) + mint(32) + bonding_curve(32) + timestamp(8) = 104 bytes
	if len(data) < 104 {
		p.GetLogger().WithField("tx_hash", tx.TxHash).Debug("PumpFun complete: data too short")
		return nil
	}

	off := 0

	var user, mint, bondingCurve string
	user, off = dex.ParsePubkey(data, off)
	mint, off = dex.ParsePubkey(data, off)
	bondingCurve, off = dex.ParsePubkey(data, off)

	var timestamp int64
	timestamp, _ = dex.ParseI64LE(data, off)

	if mint == "" || bondingCurve == "" {
		p.GetLogger().WithField("tx_hash", tx.TxHash).Debug("PumpFun complete: failed to parse mint or bonding_curve")
		return nil
	}

	txTime := uint64(tx.Timestamp.Unix())
	if timestamp > 0 {
		txTime = uint64(timestamp)
	}

	key := fmt.Sprintf("%s_graduate_%d", tx.TxHash, eventIdx)

	return &model.Liquidity{
		Addr:    bondingCurve,
		Router:  pumpFunProgramID,
		Factory: pumpFunProgramID,
		Pool:    bondingCurve,
		Hash:    tx.TxHash,
		From:    user,
		Side:    "graduate",
		Amount:  big.NewInt(0),
		Value:   0,
		Time:    txTime,
		Key:     key,
		Extra: &model.LiquidityExtra{
			Key:  key,
			Time: txTime,
		},
	}
}
