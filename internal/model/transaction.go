package model

import "math/big"

type Transaction struct {
	Addr        string            `json:"addr"`
	Protocol    string            `json:"protocol"` // e.g., uniswap_v2 / uniswap_v3
	Router      string            `json:"router"`
	Factory     string            `json:"factory"`
	Pool        string            `json:"pool"`
	Hash        string            `json:"hash"`
	From        string            `json:"from"`
	Side        string            `json:"side"`
	Amount      *big.Int          `json:"amount"`
	Price       float64           `json:"price"`
	Value       float64           `json:"value"`
	Time        uint64            `json:"time"`
	Extra       *TransactionExtra `json:"extra"`
	EventIndex  int64             `json:"index"` // Index of the event in the transaction
	TxIndex     int64             `json:"txIndex"`
	SwapIndex   int64             `json:"swapIndex"`
	BlockNumber int64             `json:"blockNumber"` // Index of the transaction in the block
}

type TransactionExtra struct {
	QuoteAddr     string `json:"quote_addr"`
	QuotePrice    string `json:"quote_price"`
	Type          string `json:"type"`           // e.g., "swap", "add_liquidity", "remove_liquidity"
	TokenSymbol   string `json:"token_symbol"`   // Symbol of the token being traded
	TokenDecimals int    `json:"token_decimals"` // Decimals of the token being traded
}
