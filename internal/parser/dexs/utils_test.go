package dex

import (
	"math/big"
	"testing"

	"unified-tx-parser/internal/types"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ExtractEVMLogsFromTransaction tests ---

func TestExtractEVMLogsFromTransaction_NilTx(t *testing.T) {
	logs := ExtractEVMLogsFromTransaction(nil)
	assert.Nil(t, logs)
}

func TestExtractEVMLogsFromTransaction_NilRawData(t *testing.T) {
	tx := &types.UnifiedTransaction{RawData: nil}
	logs := ExtractEVMLogsFromTransaction(tx)
	assert.Nil(t, logs)
}

func TestExtractEVMLogsFromTransaction_MapWithLogs(t *testing.T) {
	rawData := map[string]interface{}{
		"logs": []interface{}{
			map[string]interface{}{
				"address": "0x1111111111111111111111111111111111111111",
				"topics": []interface{}{
					"0xaaaa000000000000000000000000000000000000000000000000000000000000",
				},
				"data": "0x01020304",
			},
		},
	}
	tx := &types.UnifiedTransaction{RawData: rawData}
	logs := ExtractEVMLogsFromTransaction(tx)

	require.Len(t, logs, 1)
	assert.Equal(t, common.HexToAddress("0x1111111111111111111111111111111111111111"), logs[0].Address)
	assert.Len(t, logs[0].Topics, 1)
	assert.Equal(t, []byte{0x01, 0x02, 0x03, 0x04}, logs[0].Data)
}

func TestExtractEVMLogsFromTransaction_MapWithReceipt(t *testing.T) {
	ethLogs := []*ethtypes.Log{
		{Address: common.HexToAddress("0x2222")},
	}
	rawData := map[string]interface{}{
		"receipt": &ethtypes.Receipt{Logs: ethLogs},
	}
	tx := &types.UnifiedTransaction{RawData: rawData}
	logs := ExtractEVMLogsFromTransaction(tx)

	require.Len(t, logs, 1)
	assert.Equal(t, common.HexToAddress("0x2222"), logs[0].Address)
}

func TestExtractEVMLogsFromTransaction_DirectReceipt(t *testing.T) {
	ethLogs := []*ethtypes.Log{
		{Address: common.HexToAddress("0x3333")},
	}
	tx := &types.UnifiedTransaction{
		RawData: &ethtypes.Receipt{Logs: ethLogs},
	}
	logs := ExtractEVMLogsFromTransaction(tx)

	require.Len(t, logs, 1)
	assert.Equal(t, common.HexToAddress("0x3333"), logs[0].Address)
}

func TestExtractEVMLogsFromTransaction_DirectLogSlice(t *testing.T) {
	ethLogs := []*ethtypes.Log{
		{Address: common.HexToAddress("0x4444")},
		{Address: common.HexToAddress("0x5555")},
	}
	tx := &types.UnifiedTransaction{RawData: ethLogs}
	logs := ExtractEVMLogsFromTransaction(tx)

	require.Len(t, logs, 2)
}

func TestExtractEVMLogsFromTransaction_MapWithInvalidLogs(t *testing.T) {
	rawData := map[string]interface{}{
		"logs": "not a slice", // invalid type
	}
	tx := &types.UnifiedTransaction{RawData: rawData}
	logs := ExtractEVMLogsFromTransaction(tx)
	assert.Nil(t, logs)
}

func TestExtractEVMLogsFromTransaction_MapNoLogsNoReceipt(t *testing.T) {
	rawData := map[string]interface{}{
		"other": "data",
	}
	tx := &types.UnifiedTransaction{RawData: rawData}
	logs := ExtractEVMLogsFromTransaction(tx)
	assert.Nil(t, logs)
}

// --- ParseEVMLogsFromInterface tests ---

func TestParseEVMLogsFromInterface_NonSlice(t *testing.T) {
	assert.Nil(t, ParseEVMLogsFromInterface("not a slice"))
	assert.Nil(t, ParseEVMLogsFromInterface(nil))
	assert.Nil(t, ParseEVMLogsFromInterface(42))
}

func TestParseEVMLogsFromInterface_NonMapItems(t *testing.T) {
	items := []interface{}{"string", 42, nil}
	logs := ParseEVMLogsFromInterface(items)
	assert.Empty(t, logs)
}

func TestParseEVMLogsFromInterface_PartialFields(t *testing.T) {
	items := []interface{}{
		map[string]interface{}{
			"address": "0xabc",
			// no topics or data
		},
	}
	logs := ParseEVMLogsFromInterface(items)
	require.Len(t, logs, 1)
	assert.Equal(t, common.HexToAddress("0xabc"), logs[0].Address)
	assert.Empty(t, logs[0].Topics)
	assert.Empty(t, logs[0].Data)
}

// --- ToSignedInt256 tests ---

func TestToSignedInt256_Positive(t *testing.T) {
	// 256 = 0x0100
	b := make([]byte, 32)
	b[30] = 0x01
	b[31] = 0x00
	result := ToSignedInt256(b)
	assert.Equal(t, big.NewInt(256), result)
}

func TestToSignedInt256_Negative(t *testing.T) {
	// -1 in two's complement = 0xFFFF...FFFF (32 bytes of 0xFF)
	b := make([]byte, 32)
	for i := range b {
		b[i] = 0xFF
	}
	result := ToSignedInt256(b)
	assert.Equal(t, big.NewInt(-1), result)
}

func TestToSignedInt256_Zero(t *testing.T) {
	b := make([]byte, 32)
	result := ToSignedInt256(b)
	assert.Equal(t, 0, result.Sign())
}

func TestToSignedInt256_LargeNegative(t *testing.T) {
	// -100 in two's complement (256-bit)
	max := new(big.Int).Lsh(big.NewInt(1), 256)
	negHundred := new(big.Int).Sub(max, big.NewInt(100))
	b := negHundred.Bytes()
	// Pad to 32 bytes
	padded := make([]byte, 32)
	copy(padded[32-len(b):], b)
	result := ToSignedInt256(padded)
	assert.Equal(t, big.NewInt(-100), result)
}

func TestToSignedInt256_EmptyBytes(t *testing.T) {
	result := ToSignedInt256([]byte{})
	assert.Equal(t, big.NewInt(0), result)
}

// --- ToSignedInt64 tests ---

func TestToSignedInt64_Empty(t *testing.T) {
	assert.Equal(t, int64(0), ToSignedInt64([]byte{}))
}

func TestToSignedInt64_Positive(t *testing.T) {
	b := make([]byte, 32)
	b[31] = 42
	assert.Equal(t, int64(42), ToSignedInt64(b))
}

func TestToSignedInt64_Negative(t *testing.T) {
	b := make([]byte, 32)
	for i := range b {
		b[i] = 0xFF
	}
	assert.Equal(t, int64(-1), ToSignedInt64(b))
}

// --- CalcPrice tests ---

func TestCalcPrice_Normal(t *testing.T) {
	price := CalcPrice(big.NewInt(1000), big.NewInt(3000))
	assert.InDelta(t, 3.0, price, 0.001)
}

func TestCalcPrice_NilAmountIn(t *testing.T) {
	assert.Equal(t, 0.0, CalcPrice(nil, big.NewInt(100)))
}

func TestCalcPrice_NilAmountOut(t *testing.T) {
	assert.Equal(t, 0.0, CalcPrice(big.NewInt(100), nil))
}

func TestCalcPrice_ZeroAmountIn(t *testing.T) {
	assert.Equal(t, 0.0, CalcPrice(big.NewInt(0), big.NewInt(100)))
}

func TestCalcPrice_BothNil(t *testing.T) {
	assert.Equal(t, 0.0, CalcPrice(nil, nil))
}

func TestCalcPrice_LargeValues(t *testing.T) {
	amountIn := new(big.Int).Mul(big.NewInt(1e18), big.NewInt(1000))
	amountOut := new(big.Int).Mul(big.NewInt(1e18), big.NewInt(2000))
	price := CalcPrice(amountIn, amountOut)
	assert.InDelta(t, 2.0, price, 0.001)
}

// --- CalcV3Price tests ---

func TestCalcV3Price_Normal(t *testing.T) {
	// sqrtPriceX96 = 2^96 means price = 1.0
	sqrtPriceX96 := new(big.Int).Exp(big.NewInt(2), big.NewInt(96), nil)
	price := CalcV3Price(sqrtPriceX96)
	assert.InDelta(t, 1.0, price, 0.001)
}

func TestCalcV3Price_Nil(t *testing.T) {
	assert.Equal(t, 0.0, CalcV3Price(nil))
}

func TestCalcV3Price_Zero(t *testing.T) {
	assert.Equal(t, 0.0, CalcV3Price(big.NewInt(0)))
}

func TestCalcV3Price_Doubled(t *testing.T) {
	// sqrtPriceX96 = 2 * 2^96 means price = 4.0
	base := new(big.Int).Exp(big.NewInt(2), big.NewInt(96), nil)
	sqrtPriceX96 := new(big.Int).Mul(big.NewInt(2), base)
	price := CalcV3Price(sqrtPriceX96)
	assert.InDelta(t, 4.0, price, 0.001)
}

// --- CalcValue tests ---

func TestCalcValue_Normal(t *testing.T) {
	value := CalcValue(big.NewInt(1000), 2.5)
	assert.InDelta(t, 2500.0, value, 0.1)
}

func TestCalcValue_NilAmount(t *testing.T) {
	assert.Equal(t, 0.0, CalcValue(nil, 1.0))
}

func TestCalcValue_ZeroAmount(t *testing.T) {
	assert.Equal(t, 0.0, CalcValue(big.NewInt(0), 1.0))
}

func TestCalcValue_ZeroPrice(t *testing.T) {
	assert.Equal(t, 0.0, CalcValue(big.NewInt(100), 0))
}

// --- ConvertDecimals tests ---

func TestConvertDecimals_USDC(t *testing.T) {
	// 1,000,000 raw USDC (6 decimals) = 1.0 USDC
	amount := big.NewInt(1000000)
	result := ConvertDecimals(amount, 6)
	assert.InDelta(t, 1.0, result, 0.001)
}

func TestConvertDecimals_ETH(t *testing.T) {
	// 1e18 wei = 1.0 ETH
	amount := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	result := ConvertDecimals(amount, 18)
	assert.InDelta(t, 1.0, result, 0.001)
}

func TestConvertDecimals_Nil(t *testing.T) {
	assert.Equal(t, 0.0, ConvertDecimals(nil, 18))
}

func TestConvertDecimals_Zero(t *testing.T) {
	assert.Equal(t, 0.0, ConvertDecimals(big.NewInt(0), 18))
}

func TestConvertDecimals_ZeroDecimals(t *testing.T) {
	result := ConvertDecimals(big.NewInt(42), 0)
	assert.InDelta(t, 42.0, result, 0.001)
}

func TestConvertDecimals_HighDecimals(t *testing.T) {
	// decimals > 77 should be capped at 77
	result := ConvertDecimals(big.NewInt(1), 255)
	// Should not panic
	assert.True(t, result >= 0)
}

// --- ParseHexBytes tests ---

func TestParseHexBytes(t *testing.T) {
	result := ParseHexBytes("0x0102ff")
	assert.Equal(t, []byte{0x01, 0x02, 0xff}, result)
}

func TestParseHexBytes_Empty(t *testing.T) {
	result := ParseHexBytes("")
	assert.Empty(t, result)
}

// --- ParseHexAddress tests ---

func TestParseHexAddress(t *testing.T) {
	addr := ParseHexAddress("0x1234567890abcdef1234567890abcdef12345678")
	assert.Equal(t, common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678"), addr)
}

// --- ParseHexHash tests ---

func TestParseHexHash(t *testing.T) {
	hash := ParseHexHash("0xaaaa000000000000000000000000000000000000000000000000000000000000")
	assert.Equal(t, common.HexToHash("0xaaaa000000000000000000000000000000000000000000000000000000000000"), hash)
}

// --- BytesToBigInt tests ---

func TestBytesToBigInt(t *testing.T) {
	b := []byte{0x01, 0x00} // 256
	result := BytesToBigInt(b)
	assert.Equal(t, big.NewInt(256), result)
}

func TestBytesToBigInt_Empty(t *testing.T) {
	result := BytesToBigInt([]byte{})
	assert.Equal(t, big.NewInt(0), result)
}

// --- BigIntToBytes tests ---

func TestBigIntToBytes_Small(t *testing.T) {
	b := BigIntToBytes(big.NewInt(1))
	assert.Len(t, b, 32, "should be left-padded to 32 bytes")
	assert.Equal(t, byte(1), b[31])
	assert.Equal(t, byte(0), b[0])
}

func TestBigIntToBytes_Large(t *testing.T) {
	// 32 bytes exactly
	val := new(big.Int).Lsh(big.NewInt(1), 248) // 2^248, fits in 32 bytes
	b := BigIntToBytes(val)
	assert.Len(t, b, 32)
}

// --- ValidateAddress tests ---

func TestValidateAddress_Valid(t *testing.T) {
	assert.True(t, ValidateAddress("0x1234567890abcdef1234567890abcdef12345678"))
	assert.True(t, ValidateAddress("0xABCDEF1234567890ABCDEF1234567890ABCDEF12"))
}

func TestValidateAddress_Invalid(t *testing.T) {
	tests := []struct {
		name string
		addr string
	}{
		{"too short", "0x1234"},
		{"too long", "0x1234567890abcdef1234567890abcdef1234567890"},
		{"no prefix", "1234567890abcdef1234567890abcdef12345678"},
		{"wrong prefix", "1x1234567890abcdef1234567890abcdef12345678"},
		{"non-hex chars", "0x1234567890abcdef1234567890abcdef1234567g"},
		{"empty", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.False(t, ValidateAddress(tt.addr))
		})
	}
}

// --- StringifyBigInt tests ---

func TestStringifyBigInt(t *testing.T) {
	assert.Equal(t, "0", StringifyBigInt(nil))
	assert.Equal(t, "0", StringifyBigInt(big.NewInt(0)))
	assert.Equal(t, "12345", StringifyBigInt(big.NewInt(12345)))
	assert.Equal(t, "-100", StringifyBigInt(big.NewInt(-100)))
}

// --- ExtractEventIndex tests ---

func TestExtractEventIndex(t *testing.T) {
	assert.Equal(t, int64(0), ExtractEventIndex(nil))

	log := &ethtypes.Log{Index: 42}
	assert.Equal(t, int64(42), ExtractEventIndex(log))
}

// --- ExtractAddressFromLog tests ---

func TestExtractAddressFromLog(t *testing.T) {
	assert.Equal(t, common.Address{}, ExtractAddressFromLog(nil))

	addr := common.HexToAddress("0xabcdef")
	log := &ethtypes.Log{Address: addr}
	assert.Equal(t, addr, ExtractAddressFromLog(log))
}

// --- SafeStringConversion tests ---

func TestSafeStringConversion(t *testing.T) {
	assert.Equal(t, "", SafeStringConversion(nil))
	assert.Equal(t, "hello", SafeStringConversion("hello"))
	assert.Equal(t, "42", SafeStringConversion(42))
	assert.Equal(t, "true", SafeStringConversion(true))
}

// --- SafeUint256Conversion tests ---

func TestSafeUint256Conversion(t *testing.T) {
	// nil
	assert.Equal(t, big.NewInt(0), SafeUint256Conversion(nil))

	// *big.Int
	bi := big.NewInt(100)
	assert.Equal(t, bi, SafeUint256Conversion(bi))

	// big.Int (value, not pointer)
	biVal := *big.NewInt(200)
	result := SafeUint256Conversion(biVal)
	assert.Equal(t, big.NewInt(200), result)

	// string
	result = SafeUint256Conversion("12345")
	assert.Equal(t, big.NewInt(12345), result)

	// int
	result = SafeUint256Conversion(42)
	assert.Equal(t, big.NewInt(42), result)

	// int64
	result = SafeUint256Conversion(int64(999))
	assert.Equal(t, big.NewInt(999), result)

	// uint
	result = SafeUint256Conversion(uint(50))
	assert.Equal(t, big.NewInt(50), result)

	// uint64
	result = SafeUint256Conversion(uint64(12345))
	assert.Equal(t, big.NewInt(12345), result)

	// float64
	result = SafeUint256Conversion(float64(3.14))
	assert.Equal(t, big.NewInt(3), result) // truncated

	// unsupported type
	result = SafeUint256Conversion([]byte{1, 2, 3})
	assert.Equal(t, big.NewInt(0), result)
}

// --- GetBlockNumber tests ---

func TestGetBlockNumber(t *testing.T) {
	assert.Equal(t, int64(0), GetBlockNumber(nil))

	tx := &types.UnifiedTransaction{BlockNumber: nil}
	assert.Equal(t, int64(0), GetBlockNumber(tx))

	tx2 := &types.UnifiedTransaction{BlockNumber: big.NewInt(12345)}
	assert.Equal(t, int64(12345), GetBlockNumber(tx2))
}
