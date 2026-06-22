package dex

import (
	"fmt"
	"math"
	"math/big"

	"unified-tx-parser/internal/types"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

// ExtractEVMLogsFromTransaction extracts Ethereum logs from a unified transaction
// This function is centralized to avoid duplication across extractors
func ExtractEVMLogsFromTransaction(tx *types.UnifiedTransaction) []*ethtypes.Log {
	if tx == nil || tx.RawData == nil {
		return nil
	}

	switch rawData := tx.RawData.(type) {
	case map[string]interface{}:
		// First try to get logs directly
		if logs, ok := rawData["logs"]; ok {
			if logsList := ParseEVMLogsFromInterface(logs); logsList != nil {
				return logsList
			}
		}
		// Fall back to receipt
		if receipt, ok := rawData["receipt"]; ok {
			if ethReceipt, ok := receipt.(*ethtypes.Receipt); ok && ethReceipt != nil {
				return ethReceipt.Logs
			}
		}
	case *ethtypes.Receipt:
		if rawData != nil && rawData.Logs != nil {
			return rawData.Logs
		}
	case []*ethtypes.Log:
		return rawData
	}

	return nil
}

// ParseEVMLogsFromInterface parses logs from an interface{} (typically from JSON deserialization)
func ParseEVMLogsFromInterface(logs interface{}) []*ethtypes.Log {
	logsSlice, ok := logs.([]interface{})
	if !ok {
		return nil
	}

	result := make([]*ethtypes.Log, 0, len(logsSlice))
	for idx, item := range logsSlice {
		logMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		ethLog := &ethtypes.Log{
			Index: uint(idx), // Default to sequential index
		}

		// Parse log index (override default sequential index if provided)
		if logIndex, ok := logMap["logIndex"].(float64); ok {
			ethLog.Index = uint(logIndex)
		} else if logIndex, ok := logMap["log_index"].(float64); ok {
			ethLog.Index = uint(logIndex)
		}

		// Parse address
		if addr, ok := logMap["address"].(string); ok {
			ethLog.Address = common.HexToAddress(addr)
		}

		// Parse topics
		if topics, ok := logMap["topics"].([]interface{}); ok {
			ethLog.Topics = make([]common.Hash, len(topics))
			for i, t := range topics {
				if s, ok := t.(string); ok {
					ethLog.Topics[i] = common.HexToHash(s)
				}
			}
		}

		// Parse data
		if data, ok := logMap["data"].(string); ok {
			ethLog.Data = common.FromHex(data)
		}

		result = append(result, ethLog)
	}

	return result
}

// ToSignedInt256 converts a 256-bit big-endian byte slice to a signed big.Int
// This is used for parsing signed integers from EVM event data
func ToSignedInt256(b []byte) *big.Int {
	v := new(big.Int).SetBytes(b)
	// If the high bit is set, it's negative (two's complement)
	if len(b) > 0 && b[0]&0x80 != 0 {
		max := new(big.Int).Lsh(big.NewInt(1), 256)
		v.Sub(v, max)
	}
	return v
}

// ToSignedInt64 converts a 64-bit big-endian byte slice to a signed int64
func ToSignedInt64(b []byte) int64 {
	if len(b) == 0 {
		return 0
	}
	v := ToSignedInt256(b)
	return v.Int64()
}

// CalcPrice calculates the price ratio between amountIn and amountOut
// Returns price as float64 for downstream use
func CalcPrice(amountIn, amountOut *big.Int) float64 {
	if amountIn == nil || amountOut == nil || amountIn.Sign() == 0 {
		return 0
	}
	in := new(big.Float).SetInt(amountIn)
	out := new(big.Float).SetInt(amountOut)
	price, _ := new(big.Float).Quo(out, in).Float64()
	if math.IsNaN(price) || math.IsInf(price, 0) {
		return 0
	}
	return price
}

// CalcV3Price calculates price from sqrtPriceX96: price = (sqrtPriceX96 / 2^96)^2
// Used for Uniswap V3 and PancakeSwap V3 price calculations
func CalcV3Price(sqrtPriceX96 *big.Int) float64 {
	if sqrtPriceX96 == nil || sqrtPriceX96.Sign() == 0 {
		return 0
	}
	sq := new(big.Float).SetInt(sqrtPriceX96)
	q96 := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(2), big.NewInt(96), nil))
	ratio := new(big.Float).Quo(sq, q96)
	price := new(big.Float).Mul(ratio, ratio)
	f, _ := price.Float64()
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return f
}

// CalcValue calculates the total value of tokens: amount * price
func CalcValue(amount *big.Int, price float64) float64 {
	if amount == nil || amount.Sign() == 0 || price == 0 {
		return 0
	}
	amountFloat, _ := new(big.Float).SetInt(amount).Float64()
	value := amountFloat * price
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return value
}

// ConvertDecimals adjusts a token amount by its decimals
// e.g., 1000000 USDC (6 decimals) = 1.0 USDC = CalcDecimals(1000000, 6) = 1.0
func ConvertDecimals(amount *big.Int, decimals uint8) float64 {
	if amount == nil || amount.Sign() == 0 {
		return 0
	}
	if decimals > 77 {
		// Prevent exponent overflow
		decimals = 77
	}
	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	result := new(big.Float).Quo(
		new(big.Float).SetInt(amount),
		new(big.Float).SetInt(divisor),
	)
	value, _ := result.Float64()
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return value
}

// ParseHexBytes parses a hex string to []byte
func ParseHexBytes(hexStr string) []byte {
	return common.FromHex(hexStr)
}

// ParseHexAddress parses a hex string to ethereum address
func ParseHexAddress(hexStr string) common.Address {
	return common.HexToAddress(hexStr)
}

// ParseHexHash parses a hex string to ethereum hash
func ParseHexHash(hexStr string) common.Hash {
	return common.HexToHash(hexStr)
}

// BytesToBigInt converts bytes to big.Int (big-endian)
func BytesToBigInt(b []byte) *big.Int {
	return new(big.Int).SetBytes(b)
}

// BigIntToBytes converts big.Int to bytes (big-endian, left-padded to 32 bytes)
func BigIntToBytes(i *big.Int) []byte {
	b := i.Bytes()
	// Left-pad with zeros to 32 bytes (standard EVM encoding)
	if len(b) < 32 {
		padded := make([]byte, 32)
		copy(padded[32-len(b):], b)
		return padded
	}
	return b
}

// ValidateAddress checks if a string is a valid hex address
func ValidateAddress(addr string) bool {
	if len(addr) != 42 { // 0x + 40 hex characters
		return false
	}
	if addr[:2] != "0x" {
		return false
	}
	for _, c := range addr[2:] {
		if !isHexChar(c) {
			return false
		}
	}
	return true
}

// isHexChar checks if a rune is a valid hex character
func isHexChar(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

// StringifyBigInt converts big.Int to string with proper formatting
func StringifyBigInt(i *big.Int) string {
	if i == nil {
		return "0"
	}
	return i.String()
}

// ExtractEventIndex extracts the event index from log data
// In EVM logs, this is typically the log index within the transaction
func ExtractEventIndex(log *ethtypes.Log) int64 {
	if log == nil {
		return 0
	}
	return int64(log.Index)
}

// ExtractAddressFromLog gets the emitter address from a log
func ExtractAddressFromLog(log *ethtypes.Log) common.Address {
	if log == nil {
		return common.Address{}
	}
	return log.Address
}

// SafeStringConversion safely converts interface{} to string
func SafeStringConversion(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// SafeUint256Conversion safely converts interface{} to *big.Int
func SafeUint256Conversion(v interface{}) *big.Int {
	if v == nil {
		return big.NewInt(0)
	}
	switch val := v.(type) {
	case *big.Int:
		return val
	case big.Int:
		return &val
	case string:
		bi := new(big.Int)
		bi.SetString(val, 10)
		return bi
	case int:
		return big.NewInt(int64(val))
	case int64:
		return big.NewInt(val)
	case uint:
		return new(big.Int).SetUint64(uint64(val))
	case uint64:
		return new(big.Int).SetUint64(val)
	case float64:
		return big.NewInt(int64(val))
	}
	return big.NewInt(0)
}

// GetBlockNumber safely gets block number from a transaction as int64
func GetBlockNumber(tx *types.UnifiedTransaction) int64 {
	if tx == nil || tx.BlockNumber == nil {
		return 0
	}
	return tx.BlockNumber.Int64()
}
