package utils

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"
)

// ToJSON 将对象转换为JSON字符串
func ToJSON(obj interface{}) (string, error) {
	data, err := json.Marshal(obj)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// BigIntFromString 从字符串转换为big.Int
func BigIntFromString(s string) (*big.Int, error) {
	if s == "" {
		return big.NewInt(0), nil
	}

	// 去除可能的 0x 前缀
	s = strings.TrimPrefix(s, "0x")

	// 尝试解析十六进制
	if val, ok := new(big.Int).SetString(s, 16); ok {
		return val, nil
	}

	// 尝试解析十进制
	if val, ok := new(big.Int).SetString(s, 10); ok {
		return val, nil
	}

	return nil, fmt.Errorf("无法解析数字: %s", s)
}

// StringFromBigInt 从big.Int转换为字符串
func StringFromBigInt(val *big.Int) string {
	if val == nil {
		return "0"
	}
	return val.String()
}

// HexStringFromBigInt 从big.Int转换为十六进制字符串
func HexStringFromBigInt(val *big.Int) string {
	if val == nil {
		return "0x0"
	}
	return "0x" + val.Text(16)
}

// Int64FromString 从字符串转换为int64
func Int64FromString(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.ParseInt(s, 10, 64)
}

// TimeFromTimestampMs 从毫秒时间戳转换为时间
func TimeFromTimestampMs(timestampMs string) (time.Time, error) {
	if timestampMs == "" {
		return time.Time{}, nil
	}

	ms, err := strconv.ParseInt(timestampMs, 10, 64)
	if err != nil {
		return time.Time{}, err
	}

	return time.Unix(0, ms*int64(time.Millisecond)), nil
}

// TimeFromTimestamp 从秒时间戳转换为时间
func TimeFromTimestamp(timestamp string) (time.Time, error) {
	if timestamp == "" {
		return time.Time{}, nil
	}

	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return time.Time{}, err
	}

	return time.Unix(ts, 0), nil
}

// SafeStringValue 安全获取字符串指针的值
func SafeStringValue(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}

// NormalizeAddress 规范化地址格式
func NormalizeAddress(address string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return ""
	}

	// 以太坊地址统一为小写
	if strings.HasPrefix(address, "0x") && len(address) == 42 {
		return strings.ToLower(address)
	}

	return address
}

// IsValidAddress 检查地址是否有效
func IsValidAddress(address string, chainType string) bool {
	if address == "" {
		return false
	}

	switch strings.ToLower(chainType) {
	case "ethereum", "bsc":
		// 以太坊格式地址
		return strings.HasPrefix(address, "0x") && len(address) == 42
	case "sui":
		// Sui地址长度通常是64个字符（不包括0x前缀）
		return len(strings.TrimPrefix(address, "0x")) == 64
	case "solana":
		// Solana地址是base58编码，长度通常是32-44个字符
		return len(address) >= 32 && len(address) <= 44
	default:
		return true // 未知链类型，暂时认为有效
	}
}

// GenerateEventID 生成事件ID
func GenerateEventID(chainType, txHash string, eventIndex int) string {
	return fmt.Sprintf("%s_%s_%d", chainType, txHash, eventIndex)
}

// TruncateString 截断字符串
func TruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
