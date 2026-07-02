package thegraph

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"unified-tx-parser/internal/config"
	"unified-tx-parser/internal/dexcache"
	"unified-tx-parser/internal/model"
	"unified-tx-parser/internal/types"

	"github.com/redis/go-redis/v9"
	log "github.com/sirupsen/logrus"
)

// source identifies this package to the generic CursorStore (see
// internal/types.CursorStore) — distinct from "chain" or "protocol" so the
// same sync_cursors table can later hold cursors for a completely
// different external data source without any schema change.
const source = "thegraph"

// cursorKey names the field this sync tracks. Both V2 and V3 subgraphs
// paginate on (createdAtTimestamp, id), so one key name covers both;
// they're still stored as separate rows because protocol differs.
const cursorKey = "created_at_timestamp"

// protocol identifiers, matching what mapping.go writes into
// model.Pool.Protocol — kept as constants here so the cursor rows and the
// dex_pools.protocol values can never drift apart from typos.
const (
	protocolUniswapV2 = "uniswap_v2"
	protocolUniswapV3 = "uniswap_v3"
)

// Syncer runs one-shot or periodic prefetch syncs against the Uniswap V2/V3
// subgraphs, writing results into dex_pools/dex_tokens via the shared
// StorageEngine and persisting resume progress via CursorStore.
//
// It is deliberately Ethereum/Uniswap-specific for now (matching
// internal/thegraph/uniswap.go) — chainType is fixed at construction time
// rather than inferred, so a future BSC/PancakeSwap subgraph syncer can
// reuse the same CursorStore table and StorageEngine without this type
// needing to become a generic multi-protocol dispatcher prematurely.
type Syncer struct {
	chainType string
	cfg       config.TheGraphConfig

	v2Client *Client // nil if V2Endpoint is unconfigured
	v3Client *Client // nil if V3Endpoint is unconfigured

	storage types.StorageEngine
	cursors types.CursorStore

	// redisClient is optional — nil disables the dexcache write-through
	// entirely (dexcache's functions are all nil-safe). See NewSyncer.
	redisClient *redis.Client
}

// NewSyncer creates a Syncer. At least one of cfg.V2Endpoint/V3Endpoint
// must be set, or every SyncOnce call is a no-op.
//
// redisClient is optional (nil disables it) — when set, every page
// successfully written to storage is also written to dexcache
// (internal/dexcache), giving on-chain extractors (e.g. UniswapExtractor's
// token metadata lookup) a Redis hit instead of falling through to an
// eth_call/DB query for anything this sync already resolved. Passed as a
// constructor param rather than a setter (contrast
// UniswapExtractor.SetTokenCacheRedis) because Syncer has no
// registration-phase lifecycle to hook into — it's just constructed once
// in cmd/thegraph-sync's main.
func NewSyncer(cfg config.TheGraphConfig, chainType string, storage types.StorageEngine, cursors types.CursorStore, redisClient *redis.Client) (*Syncer, error) {
	if storage == nil {
		return nil, fmt.Errorf("thegraph: storage engine is required")
	}
	if cursors == nil {
		return nil, fmt.Errorf("thegraph: cursor store is required")
	}

	s := &Syncer{
		chainType:   chainType,
		cfg:         cfg,
		storage:     storage,
		cursors:     cursors,
		redisClient: redisClient,
	}

	if cfg.V2Endpoint != "" {
		s.v2Client = NewClient(cfg.V2Endpoint, cfg.APIKey)
	}
	if cfg.V3Endpoint != "" {
		s.v3Client = NewClient(cfg.V3Endpoint, cfg.APIKey)
	}
	if s.v2Client == nil && s.v3Client == nil {
		return nil, fmt.Errorf("thegraph: neither v2_endpoint nor v3_endpoint is configured")
	}

	return s, nil
}

// SyncOnce runs a single sync pass (which may itself be many pages, for a
// large backlog) for every configured subgraph version. It does not return
// on the first error — V2 and V3 are independent, so a failure in one
// shouldn't block the other — but aggregates and returns all errors
// encountered. Progress already made on either version (pages already
// stored, cursor already advanced) is retained regardless of a later
// failure — see syncVersion.
func (s *Syncer) SyncOnce(ctx context.Context) error {
	var errs []error

	if s.v2Client != nil {
		if err := s.syncVersion(ctx, protocolUniswapV2, s.v2Client.fetchV2PairsSince); err != nil {
			errs = append(errs, fmt.Errorf("v2: %w", err))
		}
	}
	if s.v3Client != nil {
		if err := s.syncVersion(ctx, protocolUniswapV3, s.v3Client.FetchV3PoolsSince); err != nil {
			errs = append(errs, fmt.Errorf("v3: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("thegraph sync: %w", errors.Join(errs...))
	}
	return nil
}

// fetchFunc matches the signature shared by Client.fetchV2PairsSince and
// Client.FetchV3PoolsSince, letting syncVersion stay version-agnostic.
type fetchFunc func(ctx context.Context, resumeTs int64, resumeID string, onPage PageFunc) error

// syncVersion runs one subgraph version's fetch, storing and advancing the
// cursor after every page rather than only at the end. This is the whole
// point for a backlog the size of Uniswap V2's (~500k pairs, 500+ pages at
// pageSize=1000): a single long-running fetch-everything-then-store call
// would (a) hold hours of data in memory, (b) lose 100% of that work if the
// process is killed, hits a context timeout, or the network blips on page
// 400 of 515, and (c) give an operator watching logs nothing to look at
// for the entire run. Per-page persistence means an interruption only
// costs the one in-flight page — resuming just picks up the cursor where
// it was left, thanks to fetchV2PairsSince/FetchV3PoolsSince's
// (timestamp, id) resume-cursor pagination (see uniswap.go).
func (s *Syncer) syncVersion(ctx context.Context, protocol string, fetch fetchFunc) error {
	resumeTs, resumeID := s.resumeCursor(ctx, protocol)

	page := 0
	var totalPools, totalTokens int

	err := fetch(ctx, resumeTs, resumeID, func(pr PageResult) error {
		page++

		if len(pr.Pools) == 0 {
			return nil
		}

		if err := s.storage.StoreDexData(ctx, &types.DexData{Pools: pr.Pools, Tokens: pr.Tokens}); err != nil {
			return fmt.Errorf("store page %d (%d pools / %d tokens): %w", page, len(pr.Pools), len(pr.Tokens), err)
		}

		// Pipelined batch write (WarmTokens/WarmPools), not a per-item
		// CacheToken/CachePool loop — a page can carry up to 1000 pools
		// and a similar number of tokens, and CacheToken/CachePool each
		// cost 2 Redis round trips (HSet + Expire). Looping that
		// individually over ~1800 items measured at ~85-90s per page in
		// practice (roughly matches ~1800 items × 2 round trips × ~25ms/
		// round trip) — almost entirely spent here, not in StoreDexData
		// or SetCursor either side of this block. WarmTokens/WarmPools
		// batch everything into pipelined Exec calls instead, the same
		// fix already applied to the startup warmup path for the same
		// reason (see cmd/parser's warmRedisCacheAsync).
		//
		// Still best-effort — nil-safe on a nil client, errors logged not
		// returned, same posture as CacheToken/CachePool. A cache miss
		// here just means the next consumer falls through to its own
		// resolution path, same as if this sync had never run.
		tokenMap := make(map[string]model.Token, len(pr.Tokens))
		for _, t := range pr.Tokens {
			tokenMap[t.Addr] = t
		}
		dexcache.WarmTokens(ctx, s.redisClient, tokenMap)

		poolMap := make(map[string]model.Pool, len(pr.Pools))
		for _, p := range pr.Pools {
			poolMap[p.Addr] = p
		}
		dexcache.WarmPools(ctx, s.redisClient, poolMap)

		cursorValue := formatCursor(pr.LastTimestamp, pr.LastID)
		if err := s.cursors.SetCursor(ctx, source, s.chainType, protocol, cursorKey, cursorValue); err != nil {
			// Storage already succeeded — a failed cursor write just means
			// this page gets re-fetched (harmless re-upsert) next run
			// instead of being skipped. Warn, don't abort: losing the
			// cursor write shouldn't throw away a page of real progress.
			log.Warnf("[thegraph] %s: page %d stored but failed to advance cursor: %v", protocol, page, err)
		}

		totalPools += len(pr.Pools)
		totalTokens += len(pr.Tokens)
		log.Infof("[thegraph] %s: page %d synced (%d pools, %d tokens), cursor now %s",
			protocol, page, len(pr.Pools), len(pr.Tokens), cursorValue)

		return nil
	})

	if err != nil {
		if page == 0 {
			return fmt.Errorf("fetch (no pages completed): %w", err)
		}
		// Pages before the failure are already durably stored and their
		// cursor already advanced — only the in-flight page's worth of
		// progress is lost, and it'll simply be re-fetched next run.
		return fmt.Errorf("fetch failed after %d page(s) (%d pools, %d tokens already synced): %w",
			page, totalPools, totalTokens, err)
	}

	if page == 0 {
		log.Infof("[thegraph] %s: no new pools since cursor %s", protocol, formatCursor(resumeTs, resumeID))
	} else {
		log.Infof("[thegraph] %s: sync complete — %d page(s), %d pools, %d tokens total",
			protocol, page, totalPools, totalTokens)
	}
	return nil
}

// resumeCursor loads the persisted (timestamp, id) resume point for a
// protocol, falling back to TheGraphConfig.InitialSince (treated as an
// inclusive "since" boundary, hence the -1) when no cursor has been saved
// yet or it can't be read/parsed.
func (s *Syncer) resumeCursor(ctx context.Context, protocol string) (resumeTs int64, resumeID string) {
	fallbackTs := s.cfg.InitialSince - 1

	saved, ok, err := s.cursors.GetCursor(ctx, source, s.chainType, protocol, cursorKey)
	if err != nil {
		log.Warnf("[thegraph] %s: failed to read cursor, falling back to initial_since=%d: %v", protocol, s.cfg.InitialSince, err)
		return fallbackTs, ""
	}
	if !ok {
		return fallbackTs, ""
	}

	ts, id, err := parseCursor(saved)
	if err != nil {
		log.Warnf("[thegraph] %s: stored cursor %q is invalid (%v), falling back to initial_since=%d", protocol, saved, err, s.cfg.InitialSince)
		return fallbackTs, ""
	}
	return ts, id
}

// formatCursor/parseCursor encode the (timestamp, id) resume cursor as a
// single string for types.CursorStore, which is deliberately a flat
// string-keyed store shared by any future sync job — see
// internal/types.CursorStore's doc comment. "id" is a hex address (never
// contains ':'), so splitting on the first ':' is unambiguous.
func formatCursor(ts int64, id string) string {
	return fmt.Sprintf("%d:%s", ts, id)
}

func parseCursor(s string) (ts int64, id string, err error) {
	tsStr, id, found := strings.Cut(s, ":")
	if !found {
		return 0, "", fmt.Errorf("missing ':' separator")
	}
	ts, err = strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("invalid timestamp %q: %w", tsStr, err)
	}
	return ts, id, nil
}
