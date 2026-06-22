package utils

import (
	"math/big"
	"strconv"
	"strings"
)

// GetStringField 安全获取字符串字段
func GetStringField(fields map[string]interface{}, key string) string {
	if value, ok := fields[key]; ok {
		if str, ok := value.(string); ok {
			return str
		}
	}
	return ""
}

// GetBigIntField 安全获取大整数字段
func GetBigIntField(fields map[string]interface{}, key string) *big.Int {
	if value, ok := fields[key]; ok {
		switch v := value.(type) {
		case string:
			if result, ok := new(big.Int).SetString(v, 10); ok {
				return result
			}
		case int64:
			return big.NewInt(v)
		case float64:
			return big.NewInt(int64(v))
		case int:
			return big.NewInt(int64(v))
		}
	}
	return nil
}

// GetIntField 安全获取整数字段
func GetIntField(fields map[string]interface{}, key string) int64 {
	if value, ok := fields[key]; ok {
		switch v := value.(type) {
		case int64:
			return v
		case int:
			return int64(v)
		case float64:
			return int64(v)
		case string:
			if result, err := strconv.ParseInt(v, 10, 64); err == nil {
				return result
			}
		}
	}
	return 0
}

// GetBoolField 安全获取布尔字段
func GetBoolField(fields map[string]interface{}, key string) bool {
	if value, ok := fields[key]; ok {
		switch v := value.(type) {
		case bool:
			return v
		case string:
			return v == "true" || v == "1"
		case float64:
			return v != 0
		}
	}
	return false
}

// GetFloatField 安全获取浮点数字段
func GetFloatField(fields map[string]interface{}, key string) float64 {
	if value, ok := fields[key]; ok {
		switch v := value.(type) {
		case float64:
			return v
		case int64:
			return float64(v)
		case int:
			return float64(v)
		case string:
			if result, err := strconv.ParseFloat(v, 64); err == nil {
				return result
			}
		}
	}
	return 0.0
}

// ParseEventsFromInterface 解析事件接口 - 通用方法
func ParseEventsFromInterface(events interface{}) []map[string]interface{} {
	switch eventsData := events.(type) {
	case []interface{}:
		result := make([]map[string]interface{}, 0, len(eventsData))
		for _, event := range eventsData {
			if eventMap, ok := event.(map[string]interface{}); ok {
				result = append(result, eventMap)
			}
		}
		return result
	case []map[string]interface{}:
		return eventsData
	default:
		return nil
	}
}

// ExtractPoolTokens 从池类型字符串中提取代币地址 - 通用方法
func ExtractPoolTokens(poolType string) (string, string) {
	if poolType == "" {
		return "", ""
	}

	start := strings.Index(poolType, "<")
	end := strings.Index(poolType, ">")

	if start == -1 || end == -1 || start >= end {
		return "", ""
	}

	substr := poolType[start+1 : end]
	substr = strings.ReplaceAll(substr, " ", "")
	tokens := strings.Split(substr, ",")

	if len(tokens) >= 2 {
		return tokens[0], tokens[1]
	}

	return "", ""
}

// CalculatePrice 计算价格 - 通用方法
func CalculatePrice(amountIn, amountOut *big.Int) float64 {
	if amountIn == nil || amountOut == nil || amountIn.Cmp(big.NewInt(0)) == 0 {
		return 0.0
	}

	amountInFloat := new(big.Float).SetInt(amountIn)
	amountOutFloat := new(big.Float).SetInt(amountOut)

	price := new(big.Float).Quo(amountOutFloat, amountInFloat)
	result, _ := price.Float64()
	return result
}

// FormatAddress 格式化地址 - 通用方法
func FormatAddress(addr string) string {
	if addr == "" {
		return ""
	}

	// 移除0x前缀并转为小写
	addr = strings.ToLower(addr)
	if strings.HasPrefix(addr, "0x") {
		return addr
	}
	return "0x" + addr
}

// ContainsString 检查字符串数组是否包含指定字符串
func ContainsString(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// RemoveDuplicateStrings 移除字符串数组中的重复项
func RemoveDuplicateStrings(slice []string) []string {
	keys := make(map[string]bool)
	result := []string{}

	for _, item := range slice {
		if !keys[item] {
			keys[item] = true
			result = append(result, item)
		}
	}

	return result
}

// SafeStringToInt64 安全地将字符串转换为int64
func SafeStringToInt64(s string) int64 {
	if result, err := strconv.ParseInt(s, 10, 64); err == nil {
		return result
	}
	return 0
}

// SafeStringToBigInt 安全地将字符串转换为big.Int
func SafeStringToBigInt(s string) *big.Int {
	if result, ok := new(big.Int).SetString(s, 10); ok {
		return result
	}
	return big.NewInt(0)
}
