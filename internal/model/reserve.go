package model

import "math/big"

type Reserve struct {
	Addr     string           `json:"addr"`     // pool address
	Protocol string           `json:"protocol"` // e.g., uniswap_v2 / uniswap_v3
	Amounts  map[int]*big.Int `json:"amounts"`
	Time     uint64           `json:"time"`            // block time
	Value    map[int]float64  `json:"value,omitempty"` // Value in USD or other fiat currency
}
