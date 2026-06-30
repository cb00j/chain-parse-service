package model

import "math/big"

// SwapRecordRole identifies whether a SwapRecord represents the token0 or
// token1 side of a swap event. Kept as a distinct type (not a raw string)
// so a storage implementation's role<->ordering-key mapping
// can't drift apart from how this field is actually set.
type SwapRecordRole string

const (
	RoleToken0 SwapRecordRole = "token0"
	RoleToken1 SwapRecordRole = "token1"
)

// SwapRecordSide identifies whether, from this row's token_address
// perspective, the token flowed into the pool (the user paid it) or out of
// the pool (the user received it). Derived directly from the signed
// amount0/amount1 in the swap log
type SwapRecordSide string

const (
	SideIn  SwapRecordSide = "in"
	SideOut SwapRecordSide = "out"
)

// SwapRecord is one row of the `swaps` wide table — double-entry bookkeeping
// applied to DEX swaps. Every swap event produces exactly two SwapRecords,
// one per token.
//
// This struct is intentionally storage-agnostic: it holds only the data a
// swap event actually carries. Anything that exists purely to satisfy a
// particular storage engine's quirks (e.g. InfluxDB's lack of a real UPDATE,
// or its tags-determine-the-series model) belongs in that engine's package
// under internal/storage, not here — see internal/storage/influxdb for the
// synthetic-timestamp scheme that consumes BlockTime/LogIndex/Role below.
// A future ClickHouse/PostgreSQL implementation can consume this same
// struct with a real primary key (tx_hash, log_index, role) and skip that
// scheme entirely.
//
// Design intent:
//   - RawAmount is always the unnormalized, on-chain integer value. Nothing
//     in the write path divides it by 10^decimals — that calculation is
//     deferred to read time (or to TokenDecimals being backfilled and the
//     row being rewritten by the async worker), specifically to avoid the
//     "raw value computed against a wrong/default decimals" bug class that
//     motivated this whole redesign.
//   - TokenDecimals is *int, nil meaning "not yet resolved". The write
//     path never blocks on resolving it — a nil here just means this row
//     is a candidate for the async backfill worker to pick up later.
//   - Tags vs fields in the comments below describe the InfluxDB mapping
//     specifically; a relational store would map TokenAddr/PoolAddr/etc to
//     indexed columns instead, with no tag/field distinction needed.
type SwapRecord struct {
	TokenAddr string         `json:"token_address"`
	PoolAddr  string         `json:"pool_address"`
	Protocol  string         `json:"protocol"` // e.g. uniswap_v2 / uniswap_v3
	Role      SwapRecordRole `json:"role"`
	Side      SwapRecordSide `json:"side"`

	TxHash          string   `json:"tx_hash"`
	RawAmount       *big.Int `json:"raw_amount"`
	TokenDecimals   *int     `json:"token_decimals,omitempty"` // nil = unresolved
	PairedTokenAddr string   `json:"paired_token_address"`
	BlockNumber     int64    `json:"block_number"`

	BlockTime uint64 `json:"block_time"` // unix seconds
	LogIndex  int64  `json:"log_index"`
}
