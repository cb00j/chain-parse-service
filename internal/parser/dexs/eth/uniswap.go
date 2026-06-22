package eth

import (
	"context"
	"fmt"
	"math/big"

	"unified-tx-parser/internal/model"
	dex "unified-tx-parser/internal/parser/dexs"
	"unified-tx-parser/internal/types"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

const (
	// Uniswap V2 contract addresses
	uniswapV2FactoryAddr = "0x5C69bEe701ef814a2B6a3EDD4B1652CB9cc5aA6f"

	// Uniswap V3 contract addresses
	uniswapV3FactoryAddr = "0x1F98431c8aD98523631AE4a59f267346ea31F984"

	// Event signatures (shared with PancakeSwap)
	swapV2EventSig      = "0xd78ad95fa46c994b6551d0da85fc275fe613ce37657fb8d5e3d130840159d822"
	swapV3EventSig      = "0xc42079f94a6350d7e6235f29174924f928cc2ac818eb64fed8004e115fbcca67"
	mintV2EventSig      = "0x4c209b5fc8ad50758f13e2e1088ba56a560dff690a1c6fef26394f4c03821c4f"
	burnV2EventSig      = "0xdccd412f0b1252819cb1fd330b93224ca42612892bb3f4f789976e6d81936496"
	mintV3EventSig      = "0x7a53080ba414158be7ec69b987b5fb7d07dee101fe85488f0853ae16239d0bde"
	burnV3EventSig      = "0x0c396cd989a39f4459b5fa1aed6a9a8dcdbc45908acfd67e028cd568da98982c"
	pairCreatedEventSig = "0x0d3648bd0f6ba80134a33ba9275ac585d9d315f0ad8355cddefde31afa28d0e9"
	poolCreatedEventSig = "0x783cca1c0412dd0d695e784568c96da2e9c22ff989357a2e8b1d9b2b4e6b7118"
)

// UniswapExtractor parses Uniswap V2/V3 DEX events on Ethereum and BSC.
type UniswapExtractor struct {
	*dex.EVMDexExtractor
}

// NewUniswapExtractor creates a Uniswap extractor with EVM base class.
func NewUniswapExtractor() *UniswapExtractor {
	cfg := &dex.BaseDexExtractorConfig{
		Protocols:        []string{"uniswap", "uniswap-v2", "uniswap-v3"},
		SupportedChains:  []types.ChainType{types.ChainTypeEthereum},
		LoggerModuleName: "dex-uniswap",
	}
	return &UniswapExtractor{
		EVMDexExtractor: dex.NewEVMDexExtractor(cfg),
	}
}

func (u *UniswapExtractor) ExtractDexData(ctx context.Context, blocks []types.UnifiedBlock) (*types.DexData, error) {
	dexData := &types.DexData{
		Pools:        make([]model.Pool, 0),
		Transactions: make([]model.Transaction, 0),
		Liquidities:  make([]model.Liquidity, 0),
		Reserves:     make([]model.Reserve, 0),
		Tokens:       make([]model.Token, 0),
	}

	for _, block := range blocks {
		if !u.IsChainSupported(block.ChainType) {
			continue
		}

		u.GetLogger().Debugf("processing block %s with %d transactions", block.BlockNumber.String(), len(block.Transactions))

		for _, tx := range block.Transactions {
			// FIX #4: Use shared ExtractEVMLogsFromTransaction instead of duplicate code
			ethLogs := dex.ExtractEVMLogsFromTransaction(&tx)
			if len(ethLogs) == 0 {
				continue
			}

			// FIX #2: Track swapIdx per transaction, pass logIdx as eventIndex
			swapIdx := int64(0)
			for _, log := range ethLogs {
				if !u.isUniswapLog(log) {
					continue
				}

				logType := u.getLogType(log)
				eventIndex := dex.ExtractEventIndex(log)
				u.GetLogger().Debugf("found uniswap log, type: %s, address: %s", logType, log.Address.Hex())

				switch logType {
				case "swap_v2":
					if modelTx := u.parseV2Swap(log, &tx, eventIndex, swapIdx); modelTx != nil {
						dexData.Transactions = append(dexData.Transactions, *modelTx)
						swapIdx++
					}
				case "swap_v3":
					if modelTx := u.parseV3Swap(log, &tx, eventIndex, swapIdx); modelTx != nil {
						dexData.Transactions = append(dexData.Transactions, *modelTx)
						swapIdx++
					}
				case "mint":
					if liq := u.parseLiquidity(log, &tx, "add", eventIndex); liq != nil {
						dexData.Liquidities = append(dexData.Liquidities, *liq)
					}
				case "burn":
					if liq := u.parseLiquidity(log, &tx, "remove", eventIndex); liq != nil {
						dexData.Liquidities = append(dexData.Liquidities, *liq)
					}
				case "pair_created":
					if pool := u.parseV2PairCreated(log, &tx); pool != nil {
						dexData.Pools = append(dexData.Pools, *pool)
					}
				case "pool_created":
					if pool := u.parseV3PoolCreated(log, &tx); pool != nil {
						dexData.Pools = append(dexData.Pools, *pool)
					}
				}
			}
		}
	}

	return dexData, nil
}

func (u *UniswapExtractor) SupportsBlock(block *types.UnifiedBlock) bool {
	if !u.IsChainSupported(block.ChainType) {
		return false
	}
	for _, tx := range block.Transactions {
		ethLogs := dex.ExtractEVMLogsFromTransaction(&tx)
		for _, log := range ethLogs {
			if u.isUniswapLog(log) {
				return true
			}
		}
	}
	return false
}

func (u *UniswapExtractor) isUniswapLog(log *ethtypes.Log) bool {
	if len(log.Topics) == 0 {
		return false
	}
	topic0 := log.Topics[0].Hex()
	return topic0 == swapV2EventSig ||
		topic0 == swapV3EventSig ||
		topic0 == mintV2EventSig ||
		topic0 == burnV2EventSig ||
		topic0 == mintV3EventSig ||
		topic0 == burnV3EventSig ||
		topic0 == pairCreatedEventSig ||
		topic0 == poolCreatedEventSig
}

func (u *UniswapExtractor) getLogType(log *ethtypes.Log) string {
	if len(log.Topics) == 0 {
		return ""
	}
	topic0 := log.Topics[0].Hex()
	switch topic0 {
	case swapV2EventSig:
		return "swap_v2"
	case swapV3EventSig:
		return "swap_v3"
	case mintV2EventSig, mintV3EventSig:
		return "mint"
	case burnV2EventSig, burnV3EventSig:
		return "burn"
	case pairCreatedEventSig:
		return "pair_created"
	case poolCreatedEventSig:
		return "pool_created"
	default:
		return ""
	}
}

// parseV2Swap parses V2 Swap(address indexed sender, uint256 amount0In, uint256 amount1In, uint256 amount0Out, uint256 amount1Out, address indexed to)
func (u *UniswapExtractor) parseV2Swap(log *ethtypes.Log, tx *types.UnifiedTransaction, logIdx, swapIdx int64) *model.Transaction {
	if len(log.Data) < 128 {
		u.GetLogger().WithField("tx_hash", tx.TxHash).Warn("V2 swap log data too short")
		return nil
	}

	amount0In := new(big.Int).SetBytes(log.Data[0:32])
	amount1In := new(big.Int).SetBytes(log.Data[32:64])
	amount0Out := new(big.Int).SetBytes(log.Data[64:96])
	amount1Out := new(big.Int).SetBytes(log.Data[96:128])

	var amountIn, amountOut *big.Int
	if amount0In.Sign() > 0 {
		amountIn = amount0In
		amountOut = amount1Out
	} else {
		amountIn = amount1In
		amountOut = amount0Out
	}

	price := dex.CalcPrice(amountIn, amountOut)
	value := dex.CalcValue(amountIn, price)
	poolAddr := log.Address.Hex()

	return &model.Transaction{
		Addr:        poolAddr,
		Router:      tx.ToAddress,
		Factory:     uniswapV2FactoryAddr,
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

// parseV3Swap parses V3 Swap(address indexed sender, address indexed recipient, int256 amount0, int256 amount1, uint160 sqrtPriceX96, uint128 liquidity, int24 tick)
func (u *UniswapExtractor) parseV3Swap(log *ethtypes.Log, tx *types.UnifiedTransaction, logIdx, swapIdx int64) *model.Transaction {
	if len(log.Data) < 160 {
		u.GetLogger().WithField("tx_hash", tx.TxHash).Warn("V3 swap log data too short")
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
		Factory:     uniswapV3FactoryAddr,
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
func (u *UniswapExtractor) parseLiquidity(log *ethtypes.Log, tx *types.UnifiedTransaction, side string, logIdx int64) *model.Liquidity {
	if len(log.Data) < 64 {
		return nil
	}

	topic0 := log.Topics[0].Hex()

	var amount0, amount1 *big.Int
	switch topic0 {
	case mintV3EventSig:
		// V3 Mint data: [address sender, uint128 amount, uint256 amount0, uint256 amount1]
		if len(log.Data) >= 128 {
			amount0 = new(big.Int).SetBytes(log.Data[64:96])
			amount1 = new(big.Int).SetBytes(log.Data[96:128])
		}
	case burnV3EventSig:
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
		Factory: u.getFactoryAddress(log),
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
// FIX #5: Pool address from data[0:32], not log.Address
func (u *UniswapExtractor) parseV2PairCreated(log *ethtypes.Log, tx *types.UnifiedTransaction) *model.Pool {
	if len(log.Topics) < 3 || len(log.Data) < 64 {
		return nil
	}

	token0 := common.BytesToAddress(log.Topics[1].Bytes()).Hex()
	token1 := common.BytesToAddress(log.Topics[2].Bytes()).Hex()
	pairAddr := common.BytesToAddress(log.Data[0:32]).Hex()

	return &model.Pool{
		Addr:     pairAddr,
		Factory:  uniswapV2FactoryAddr,
		Protocol: "uniswap",
		Tokens:   map[int]string{0: token0, 1: token1},
		Fee:      3000,
		Extra: &model.PoolExtra{
			Hash: tx.TxHash,
			From: tx.FromAddress,
			Time: uint64(tx.Timestamp.Unix()),
		},
	}
}

// parseV3PoolCreated parses PoolCreated(address indexed token0, address indexed token1, uint24 indexed fee, int24 tickSpacing, address pool)
// FIX #5: Pool address from data[32:64], not log.Address
func (u *UniswapExtractor) parseV3PoolCreated(log *ethtypes.Log, tx *types.UnifiedTransaction) *model.Pool {
	if len(log.Topics) < 4 || len(log.Data) < 64 {
		return nil
	}

	token0 := common.BytesToAddress(log.Topics[1].Bytes()).Hex()
	token1 := common.BytesToAddress(log.Topics[2].Bytes()).Hex()
	fee := new(big.Int).SetBytes(log.Topics[3].Bytes())
	poolAddr := common.BytesToAddress(log.Data[32:64]).Hex()

	return &model.Pool{
		Addr:     poolAddr,
		Factory:  uniswapV3FactoryAddr,
		Protocol: "uniswap",
		Tokens:   map[int]string{0: token0, 1: token1},
		Fee:      int(fee.Int64()),
		Extra: &model.PoolExtra{
			Hash: tx.TxHash,
			From: tx.FromAddress,
			Time: uint64(tx.Timestamp.Unix()),
		},
	}
}

func (u *UniswapExtractor) getFactoryAddress(log *ethtypes.Log) string {
	if len(log.Topics) == 0 {
		return ""
	}
	topic0 := log.Topics[0].Hex()
	switch topic0 {
	case swapV2EventSig, mintV2EventSig, burnV2EventSig, pairCreatedEventSig:
		return uniswapV2FactoryAddr
	case swapV3EventSig, mintV3EventSig, burnV3EventSig, poolCreatedEventSig:
		return uniswapV3FactoryAddr
	default:
		return ""
	}
}
