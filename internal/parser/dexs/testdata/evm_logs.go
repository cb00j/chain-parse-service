package testdata

import (
	"math/big"
	"time"

	"unified-tx-parser/internal/types"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

// --- PancakeSwap / Uniswap V2 event signatures ---
const (
	SwapV2EventSig      = "0xd78ad95fa46c994b6551d0da85fc275fe613ce37657fb8d5e3d130840159d822"
	SwapV3EventSig      = "0xc42079f94a6350d7e6235f29174924f928cc2ac818eb64fed8004e115fbcca67"
	MintV2EventSig      = "0x4c209b5fc8ad50758f13e2e1088ba56a560dff690a1c6fef26394f4c03821c4f"
	BurnV2EventSig      = "0xdccd412f0b1252819cb1fd330b93224ca42612892bb3f4f789976e6d81936496"
	PairCreatedEventSig = "0x0d3648bd0f6ba80134a33ba9275ac585d9d315f0ad8355cddefde31afa28d0e9"
	PoolCreatedEventSig = "0x783cca1c0412dd0d695e784568c96da2e9c22ff989357a2e8b1d9b2b4e6b7118"

	// V3 event signatures (shared by PancakeSwap and Uniswap)
	MintV3EventSig      = "0x7a53080ba414158be7ec69b987b5fb7d07dee101fe85488f0853ae16239d0bde"
	BurnV3EventSig      = "0x0c396cd989a39f4459b5fa1aed6a9a8dcdbc45908acfd67e028cd568da98982c"

	// FourMeme event signatures
	FourMemeV2TokenPurchaseSig = "0x7db52723a3b2cdd6164364b3b766e65e540d7be48ffa89582956d8eaebe62942"
	FourMemeV2TokenSaleSig     = "0x0a5575b3648bae2210cee56bf33254cc1ddfbc7bf637c0af2ac18b14fb1bae19"
	FourMemeV2TokenCreateSig   = "0x396d5e902b675b032348d3d2e9517ee8f0c4a926603fbc075d3d282ff00cad20"
	FourMemeV2LiquidityAddSig  = "0xc18aa71171b358b706fe3dd345299685ba21a5316c66ffa9e319268b033c44b0"
	FourMemeV1TokenPurchaseSig = "0x623b3804fa71d67900d064613da8f94b9617215ee90799290593e1745087ad18"
	FourMemeV1TokenSaleSig     = "0x3aa3f154f6bf5e3490d1a7205aa8d1412e76d26f9d186830de86fb9309224040"
	FourMemeV1TokenCreateSig   = "0xc60523754e4c8d044ae75f841c3a7f27fefeed24c086155510c2ae0edf538fa0"

	FourMemeV1Addr = "0xEC4549caDcE5DA21Df6E6422d448034B5233bFbC"
	FourMemeV2Addr = "0x5c952063c7fc8610FFDB798152D69F0B9550762b"
)

// MakeEVMLogData builds a byte slice from big.Int values (each padded to 32 bytes).
func MakeEVMLogData(values ...*big.Int) []byte {
	data := make([]byte, 0, len(values)*32)
	for _, v := range values {
		b := v.Bytes()
		padded := make([]byte, 32)
		copy(padded[32-len(b):], b)
		data = append(data, padded...)
	}
	return data
}

// MakeAddressData builds a 32-byte ABI-encoded address.
func MakeAddressData(addr string) []byte {
	padded := make([]byte, 32)
	addrBytes := common.HexToAddress(addr).Bytes()
	copy(padded[32-len(addrBytes):], addrBytes)
	return padded
}

// MakeEVMLog constructs an Ethereum log suitable for testdata injection via RawData.
func MakeEVMLog(address string, topic0 string, topics []string, data []byte) map[string]interface{} {
	topicList := make([]interface{}, 0, 1+len(topics))
	topicList = append(topicList, topic0)
	for _, t := range topics {
		topicList = append(topicList, t)
	}
	return map[string]interface{}{
		"address": address,
		"topics":  topicList,
		"data":    "0x" + common.Bytes2Hex(data),
	}
}

// TxWithEVMLogs creates a UnifiedTransaction whose RawData contains EVM logs
// in the map[string]interface{}{"logs": [...]]} format that extractors expect.
func TxWithEVMLogs(chainType types.ChainType, txHash string, blockNum int64, logs []map[string]interface{}) types.UnifiedTransaction {
	logInterfaces := make([]interface{}, len(logs))
	for i, l := range logs {
		logInterfaces[i] = l
	}
	return types.UnifiedTransaction{
		TxHash:      txHash,
		ChainType:   chainType,
		ChainID:     chainIDFor(chainType),
		BlockNumber: big.NewInt(blockNum),
		BlockHash:   "0xblock" + big.NewInt(blockNum).String(),
		FromAddress: "0xuser",
		ToAddress:   "0xrouter",
		Value:       big.NewInt(0),
		Status:      types.TransactionStatusSuccess,
		Timestamp:   time.Now(),
		RawData: map[string]interface{}{
			"logs": logInterfaces,
		},
	}
}

// TxWithEthReceipt creates a UnifiedTransaction with an *ethtypes.Receipt in RawData.
func TxWithEthReceipt(chainType types.ChainType, txHash string, blockNum int64, logs []*ethtypes.Log) types.UnifiedTransaction {
	return types.UnifiedTransaction{
		TxHash:      txHash,
		ChainType:   chainType,
		ChainID:     chainIDFor(chainType),
		BlockNumber: big.NewInt(blockNum),
		BlockHash:   "0xblock",
		FromAddress: "0xuser",
		ToAddress:   "0xrouter",
		Value:       big.NewInt(0),
		Status:      types.TransactionStatusSuccess,
		Timestamp:   time.Now(),
		RawData: map[string]interface{}{
			"receipt": &ethtypes.Receipt{Logs: logs},
		},
	}
}

// BlockWithTxs creates a UnifiedBlock containing the given transactions.
func BlockWithTxs(chainType types.ChainType, blockNum int64, txs []types.UnifiedTransaction) types.UnifiedBlock {
	return types.UnifiedBlock{
		BlockNumber:  big.NewInt(blockNum),
		BlockHash:    "0xblock" + big.NewInt(blockNum).String(),
		ChainType:    chainType,
		ChainID:      chainIDFor(chainType),
		Timestamp:    time.Now(),
		TxCount:      len(txs),
		Transactions: txs,
		Events:       []types.UnifiedEvent{},
	}
}

// --- V2 Swap test data ---

// V2SwapLogData builds log data for a V2 Swap event:
// (uint256 amount0In, uint256 amount1In, uint256 amount0Out, uint256 amount1Out)
func V2SwapLogData(amount0In, amount1In, amount0Out, amount1Out *big.Int) []byte {
	return MakeEVMLogData(amount0In, amount1In, amount0Out, amount1Out)
}

// V3SwapLogData builds log data for a V3 Swap event:
// (int256 amount0, int256 amount1, uint160 sqrtPriceX96, uint128 liquidity, int24 tick)
func V3SwapLogData(amount0, amount1, sqrtPriceX96, liquidity *big.Int, tick int32) []byte {
	tickBig := big.NewInt(int64(tick))
	return MakeEVMLogData(amount0, amount1, sqrtPriceX96, liquidity, tickBig)
}

// PairCreatedLogData builds log data for PairCreated event:
// (address pair, uint256 pairId)
func PairCreatedLogData(pairAddr string, pairId *big.Int) []byte {
	data := make([]byte, 0, 64)
	data = append(data, MakeAddressData(pairAddr)...)
	b := pairId.Bytes()
	padded := make([]byte, 32)
	copy(padded[32-len(b):], b)
	data = append(data, padded...)
	return data
}

// --- FourMeme test data ---

// FourMemeV2PurchaseLogData builds log data for V2 TokenPurchase:
// (address token, address account, uint256 price, uint256 amount, uint256 cost, uint256 fee, uint256 offers, uint256 funds)
func FourMemeV2PurchaseLogData(tokenAddr, account string, price, amount, cost, fee, offers, funds *big.Int) []byte {
	data := make([]byte, 0, 256)
	data = append(data, MakeAddressData(tokenAddr)...)
	data = append(data, MakeAddressData(account)...)
	data = append(data, MakeEVMLogData(price, amount, cost, fee, offers, funds)...)
	return data
}

// FourMemeV1PurchaseLogData builds log data for V1 TokenPurchase:
// (address token, address account, uint256 tokenAmount, uint256 etherAmount)
func FourMemeV1PurchaseLogData(tokenAddr, account string, tokenAmount, etherAmount *big.Int) []byte {
	data := make([]byte, 0, 128)
	data = append(data, MakeAddressData(tokenAddr)...)
	data = append(data, MakeAddressData(account)...)
	data = append(data, MakeEVMLogData(tokenAmount, etherAmount)...)
	return data
}

// FourMemeV2TokenCreateLogData builds log data for V2 TokenCreate:
// (address creator, address token, ...) - minimum 256 bytes
func FourMemeV2TokenCreateLogData(creator, tokenAddr string) []byte {
	data := make([]byte, 0, 256)
	data = append(data, MakeAddressData(creator)...)
	data = append(data, MakeAddressData(tokenAddr)...)
	// Pad remaining bytes for requestId + dynamic string offsets + totalSupply + launchTime + launchFee
	data = append(data, make([]byte, 192)...)
	return data
}

// PoolCreatedV3LogData builds log data for V3 PoolCreated event:
// (int24 tickSpacing, address pool)
func PoolCreatedV3LogData(tickSpacing int32, poolAddr string) []byte {
	data := make([]byte, 0, 64)
	tickBig := big.NewInt(int64(tickSpacing))
	tickBytes := tickBig.Bytes()
	tickPadded := make([]byte, 32)
	copy(tickPadded[32-len(tickBytes):], tickBytes)
	data = append(data, tickPadded...)
	data = append(data, MakeAddressData(poolAddr)...)
	return data
}

// FourMemeV1TokenCreateLogData builds log data for V1 TokenCreate:
// (address creator, address token, uint256 requestId, string name, string symbol, uint256 totalSupply, uint256 launchTime)
// Minimum 224 bytes
func FourMemeV1TokenCreateLogData(creator, tokenAddr string) []byte {
	data := make([]byte, 0, 224)
	data = append(data, MakeAddressData(creator)...)
	data = append(data, MakeAddressData(tokenAddr)...)
	// Pad remaining bytes for requestId + dynamic string offsets + totalSupply + launchTime
	data = append(data, make([]byte, 160)...)
	return data
}

// V3MintLogData builds log data for V3 Mint event:
// (address sender, uint128 amount, uint256 amount0, uint256 amount1)
func V3MintLogData(sender string, amount, amount0, amount1 *big.Int) []byte {
	data := make([]byte, 0, 128)
	data = append(data, MakeAddressData(sender)...)
	amountBytes := amount.Bytes()
	amountPadded := make([]byte, 32)
	copy(amountPadded[32-len(amountBytes):], amountBytes)
	data = append(data, amountPadded...)
	data = append(data, MakeEVMLogData(amount0, amount1)...)
	return data
}

// V3BurnLogData builds log data for V3 Burn event:
// (uint128 amount, uint256 amount0, uint256 amount1)
func V3BurnLogData(amount, amount0, amount1 *big.Int) []byte {
	return MakeEVMLogData(amount, amount0, amount1)
}

// --- Sui event test data ---

// SuiSwapEvent creates a Sui event map for Bluefin/Cetus swap testing.
func SuiSwapEvent(eventType, poolId, sender string, amountIn, amountOut string, a2b bool) map[string]interface{} {
	return map[string]interface{}{
		"type":   eventType,
		"sender": sender,
		"id": map[string]interface{}{
			"eventSeq": "0",
		},
		"parsedJson": map[string]interface{}{
			"pool_id":    poolId,
			"amount_in":  amountIn,
			"amount_out": amountOut,
			"a2b":        a2b,
		},
	}
}

// SuiLiquidityEvent creates a Sui event map for liquidity add/remove testing.
func SuiLiquidityEvent(eventType, poolId, sender string, coinAAmount, coinBAmount string) map[string]interface{} {
	return map[string]interface{}{
		"type":   eventType,
		"sender": sender,
		"id": map[string]interface{}{
			"eventSeq": "1",
		},
		"parsedJson": map[string]interface{}{
			"pool_id":       poolId,
			"coin_a_amount": coinAAmount,
			"coin_b_amount": coinBAmount,
		},
	}
}

// TxWithSuiEvents creates a UnifiedTransaction with Sui events in RawData.
func TxWithSuiEvents(txHash string, blockNum int64, events []map[string]interface{}) types.UnifiedTransaction {
	eventInterfaces := make([]interface{}, len(events))
	for i, e := range events {
		eventInterfaces[i] = e
	}
	return types.UnifiedTransaction{
		TxHash:      txHash,
		ChainType:   types.ChainTypeSui,
		ChainID:     "sui",
		BlockNumber: big.NewInt(blockNum),
		BlockHash:   "0xsuiblock",
		FromAddress: "0xsuiuser",
		ToAddress:   "",
		Value:       big.NewInt(0),
		Status:      types.TransactionStatusSuccess,
		Timestamp:   time.Now(),
		RawData: map[string]interface{}{
			"events": eventInterfaces,
		},
	}
}

func chainIDFor(ct types.ChainType) string {
	switch ct {
	case types.ChainTypeBSC:
		return "56"
	case types.ChainTypeEthereum:
		return "1"
	case types.ChainTypeSolana:
		return "solana"
	case types.ChainTypeSui:
		return "sui"
	default:
		return string(ct)
	}
}
