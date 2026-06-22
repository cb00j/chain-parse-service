package model

import "math/big"

type Liquidity struct {
	Addr    string          `json:"addr"`
	Router  string          `json:"router"`
	Factory string          `json:"factory"`
	Pool    string          `json:"pool"`
	Hash    string          `json:"hash"`
	From    string          `json:"from"`
	Pos     string          `json:"pos"`
	Side    string          `json:"side"`
	Amount  *big.Int        `json:"amount"`
	Value   float64         `json:"value"`
	Time    uint64          `json:"time"`
	Key     string          `json:"key"` // Unique key for the liquidity event
	Extra   *LiquidityExtra `json:"extra,omitempty"`
}

type LiquidityExtra struct {
	Key     string    `json:"key"`     // Unique key for the liquidity event
	Amounts *big.Int  `json:"amounts"` // Amounts of tokens involved in the liquidity event
	Values  []float64 `json:"values"`  // Values of tokens in USD or other fiat currency
	Time    uint64    `json:"time"`    // Timestamp of the liquidity event
}
