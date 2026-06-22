package utils

import (
	"database/sql"
	"encoding/json"
	"math/big"
)

// FromJSON 从JSON字符串反序列化对象
func FromJSON(jsonStr string, v interface{}) error {
	return json.Unmarshal([]byte(jsonStr), v)
}

// BigIntToNullString 将big.Int转换为sql.NullString
func BigIntToNullString(val *big.Int) sql.NullString {
	if val == nil {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: val.String(), Valid: true}
}

// BigIntToNullInt64 将big.Int转换为sql.NullInt64
func BigIntToNullInt64(val *big.Int) sql.NullInt64 {
	if val == nil {
		return sql.NullInt64{Valid: false}
	}
	if !val.IsInt64() {
		// 如果值太大，返回无效
		return sql.NullInt64{Valid: false}
	}
	return sql.NullInt64{Int64: val.Int64(), Valid: true}
}

// StringToNullString 将字符串转换为sql.NullString
func StringToNullString(val string) sql.NullString {
	if val == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: val, Valid: true}
}

// IntToNullInt 将int转换为sql.NullInt32
func IntToNullInt(val int) sql.NullInt32 {
	return sql.NullInt32{Int32: int32(val), Valid: true}
}

// NullStringToBigInt 扫描NullString到*big.Int的辅助函数
func NullStringToBigInt(target **big.Int) *sql.NullString {
	return &sql.NullString{}
}

// NullInt64ToBigInt 扫描NullInt64到*big.Int的辅助函数
func NullInt64ToBigInt(target **big.Int) *sql.NullInt64 {
	return &sql.NullInt64{}
}

// NullStringToString 扫描NullString到string的辅助函数
func NullStringToString(target *string) *sql.NullString {
	return &sql.NullString{}
}

// NullIntToInt 扫描NullInt32到int的辅助函数
func NullIntToInt(target *int) *sql.NullInt32 {
	return &sql.NullInt32{}
}

// ProcessNullString 处理NullString扫描结果
func ProcessNullString(ns sql.NullString, target *string) {
	if ns.Valid {
		*target = ns.String
	}
}

// ProcessNullStringToBigInt 处理NullString到BigInt的转换
func ProcessNullStringToBigInt(ns sql.NullString, target **big.Int) {
	if ns.Valid && ns.String != "" {
		if val, ok := new(big.Int).SetString(ns.String, 10); ok {
			*target = val
		}
	}
}

// ProcessNullInt64ToBigInt 处理NullInt64到BigInt的转换
func ProcessNullInt64ToBigInt(ni sql.NullInt64, target **big.Int) {
	if ni.Valid {
		*target = big.NewInt(ni.Int64)
	}
}

// ProcessNullInt32ToInt 处理NullInt32到int的转换
func ProcessNullInt32ToInt(ni sql.NullInt32, target *int) {
	if ni.Valid {
		*target = int(ni.Int32)
	}
}
