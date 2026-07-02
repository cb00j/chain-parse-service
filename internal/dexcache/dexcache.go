// Package dexcache provides a shared Redis-backed cache for DEX pool/token
// metadata, sitting between storage (source of truth) and anything that
// needs fast lookups (on-chain extractors resolving token decimals, lazy
// pool creation, etc.).
//
// The token cache format here is deliberately identical to the one
// internal/parser/dexs/eth.UniswapExtractor already writes/reads
// (token_meta:<addr> hash with decimals/symbol/name, 30-day TTL) — this is
// the same cache, not a parallel one. That compatibility is the actual
// point: when internal/thegraph.Syncer prefetches a token from a subgraph
// and caches it here, the on-chain extractor's own Redis lookup (which
// runs before it falls back to an eth_call) gets a hit for free, even
// though the two components never call each other directly.
package dexcache

import (
	"context"
	"strconv"
	"strings"
	"time"

	"unified-tx-parser/internal/model"

	"github.com/redis/go-redis/v9"
	log "github.com/sirupsen/logrus"
)

// ttl applies to both token and pool cache entries. Pool identity fields
// (factory/token0/token1/fee) never change once a pool exists, so in
// principle a pool entry could be cached forever — but using the same TTL
// as tokens keeps this package simple (one policy, not two), and anything
// still actively traded will naturally get re-cached well before 30 days
// by either a future subgraph sync or on-chain re-discovery.
const ttl = 30 * 24 * time.Hour

const (
	tokenPrefix = "token_meta:"
	poolPrefix  = "pool_meta:"
)

// requestTimeout bounds each individual Redis call. Short and strict:
// caching is a best-effort acceleration layer here, not a source of truth,
// so a slow/unavailable Redis should never meaningfully delay the caller.
const requestTimeout = 2 * time.Second

// warmupMarkerKey records that a full dexcache warmup has already run
// recently. Checked before starting a new warmup (see IsWarmed) so that:
//   - A restart shortly after a previous warmup doesn't redundantly
//     re-read the whole DB and re-push everything into Redis — the data
//     is already there (individual entries carry their own ttl and don't
//     need re-warming before they'd naturally expire anyway).
//   - Running multiple parser processes against the same Redis (one per
//     chain, a normal deployment shape for this project) doesn't have
//     every single one of them independently perform the same full
//     warmup on startup — whichever process starts first "wins" and sets
//     the marker; the rest see it and skip.
//
// This is a best-effort marker, not a distributed lock: two processes
// starting at the exact same moment could both miss the marker and both
// warm concurrently. That's wasteful but not harmful (the writes are
// idempotent), so it isn't worth the added complexity of a real
// SETNX-based lock for what should be a rare race in practice.
const warmupMarkerKey = "dexcache:warmup:marker"

// IsWarmed reports whether a full warmup has run recently (within ttl —
// the marker uses the same TTL as individual cache entries, since
// re-warming more often than entries would naturally expire on their own
// accomplishes nothing: nothing has actually gone stale yet). Returns
// false on any Redis error, which is the safe default — worst case is an
// unnecessary warmup, not a missed one.
func IsWarmed(ctx context.Context, client *redis.Client) bool {
	if client == nil {
		return false
	}
	callCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	return client.Exists(callCtx, warmupMarkerKey).Val() > 0
}

// MarkWarmed records that a warmup just completed. Call after a warmup
// run (successful or not — even a partially-failed warmup shouldn't
// trigger every subsequent restart to redundantly retry the whole thing;
// individual cache misses still fall through to their normal resolution
// path regardless).
func MarkWarmed(ctx context.Context, client *redis.Client) {
	if client == nil {
		return
	}
	callCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	if err := client.Set(callCtx, warmupMarkerKey, time.Now().Unix(), ttl).Err(); err != nil {
		log.Warnf("[dexcache] failed to set warmup marker: %v", err)
	}
}

// WarmTokens writes many tokens to Redis in one pipelined round trip
// (batched in chunks — see warmBatchSize), instead of the 2 network round
// trips per token (HSet + Expire) that calling CacheToken in a loop would
// cost. This matters specifically at startup: warming tens of thousands of
// tokens one-by-one over the network can take a very long time and would
// otherwise measurably delay process startup; pipelining collapses that to
// a handful of round trips.
//
// Returns the number of tokens NOT successfully cached — 0 means every
// batch succeeded. Errors for a batch are logged, not returned as an
// error value, matching this package's best-effort posture; the count is
// returned (rather than only logged) so a caller warming a large dataset
// can tell "fully warmed" apart from "partially warmed" instead of taking
// a log line's word for it.
//
// If ctx is cancelled or its deadline passes partway through, WarmTokens
// stops immediately rather than continuing to submit batches that are
// guaranteed to fail — each already-expired-context Exec call fails
// instantly with no network round trip, so without this check a large
// remaining batch count would spam one failure log line per batch in a
// tight loop instead of one clear "stopped early" message.
func WarmTokens(ctx context.Context, client *redis.Client, tokens map[string]model.Token) int {
	if client == nil || len(tokens) == 0 {
		return 0
	}
	addrs := make([]string, 0, len(tokens))
	for addr := range tokens {
		addrs = append(addrs, addr)
	}
	failed := 0
	for i := 0; i < len(addrs); i += warmBatchSize {
		if err := ctx.Err(); err != nil {
			remaining := len(addrs) - i
			log.Warnf("[dexcache] warm tokens: stopping early at %d/%d (%v)", i, len(addrs), err)
			return failed + remaining
		}
		end := min(i+warmBatchSize, len(addrs))
		pipe := client.Pipeline()
		for _, addr := range addrs[i:end] {
			token := tokens[addr]
			key := tokenPrefix + strings.ToLower(addr)
			pipe.HSet(ctx, key, map[string]interface{}{
				"decimals": token.Decimals,
				"symbol":   token.Symbol,
				"name":     token.Name,
			})
			pipe.Expire(ctx, key, ttl)
		}
		if _, err := pipe.Exec(ctx); err != nil {
			log.Warnf("[dexcache] warm tokens batch %d-%d failed: %v", i, end, err)
			failed += end - i
		}
	}
	return failed
}

// WarmPools is the pool counterpart of WarmTokens — see its doc comment
// for why pipelining/batching and the early-exit-on-expired-context check
// matter. Returns the number of pools NOT successfully cached.
func WarmPools(ctx context.Context, client *redis.Client, pools map[string]model.Pool) int {
	if client == nil || len(pools) == 0 {
		return 0
	}
	addrs := make([]string, 0, len(pools))
	for addr := range pools {
		addrs = append(addrs, addr)
	}
	failed := 0
	for i := 0; i < len(addrs); i += warmBatchSize {
		if err := ctx.Err(); err != nil {
			remaining := len(addrs) - i
			log.Warnf("[dexcache] warm pools: stopping early at %d/%d (%v)", i, len(addrs), err)
			return failed + remaining
		}
		end := min(i+warmBatchSize, len(addrs))
		pipe := client.Pipeline()
		for _, addr := range addrs[i:end] {
			pool := pools[addr]
			key := poolPrefix + strings.ToLower(addr)
			pipe.HSet(ctx, key, map[string]interface{}{
				"factory":  pool.Factory,
				"protocol": pool.Protocol,
				"token0":   pool.Tokens[0],
				"token1":   pool.Tokens[1],
				"fee":      pool.Fee,
				"source":   string(pool.Source),
			})
			pipe.Expire(ctx, key, ttl)
		}
		if _, err := pipe.Exec(ctx); err != nil {
			log.Warnf("[dexcache] warm pools batch %d-%d failed: %v", i, end, err)
			failed += end - i
		}
	}
	return failed
}

// warmBatchSize caps how many tokens/pools go into a single pipelined
// Exec call. Unbounded pipelines risk one oversized command buffer and a
// single all-or-nothing failure for the whole warmup; chunking keeps
// partial progress on a mid-warmup error and bounds memory for the pending
// command queue.
const warmBatchSize = 1000

// CacheToken writes token's metadata to Redis, keyed by address. Errors are
// logged, not returned — see the package doc comment on why this is
// best-effort. Safe to call with a nil client (no-op), so callers that
// make Redis optional (like internal/thegraph.Syncer) don't need to guard
// every call site.
func CacheToken(ctx context.Context, client *redis.Client, token model.Token) {
	if client == nil {
		return
	}
	callCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	key := tokenPrefix + strings.ToLower(token.Addr)
	fields := map[string]interface{}{
		"decimals": token.Decimals,
		"symbol":   token.Symbol,
		"name":     token.Name,
	}
	if err := client.HSet(callCtx, key, fields).Err(); err != nil {
		log.Warnf("[dexcache] failed to cache token %s: %v", token.Addr, err)
		return
	}
	client.Expire(callCtx, key, ttl)
}

// GetToken reads cached metadata for addr. Returns ok=false on any miss or
// error (key not found, Redis unavailable, malformed decimals value) —
// callers should treat this the same as a cache miss and fall through to
// their next resolution step, not as a fatal error. Safe to call with a
// nil client (always a miss).
func GetToken(ctx context.Context, client *redis.Client, addr string) (model.Token, bool) {
	if client == nil {
		return model.Token{}, false
	}
	callCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	key := tokenPrefix + strings.ToLower(addr)
	vals, err := client.HGetAll(callCtx, key).Result()
	if err != nil || len(vals) == 0 {
		return model.Token{}, false // includes redis.Nil (key doesn't exist)
	}
	decimals, err := strconv.Atoi(vals["decimals"])
	if err != nil {
		return model.Token{}, false
	}
	return model.Token{
		Addr:     addr,
		Decimals: decimals,
		Symbol:   vals["symbol"],
		Name:     vals["name"],
	}, true
}

// CachePool writes pool's identity fields to Redis, keyed by address. Only
// the fields needed to identify/route a pool are cached (factory, protocol,
// tokens, fee, source) — not Extra, which carries per-event/per-sync
// provenance data that's cheap to re-derive and not useful for a lookup
// cache. Safe to call with a nil client (no-op).
func CachePool(ctx context.Context, client *redis.Client, pool model.Pool) {
	if client == nil {
		return
	}
	callCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	key := poolPrefix + strings.ToLower(pool.Addr)
	fields := map[string]interface{}{
		"factory":  pool.Factory,
		"protocol": pool.Protocol,
		"token0":   pool.Tokens[0],
		"token1":   pool.Tokens[1],
		"fee":      pool.Fee,
		"source":   string(pool.Source),
	}
	if err := client.HSet(callCtx, key, fields).Err(); err != nil {
		log.Warnf("[dexcache] failed to cache pool %s: %v", pool.Addr, err)
		return
	}
	client.Expire(callCtx, key, ttl)
}

// GetPool reads cached identity fields for addr. Returns ok=false on any
// miss or error. Safe to call with a nil client (always a miss).
func GetPool(ctx context.Context, client *redis.Client, addr string) (model.Pool, bool) {
	if client == nil {
		return model.Pool{}, false
	}
	callCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	key := poolPrefix + strings.ToLower(addr)
	vals, err := client.HGetAll(callCtx, key).Result()
	if err != nil || len(vals) == 0 {
		return model.Pool{}, false
	}
	fee, err := strconv.Atoi(vals["fee"])
	if err != nil {
		return model.Pool{}, false
	}
	return model.Pool{
		Addr:     addr,
		Factory:  vals["factory"],
		Protocol: vals["protocol"],
		Tokens:   map[int]string{0: vals["token0"], 1: vals["token1"]},
		Fee:      fee,
		Source:   model.PoolSource(vals["source"]),
	}, true
}
