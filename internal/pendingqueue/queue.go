// Package pendingqueue holds swap events whose pool hasn't been resolved
// yet (token0/token1 unknown), so they can be reprocessed once the pool's
// tokens become known — without blocking the hot path (block/log
// processing) on an eth_call.
//
// This exists specifically for model.SwapRecord (the `swaps` double-entry
// table): token_address is an InfluxDB tag, and tags can't be written as
// placeholders and backfilled the way a field can (a point's identity is
// its measurement+tags+timestamp — there's no partial write, no ALTER).
// model.Transaction has no such requirement (see insertTransaction's tags:
// addr/hash/from/side/pool/router/factory/protocol — no token address at
// all) and is written unconditionally regardless of whether this queue is
// involved.
//
// Queue is intentionally minimal and storage-agnostic: an in-memory
// implementation (Memory) is used today; a Kafka-backed implementation can
// satisfy the same interface later without callers changing, which is the
// whole point of having this interface instead of wiring cmd/parser
// directly to channels.
package pendingqueue

import (
	"context"
	"math/big"
	"time"

	"unified-tx-parser/internal/model"
)

// Message is one swap event whose pool wasn't resolved at the time it was
// scanned. It carries everything needed to build the two model.SwapRecord
// rows once token0/token1 become known — Amount0/Side0/Amount1/Side1 are
// already derived from the raw log by the caller (parseV2Swap/
// parseV3Swap), so the consumer doesn't need to re-parse the original log,
// only supply the two token addresses.
type Message struct {
	PoolAddr    string
	Protocol    string // e.g. "uniswap_v2" / "uniswap_v3"
	TxHash      string
	BlockNumber int64
	BlockTime   uint64
	LogIndex    int64

	Amount0 *big.Int
	Side0   model.SwapRecordSide
	Amount1 *big.Int
	Side1   model.SwapRecordSide

	EnqueuedAt time.Time
}

// Queue is the interface producers (parseV2Swap/parseV3Swap) and
// consumers (the pool-resolution worker) share. Implementations must be
// safe for concurrent use — Enqueue is called from the hot path, Consume
// runs in its own goroutine.
type Queue interface {
	// Enqueue adds msg to the queue, grouped by msg.PoolAddr. Must never
	// block for long — the hot path is calling this. Returns an error
	// (e.g. ErrQueueFull) if the message can't be accepted; the caller's
	// fallback is simply not writing a SwapRecord for this event (see
	// buildSwapRecords' doc comment — Transaction is unaffected either
	// way).
	Enqueue(ctx context.Context, msg Message) error

	// Consume runs handler once per pool address, batched with every
	// currently-queued message for that pool — not one at a time — so a
	// burst of swaps on the same brand-new pool resolves and flushes
	// together instead of the resolution work (and any RPC call it
	// costs) being repeated per message. Blocks until ctx is cancelled.
	//
	// If handler returns an error, the messages are requeued for another
	// attempt (up to an implementation-defined retry/expiry limit) rather
	// than dropped on the first failure.
	Consume(ctx context.Context, handler func(ctx context.Context, poolAddr string, msgs []Message) error) error
}
