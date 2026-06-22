package model

import "math/big"

type Reserve struct {
	Addr    string           `json:"addr"` // pool address
	Amounts map[int]*big.Int `json:"amounts"`
	Time    uint64           `json:"time"`            // block time
	Value   map[int]float64  `json:"value,omitempty"` // Value in USD or other fiat currency
}
