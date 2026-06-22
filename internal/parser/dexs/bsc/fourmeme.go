package bsc

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"unified-tx-parser/internal/model"
	dex "unified-tx-parser/internal/parser/dexs"
	"unified-tx-parser/internal/types"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

const (
	// TokenManager V1 contract address (tokens created before 2024-09-05)
	fourMemeV1Addr = "0xEC4549caDcE5DA21Df6E6422d448034B5233bFbC"
	// TokenManager V2 contract address (tokens created after 2024-09-05)
	fourMemeV2Addr = "0x5c952063c7fc8610FFDB798152D69F0B9550762b"

	// V2 event signatures
	fourMemeV2TokenCreateSig    = "0x396d5e902b675b032348d3d2e9517ee8f0c4a926603fbc075d3d282ff00cad20"
	fourMemeV2TokenPurchaseSig  = "0x7db52723a3b2cdd6164364b3b766e65e540d7be48ffa89582956d8eaebe62942"
	fourMemeV2TokenSaleSig      = "0x0a5575b3648bae2210cee56bf33254cc1ddfbc7bf637c0af2ac18b14fb1bae19"
	fourMemeV2LiquidityAddedSig = "0xc18aa71171b358b706fe3dd345299685ba21a5316c66ffa9e319268b033c44b0"

	// V1 event signatures
	fourMemeV1TokenCreateSig   = "0xc60523754e4c8d044ae75f841c3a7f27fefeed24c086155510c2ae0edf538fa0"
	fourMemeV1TokenPurchaseSig = "0x623b3804fa71d67900d064613da8f94b9617215ee90799290593e1745087ad18"
	fourMemeV1TokenSaleSig     = "0x3aa3f154f6bf5e3490d1a7205aa8d1412e76d26f9d186830de86fb9309224040"
)

var (
	fourMemeV1AddrLower = strings.ToLower(fourMemeV1Addr)
	fourMemeV2AddrLower = strings.ToLower(fourMemeV2Addr)
)

// FourMemeExtractor parses FourMeme V1/V2 DEX events on BSC.
type FourMemeExtractor struct {
	*dex.EVMDexExtractor
}

// NewFourMemeExtractor creates a FourMeme extractor with EVM base class.
func NewFourMemeExtractor() *FourMemeExtractor {
	cfg := &dex.BaseDexExtractorConfig{
		Protocols:        []string{"fourmeme"},
		SupportedChains:  []types.ChainType{types.ChainTypeBSC},
		LoggerModuleName: "dex-fourmeme",
	}
	return &FourMemeExtractor{
		EVMDexExtractor: dex.NewEVMDexExtractor(cfg),
	}
}

func (f *FourMemeExtractor) SupportsBlock(block *types.UnifiedBlock) bool {
	if block.ChainType != types.ChainTypeBSC {
		return false
	}
	for _, tx := range block.Transactions {
		ethLogs := dex.ExtractEVMLogsFromTransaction(&tx)
		for _, log := range ethLogs {
			if f.isFourMemeLog(log) {
				return true
			}
		}
	}
	return false
}

func (f *FourMemeExtractor) ExtractDexData(ctx context.Context, blocks []types.UnifiedBlock) (*types.DexData, error) {
	dexData := &types.DexData{
		Pools:        make([]model.Pool, 0),
		Transactions: make([]model.Transaction, 0),
		Liquidities:  make([]model.Liquidity, 0),
		Reserves:     make([]model.Reserve, 0),
		Tokens:       make([]model.Token, 0),
	}

	for _, block := range blocks {
		if block.ChainType != types.ChainTypeBSC {
			continue
		}

		for _, tx := range block.Transactions {
			ethLogs := dex.ExtractEVMLogsFromTransaction(&tx)
			if len(ethLogs) == 0 {
				continue
			}

			swapIdx := int64(0)
			for logIdx, log := range ethLogs {
				if !f.isFourMemeLog(log) {
					continue
				}

				topic0 := log.Topics[0].Hex()
				contractVersion := f.getContractVersion(log)

				switch {
				case topic0 == fourMemeV2TokenPurchaseSig && contractVersion == 2:
					if modelTx := f.parseV2Purchase(log, &tx, int64(logIdx), swapIdx); modelTx != nil {
						dexData.Transactions = append(dexData.Transactions, *modelTx)
						swapIdx++
					}

				case topic0 == fourMemeV2TokenSaleSig && contractVersion == 2:
					if modelTx := f.parseV2Sale(log, &tx, int64(logIdx), swapIdx); modelTx != nil {
						dexData.Transactions = append(dexData.Transactions, *modelTx)
						swapIdx++
					}

				case topic0 == fourMemeV1TokenPurchaseSig && contractVersion == 1:
					if modelTx := f.parseV1Purchase(log, &tx, int64(logIdx), swapIdx); modelTx != nil {
						dexData.Transactions = append(dexData.Transactions, *modelTx)
						swapIdx++
					}

				case topic0 == fourMemeV1TokenSaleSig && contractVersion == 1:
					if modelTx := f.parseV1Sale(log, &tx, int64(logIdx), swapIdx); modelTx != nil {
						dexData.Transactions = append(dexData.Transactions, *modelTx)
						swapIdx++
					}

				case topic0 == fourMemeV2TokenCreateSig && contractVersion == 2:
					if pool := f.parseV2TokenCreate(log, &tx); pool != nil {
						dexData.Pools = append(dexData.Pools, *pool)
					}

				case topic0 == fourMemeV1TokenCreateSig && contractVersion == 1:
					if pool := f.parseV1TokenCreate(log, &tx); pool != nil {
						dexData.Pools = append(dexData.Pools, *pool)
					}

				case topic0 == fourMemeV2LiquidityAddedSig && contractVersion == 2:
					if liq := f.parseV2LiquidityAdded(log, &tx, int64(logIdx)); liq != nil {
						dexData.Liquidities = append(dexData.Liquidities, *liq)
					}
				}
			}
		}
	}

	return dexData, nil
}

func (f *FourMemeExtractor) isFourMemeLog(log *ethtypes.Log) bool {
	if len(log.Topics) == 0 {
		return false
	}
	addr := strings.ToLower(log.Address.Hex())
	if addr != fourMemeV1AddrLower && addr != fourMemeV2AddrLower {
		return false
	}

	topic0 := log.Topics[0].Hex()
	return topic0 == fourMemeV2TokenCreateSig ||
		topic0 == fourMemeV2TokenPurchaseSig ||
		topic0 == fourMemeV2TokenSaleSig ||
		topic0 == fourMemeV2LiquidityAddedSig ||
		topic0 == fourMemeV1TokenCreateSig ||
		topic0 == fourMemeV1TokenPurchaseSig ||
		topic0 == fourMemeV1TokenSaleSig
}

func (f *FourMemeExtractor) getContractVersion(log *ethtypes.Log) int {
	addr := strings.ToLower(log.Address.Hex())
	switch addr {
	case fourMemeV1AddrLower:
		return 1
	case fourMemeV2AddrLower:
		return 2
	default:
		return 0
	}
}

// parseV2Purchase parses V2 TokenPurchase(address token, address account, uint256 price, uint256 amount, uint256 cost, uint256 fee, uint256 offers, uint256 funds)
// 8 ABI params, all non-indexed, data = 8 * 32 = 256 bytes
func (f *FourMemeExtractor) parseV2Purchase(log *ethtypes.Log, tx *types.UnifiedTransaction, logIdx, swapIdx int64) *model.Transaction {
	if len(log.Data) < 256 {
		f.GetLogger().WithField("tx_hash", tx.TxHash).Warnf("V2 TokenPurchase data too short: %d bytes", len(log.Data))
		return nil
	}

	tokenAddr := common.BytesToAddress(log.Data[0:32]).Hex()
	account := common.BytesToAddress(log.Data[32:64]).Hex()
	price := new(big.Int).SetBytes(log.Data[64:96])
	amount := new(big.Int).SetBytes(log.Data[96:128])
	cost := new(big.Int).SetBytes(log.Data[128:160])
	fee := new(big.Int).SetBytes(log.Data[160:192])

	priceFloat := weiToFloat(price)
	costFloat := weiToFloat(cost)
	feeFloat := weiToFloat(fee)

	return &model.Transaction{
		Addr:        tokenAddr,
		Router:      fourMemeV2Addr,
		Factory:     fourMemeV2Addr,
		Pool:        tokenAddr,
		Hash:        tx.TxHash,
		From:        account,
		Side:        "buy",
		Amount:      amount,
		Price:       priceFloat,
		Value:       costFloat,
		Time:        uint64(tx.Timestamp.Unix()),
		EventIndex:  logIdx,
		TxIndex:     int64(tx.TxIndex),
		SwapIndex:   swapIdx,
		BlockNumber: dex.GetBlockNumber(tx),
		Extra: &model.TransactionExtra{
			QuotePrice:    fmt.Sprintf("%.18f", priceFloat),
			Type:          "buy",
			TokenDecimals: 18,
			QuoteAddr:     fmt.Sprintf("fee:%.18f", feeFloat),
		},
	}
}

// parseV2Sale parses V2 TokenSale(address token, address account, uint256 price, uint256 amount, uint256 cost, uint256 fee, uint256 offers, uint256 funds)
func (f *FourMemeExtractor) parseV2Sale(log *ethtypes.Log, tx *types.UnifiedTransaction, logIdx, swapIdx int64) *model.Transaction {
	if len(log.Data) < 256 {
		f.GetLogger().WithField("tx_hash", tx.TxHash).Warnf("V2 TokenSale data too short: %d bytes", len(log.Data))
		return nil
	}

	tokenAddr := common.BytesToAddress(log.Data[0:32]).Hex()
	account := common.BytesToAddress(log.Data[32:64]).Hex()
	price := new(big.Int).SetBytes(log.Data[64:96])
	amount := new(big.Int).SetBytes(log.Data[96:128])
	cost := new(big.Int).SetBytes(log.Data[128:160])
	fee := new(big.Int).SetBytes(log.Data[160:192])

	priceFloat := weiToFloat(price)
	costFloat := weiToFloat(cost)
	feeFloat := weiToFloat(fee)

	return &model.Transaction{
		Addr:        tokenAddr,
		Router:      fourMemeV2Addr,
		Factory:     fourMemeV2Addr,
		Pool:        tokenAddr,
		Hash:        tx.TxHash,
		From:        account,
		Side:        "sell",
		Amount:      amount,
		Price:       priceFloat,
		Value:       costFloat,
		Time:        uint64(tx.Timestamp.Unix()),
		EventIndex:  logIdx,
		TxIndex:     int64(tx.TxIndex),
		SwapIndex:   swapIdx,
		BlockNumber: dex.GetBlockNumber(tx),
		Extra: &model.TransactionExtra{
			QuotePrice:    fmt.Sprintf("%.18f", priceFloat),
			Type:          "sell",
			TokenDecimals: 18,
			QuoteAddr:     fmt.Sprintf("fee:%.18f", feeFloat),
		},
	}
}

// parseV1Purchase parses V1 TokenPurchase(address token, address account, uint256 tokenAmount, uint256 etherAmount)
// 4 ABI params, all non-indexed, data = 4 * 32 = 128 bytes
func (f *FourMemeExtractor) parseV1Purchase(log *ethtypes.Log, tx *types.UnifiedTransaction, logIdx, swapIdx int64) *model.Transaction {
	if len(log.Data) < 128 {
		f.GetLogger().WithField("tx_hash", tx.TxHash).Warnf("V1 TokenPurchase data too short: %d bytes", len(log.Data))
		return nil
	}

	tokenAddr := common.BytesToAddress(log.Data[0:32]).Hex()
	account := common.BytesToAddress(log.Data[32:64]).Hex()
	tokenAmount := new(big.Int).SetBytes(log.Data[64:96])
	etherAmount := new(big.Int).SetBytes(log.Data[96:128])

	price := dex.CalcPrice(etherAmount, tokenAmount)
	costFloat := weiToFloat(etherAmount)

	return &model.Transaction{
		Addr:        tokenAddr,
		Router:      fourMemeV1Addr,
		Factory:     fourMemeV1Addr,
		Pool:        tokenAddr,
		Hash:        tx.TxHash,
		From:        account,
		Side:        "buy",
		Amount:      tokenAmount,
		Price:       price,
		Value:       costFloat,
		Time:        uint64(tx.Timestamp.Unix()),
		EventIndex:  logIdx,
		TxIndex:     int64(tx.TxIndex),
		SwapIndex:   swapIdx,
		BlockNumber: dex.GetBlockNumber(tx),
		Extra: &model.TransactionExtra{
			QuotePrice:    fmt.Sprintf("%.18f", price),
			Type:          "buy",
			TokenDecimals: 18,
		},
	}
}

// parseV1Sale parses V1 TokenSale(address token, address account, uint256 tokenAmount, uint256 etherAmount)
func (f *FourMemeExtractor) parseV1Sale(log *ethtypes.Log, tx *types.UnifiedTransaction, logIdx, swapIdx int64) *model.Transaction {
	if len(log.Data) < 128 {
		f.GetLogger().WithField("tx_hash", tx.TxHash).Warnf("V1 TokenSale data too short: %d bytes", len(log.Data))
		return nil
	}

	tokenAddr := common.BytesToAddress(log.Data[0:32]).Hex()
	account := common.BytesToAddress(log.Data[32:64]).Hex()
	tokenAmount := new(big.Int).SetBytes(log.Data[64:96])
	etherAmount := new(big.Int).SetBytes(log.Data[96:128])

	price := dex.CalcPrice(etherAmount, tokenAmount)
	costFloat := weiToFloat(etherAmount)

	return &model.Transaction{
		Addr:        tokenAddr,
		Router:      fourMemeV1Addr,
		Factory:     fourMemeV1Addr,
		Pool:        tokenAddr,
		Hash:        tx.TxHash,
		From:        account,
		Side:        "sell",
		Amount:      tokenAmount,
		Price:       price,
		Value:       costFloat,
		Time:        uint64(tx.Timestamp.Unix()),
		EventIndex:  logIdx,
		TxIndex:     int64(tx.TxIndex),
		SwapIndex:   swapIdx,
		BlockNumber: dex.GetBlockNumber(tx),
		Extra: &model.TransactionExtra{
			QuotePrice:    fmt.Sprintf("%.18f", price),
			Type:          "sell",
			TokenDecimals: 18,
		},
	}
}

// parseV2TokenCreate parses V2 TokenCreate(address creator, address token, uint256 requestId, string name, string symbol, uint256 totalSupply, uint256 launchTime, uint256 launchFee)
// Note: string types use dynamic encoding (offset + length + data), so data >= 256 bytes
func (f *FourMemeExtractor) parseV2TokenCreate(log *ethtypes.Log, tx *types.UnifiedTransaction) *model.Pool {
	if len(log.Data) < 256 {
		f.GetLogger().WithField("tx_hash", tx.TxHash).Warnf("V2 TokenCreate data too short: %d bytes", len(log.Data))
		return nil
	}

	creator := common.BytesToAddress(log.Data[0:32]).Hex()
	tokenAddr := common.BytesToAddress(log.Data[32:64]).Hex()

	return &model.Pool{
		Addr:     tokenAddr,
		Factory:  fourMemeV2Addr,
		Protocol: "fourmeme",
		Tokens:   map[int]string{0: tokenAddr},
		Fee:      0,
		Args: map[string]interface{}{
			"creator": creator,
			"version": 2,
		},
		Extra: &model.PoolExtra{
			Hash: tx.TxHash,
			From: creator,
			Time: uint64(tx.Timestamp.Unix()),
		},
	}
}

// parseV1TokenCreate parses V1 TokenCreate(address creator, address token, uint256 requestId, string name, string symbol, uint256 totalSupply, uint256 launchTime)
func (f *FourMemeExtractor) parseV1TokenCreate(log *ethtypes.Log, tx *types.UnifiedTransaction) *model.Pool {
	if len(log.Data) < 224 {
		f.GetLogger().WithField("tx_hash", tx.TxHash).Warnf("V1 TokenCreate data too short: %d bytes", len(log.Data))
		return nil
	}

	creator := common.BytesToAddress(log.Data[0:32]).Hex()
	tokenAddr := common.BytesToAddress(log.Data[32:64]).Hex()

	return &model.Pool{
		Addr:     tokenAddr,
		Factory:  fourMemeV1Addr,
		Protocol: "fourmeme",
		Tokens:   map[int]string{0: tokenAddr},
		Fee:      0,
		Args: map[string]interface{}{
			"creator": creator,
			"version": 1,
		},
		Extra: &model.PoolExtra{
			Hash: tx.TxHash,
			From: creator,
			Time: uint64(tx.Timestamp.Unix()),
		},
	}
}

// parseV2LiquidityAdded parses V2 LiquidityAdded(address base, uint256 offers, address quote, uint256 funds)
// 4 ABI params, data = 128 bytes
func (f *FourMemeExtractor) parseV2LiquidityAdded(log *ethtypes.Log, tx *types.UnifiedTransaction, logIdx int64) *model.Liquidity {
	if len(log.Data) < 128 {
		f.GetLogger().WithField("tx_hash", tx.TxHash).Warnf("V2 LiquidityAdded data too short: %d bytes", len(log.Data))
		return nil
	}

	baseAddr := common.BytesToAddress(log.Data[0:32]).Hex()
	offers := new(big.Int).SetBytes(log.Data[32:64])
	_ = common.BytesToAddress(log.Data[64:96]) // quoteAddr: address(0) = BNB, otherwise BEP20
	funds := new(big.Int).SetBytes(log.Data[96:128])

	fundsFloat := weiToFloat(funds)
	key := fmt.Sprintf("%s_liquidity_%d", tx.TxHash, logIdx)

	return &model.Liquidity{
		Addr:    baseAddr,
		Router:  fourMemeV2Addr,
		Factory: fourMemeV2Addr,
		Pool:    baseAddr,
		Hash:    tx.TxHash,
		From:    tx.FromAddress,
		Side:    "add",
		Amount:  offers,
		Value:   fundsFloat,
		Time:    uint64(tx.Timestamp.Unix()),
		Key:     key,
		Extra: &model.LiquidityExtra{
			Key:     key,
			Amounts: funds,
			Values:  []float64{weiToFloat(offers), fundsFloat},
			Time:    uint64(tx.Timestamp.Unix()),
		},
	}
}


func weiToFloat(wei *big.Int) float64 {
	if wei == nil || wei.Sign() == 0 {
		return 0
	}
	return dex.ConvertDecimals(wei, 18)
}
