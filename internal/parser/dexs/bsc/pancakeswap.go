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
	pancakeSwapV2FactoryAddr = "0xcA143Ce32Fe78f1f7019d7d551a6402fC5350c73"
	pancakeSwapV3FactoryAddr = "0x0BFbCF9fa4f9C56B0F40a671Ad40E0805A091865"

	pancakeSwapV2EventSig      = "0xd78ad95fa46c994b6551d0da85fc275fe613ce37657fb8d5e3d130840159d822"
	pancakeSwapV3EventSig      = "0xc42079f94a6350d7e6235f29174924f928cc2ac818eb64fed8004e115fbcca67"
	pancakeMintV2EventSig      = "0x4c209b5fc8ad50758f13e2e1088ba56a560dff690a1c6fef26394f4c03821c4f"
	pancakeBurnV2EventSig      = "0xdccd412f0b1252819cb1fd330b93224ca42612892bb3f4f789976e6d81936496"
	pancakeMintV3EventSig      = "0x7a53080ba414158be7ec69b987b5fb7d07dee101fe85488f0853ae16239d0bde"
	pancakeBurnV3EventSig      = "0x0c396cd989a39f4459b5fa1aed6a9a8dcdbc45908acfd67e028cd568da98982c"
	pancakePairCreatedEventSig = "0x0d3648bd0f6ba80134a33ba9275ac585d9d315f0ad8355cddefde31afa28d0e9"
	pancakePoolCreatedEventSig = "0x783cca1c0412dd0d695e784568c96da2e9c22ff989357a2e8b1d9b2b4e6b7118"
)

// PancakeSwapExtractor parses PancakeSwap V2/V3 DEX events on BSC and Ethereum.
type PancakeSwapExtractor struct {
	*dex.EVMDexExtractor
}

// NewPancakeSwapExtractor creates a PancakeSwap extractor with EVM base class.
func NewPancakeSwapExtractor() *PancakeSwapExtractor {
	cfg := &dex.BaseDexExtractorConfig{
		Protocols:        []string{"pancakeswap", "pancakeswap-v2", "pancakeswap-v3"},
		SupportedChains:  []types.ChainType{types.ChainTypeBSC},
		LoggerModuleName: "dex-pancakeswap",
	}
	return &PancakeSwapExtractor{
		EVMDexExtractor: dex.NewEVMDexExtractor(cfg),
	}
}

func (p *PancakeSwapExtractor) ExtractDexData(ctx context.Context, blocks []types.UnifiedBlock) (*types.DexData, error) {
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
			ethLogs := dex.ExtractEVMLogsFromTransaction(&tx)
			if len(ethLogs) == 0 {
				continue
			}

			swapIdx := int64(0)
			for _, log := range ethLogs {
				if !p.isPancakeSwapLog(log) {
					continue
				}

				logType := p.getLogType(log)
				eventIndex := dex.ExtractEventIndex(log)
				switch logType {
				case "swap_v2":
					if modelTx := p.parseV2Swap(log, &tx, eventIndex, swapIdx); modelTx != nil {
						dexData.Transactions = append(dexData.Transactions, *modelTx)
						swapIdx++
					}
				case "swap_v3":
					if modelTx := p.parseV3Swap(log, &tx, eventIndex, swapIdx); modelTx != nil {
						dexData.Transactions = append(dexData.Transactions, *modelTx)
						swapIdx++
					}
				case "mint":
					if liq := p.parseLiquidity(log, &tx, "add", eventIndex); liq != nil {
						dexData.Liquidities = append(dexData.Liquidities, *liq)
					}
				case "burn":
					if liq := p.parseLiquidity(log, &tx, "remove", eventIndex); liq != nil {
						dexData.Liquidities = append(dexData.Liquidities, *liq)
					}
				case "pair_created":
					if pool := p.parseV2PairCreated(log, &tx); pool != nil {
						dexData.Pools = append(dexData.Pools, *pool)
					}
				case "pool_created":
					if pool := p.parseV3PoolCreated(log, &tx); pool != nil {
						dexData.Pools = append(dexData.Pools, *pool)
					}
				}
			}
		}
	}

	return dexData, nil
}

func (p *PancakeSwapExtractor) SupportsBlock(block *types.UnifiedBlock) bool {
	if !p.IsChainSupported(block.ChainType) {
		return false
	}
	for _, tx := range block.Transactions {
		ethLogs := dex.ExtractEVMLogsFromTransaction(&tx)
		for _, log := range ethLogs {
			if p.isPancakeSwapLog(log) {
				return true
			}
		}
	}
	return false
}

func (p *PancakeSwapExtractor) isPancakeSwapLog(log *ethtypes.Log) bool {
	if len(log.Topics) == 0 {
		return false
	}
	topic0 := log.Topics[0].Hex()
	return topic0 == pancakeSwapV2EventSig ||
		topic0 == pancakeSwapV3EventSig ||
		topic0 == pancakeMintV2EventSig ||
		topic0 == pancakeBurnV2EventSig ||
		topic0 == pancakeMintV3EventSig ||
		topic0 == pancakeBurnV3EventSig ||
		topic0 == pancakePairCreatedEventSig ||
		topic0 == pancakePoolCreatedEventSig
}

func (p *PancakeSwapExtractor) getLogType(log *ethtypes.Log) string {
	if len(log.Topics) == 0 {
		return ""
	}
	topic0 := log.Topics[0].Hex()
	switch topic0 {
	case pancakeSwapV2EventSig:
		return "swap_v2"
	case pancakeSwapV3EventSig:
		return "swap_v3"
	case pancakeMintV2EventSig, pancakeMintV3EventSig:
		return "mint"
	case pancakeBurnV2EventSig, pancakeBurnV3EventSig:
		return "burn"
	case pancakePairCreatedEventSig:
		return "pair_created"
	case pancakePoolCreatedEventSig:
		return "pool_created"
	default:
		return ""
	}
}

// parseV2Swap parses V2 Swap(address indexed sender, uint256 amount0In, uint256 amount1In, uint256 amount0Out, uint256 amount1Out, address indexed to)
func (p *PancakeSwapExtractor) parseV2Swap(log *ethtypes.Log, tx *types.UnifiedTransaction, logIdx, swapIdx int64) *model.Transaction {
	if len(log.Data) < 128 {
		p.GetLogger().WithField("tx_hash", tx.TxHash).Warn("V2 swap log data too short")
		return nil
	}

	amount0In := new(big.Int).SetBytes(log.Data[0:32])
	amount1In := new(big.Int).SetBytes(log.Data[32:64])
	amount0Out := new(big.Int).SetBytes(log.Data[64:96])
	amount1Out := new(big.Int).SetBytes(log.Data[96:128])

	// Determine swap direction: nonzero amountIn side is what the user paid
	var amountIn, amountOut *big.Int
	var direction int // 0 = token0->token1, 1 = token1->token0
	if amount0In.Sign() > 0 {
		amountIn = amount0In
		amountOut = amount1Out
		direction = 0
	} else {
		amountIn = amount1In
		amountOut = amount0Out
		direction = 1
	}

	price := dex.CalcPrice(amountIn, amountOut)
	value := p.estimateV2SwapValue(amount0In, amount1In, amount0Out, amount1Out, direction)
	poolAddr := log.Address.Hex()

	return &model.Transaction{
		Addr:        poolAddr,
		Router:      tx.ToAddress,
		Factory:     pancakeSwapV2FactoryAddr,
		Pool:        poolAddr,
		Hash:        tx.TxHash,
		From:        tx.FromAddress,
		Side:        "swap",
		Amount:      amountIn,
		Price:       price,
		Value:       value,
		Time:        uint64(tx.Timestamp.Unix()),
		EventIndex:  logIdx,
		TxIndex:     int64(tx.TxIndex),
		SwapIndex:   swapIdx,
		BlockNumber: dex.GetBlockNumber(tx),
		Extra: &model.TransactionExtra{
			QuotePrice:    fmt.Sprintf("%.18f", price),
			Type:          "swap",
		},
	}
}

// estimateV2SwapValue estimates the USD value of a V2 swap using QuoteAssets.
// For V2 swaps we don't have token addresses in the event, so we use the raw
// amount * price calculation as a fallback. This provides a relative value that
// can be refined when pool token addresses are known from PairCreated events.
func (p *PancakeSwapExtractor) estimateV2SwapValue(amount0In, amount1In, amount0Out, amount1Out *big.Int, direction int) float64 {
	var amountIn, amountOut *big.Int
	if direction == 0 {
		amountIn = amount0In
		amountOut = amount1Out
	} else {
		amountIn = amount1In
		amountOut = amount0Out
	}

	price := dex.CalcPrice(amountIn, amountOut)
	return dex.CalcValue(amountIn, price)
}

// parseV3Swap parses V3 Swap(address indexed sender, address indexed recipient, int256 amount0, int256 amount1, uint160 sqrtPriceX96, uint128 liquidity, int24 tick)
func (p *PancakeSwapExtractor) parseV3Swap(log *ethtypes.Log, tx *types.UnifiedTransaction, logIdx, swapIdx int64) *model.Transaction {
	if len(log.Data) < 160 {
		p.GetLogger().WithField("tx_hash", tx.TxHash).Warn("V3 swap log data too short")
		return nil
	}

	// V3 Swap amounts are signed int256:
	//   positive = flows into pool (user pays)
	//   negative = flows out of pool (user receives)
	amount0 := dex.ToSignedInt256(log.Data[0:32])
	amount1 := dex.ToSignedInt256(log.Data[32:64])
	sqrtPriceX96 := new(big.Int).SetBytes(log.Data[64:96])

	// Pick the positive amount as amountIn (what user paid)
	amountIn := new(big.Int).Abs(amount0)
	amountOut := new(big.Int).Abs(amount1)
	if amount0.Sign() < 0 {
		amountIn, amountOut = amountOut, amountIn
	}

	price := dex.CalcV3Price(sqrtPriceX96)
	value := dex.CalcValue(amountIn, price)
	poolAddr := log.Address.Hex()

	return &model.Transaction{
		Addr:        poolAddr,
		Router:      tx.ToAddress,
		Factory:     pancakeSwapV3FactoryAddr,
		Pool:        poolAddr,
		Hash:        tx.TxHash,
		From:        tx.FromAddress,
		Side:        "swap",
		Amount:      amountIn,
		Price:       price,
		Value:       value,
		Time:        uint64(tx.Timestamp.Unix()),
		EventIndex:  logIdx,
		TxIndex:     int64(tx.TxIndex),
		SwapIndex:   swapIdx,
		BlockNumber: dex.GetBlockNumber(tx),
		Extra: &model.TransactionExtra{
			QuotePrice:    fmt.Sprintf("%.18f", price),
			Type:          "swap",
		},
	}
}

// parseLiquidity parses V2/V3 Mint/Burn events
func (p *PancakeSwapExtractor) parseLiquidity(log *ethtypes.Log, tx *types.UnifiedTransaction, side string, logIdx int64) *model.Liquidity {
	if len(log.Data) < 64 {
		return nil
	}

	topic0 := log.Topics[0].Hex()

	var amount0, amount1 *big.Int
	switch topic0 {
	case pancakeMintV3EventSig:
		// V3 Mint data: [address sender, uint128 amount, uint256 amount0, uint256 amount1]
		if len(log.Data) >= 128 {
			amount0 = new(big.Int).SetBytes(log.Data[64:96])
			amount1 = new(big.Int).SetBytes(log.Data[96:128])
		}
	case pancakeBurnV3EventSig:
		// V3 Burn data: [uint128 amount, uint256 amount0, uint256 amount1]
		if len(log.Data) >= 96 {
			amount0 = new(big.Int).SetBytes(log.Data[32:64])
			amount1 = new(big.Int).SetBytes(log.Data[64:96])
		}
	default:
		// V2 Mint/Burn data: [uint256 amount0, uint256 amount1]
		amount0 = new(big.Int).SetBytes(log.Data[0:32])
		amount1 = new(big.Int).SetBytes(log.Data[32:64])
	}

	if amount0 == nil {
		amount0 = big.NewInt(0)
	}
	if amount1 == nil {
		amount1 = big.NewInt(0)
	}

	totalAmount := new(big.Int).Add(amount0, amount1)
	val0, _ := new(big.Float).SetInt(amount0).Float64()
	val1, _ := new(big.Float).SetInt(amount1).Float64()

	poolAddr := log.Address.Hex()
	key := fmt.Sprintf("%s_%s_%d", tx.TxHash, side, logIdx)

	return &model.Liquidity{
		Addr:    poolAddr,
		Router:  tx.ToAddress,
		Factory: p.getFactoryAddress(log),
		Pool:    poolAddr,
		Hash:    tx.TxHash,
		From:    tx.FromAddress,
		Side:    side,
		Amount:  totalAmount,
		Value:   val0 + val1,
		Time:    uint64(tx.Timestamp.Unix()),
		Key:     key,
		Extra: &model.LiquidityExtra{
			Key:     key,
			Amounts: amount1,
			Values:  []float64{val0, val1},
			Time:    uint64(tx.Timestamp.Unix()),
		},
	}
}

// parseV2PairCreated parses PairCreated(address indexed token0, address indexed token1, address pair, uint256)
func (p *PancakeSwapExtractor) parseV2PairCreated(log *ethtypes.Log, tx *types.UnifiedTransaction) *model.Pool {
	if len(log.Topics) < 3 || len(log.Data) < 64 {
		return nil
	}

	token0 := common.BytesToAddress(log.Topics[1].Bytes()).Hex()
	token1 := common.BytesToAddress(log.Topics[2].Bytes()).Hex()
	pairAddr := common.BytesToAddress(log.Data[0:32]).Hex()

	return &model.Pool{
		Addr:     pairAddr,
		Factory:  pancakeSwapV2FactoryAddr,
		Protocol: "pancakeswap",
		Tokens:   map[int]string{0: token0, 1: token1},
		Fee:      2500,
		Extra: &model.PoolExtra{
			Hash: tx.TxHash,
			From: tx.FromAddress,
			Time: uint64(tx.Timestamp.Unix()),
		},
	}
}

// parseV3PoolCreated parses PoolCreated(address indexed token0, address indexed token1, uint24 indexed fee, int24 tickSpacing, address pool)
func (p *PancakeSwapExtractor) parseV3PoolCreated(log *ethtypes.Log, tx *types.UnifiedTransaction) *model.Pool {
	if len(log.Topics) < 4 || len(log.Data) < 64 {
		return nil
	}

	token0 := common.BytesToAddress(log.Topics[1].Bytes()).Hex()
	token1 := common.BytesToAddress(log.Topics[2].Bytes()).Hex()
	fee := new(big.Int).SetBytes(log.Topics[3].Bytes())
	poolAddr := common.BytesToAddress(log.Data[32:64]).Hex()

	return &model.Pool{
		Addr:     poolAddr,
		Factory:  pancakeSwapV3FactoryAddr,
		Protocol: "pancakeswap",
		Tokens:   map[int]string{0: token0, 1: token1},
		Fee:      int(fee.Int64()),
		Extra: &model.PoolExtra{
			Hash: tx.TxHash,
			From: tx.FromAddress,
			Time: uint64(tx.Timestamp.Unix()),
		},
	}
}

func (p *PancakeSwapExtractor) getFactoryAddress(log *ethtypes.Log) string {
	if len(log.Topics) == 0 {
		return ""
	}
	topic0 := log.Topics[0].Hex()
	switch topic0 {
	case pancakeSwapV2EventSig, pancakeMintV2EventSig, pancakeBurnV2EventSig, pancakePairCreatedEventSig:
		return pancakeSwapV2FactoryAddr
	case pancakeSwapV3EventSig, pancakeMintV3EventSig, pancakeBurnV3EventSig, pancakePoolCreatedEventSig:
		return pancakeSwapV3FactoryAddr
	default:
		return ""
	}
}

// isQuoteAsset checks if an address is a configured quote asset
func (p *PancakeSwapExtractor) isQuoteAsset(addr string) bool {
	if p.GetQuoteAssetRank(strings.ToLower(addr)) >= 0 {
		return true
	}
	return p.GetQuoteAssetRank(addr) >= 0
}


// getTokenSymbol returns a short symbol derived from the token address
func getTokenSymbol(tokenAddr string) string {
	if len(tokenAddr) >= 8 {
		return strings.ToUpper(tokenAddr[2:8])
	}
	return "UNKNOWN"
}
