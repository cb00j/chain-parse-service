package thegraph

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"unified-tx-parser/internal/config"
	"unified-tx-parser/internal/model"
	"unified-tx-parser/internal/types"

	log "github.com/sirupsen/logrus"
)

// source identifies this package to the generic CursorStore (see
// internal/types.CursorStore) — distinct from "chain" or "protocol" so the
// same sync_cursors table can later hold cursors for a completely
// different external data source without any schema change.
const source = "thegraph"

// cursorKey names the field this sync tracks. Both V2 and V3 subgraphs
// paginate/increment on createdAtTimestamp, so one key name covers both;
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
// StorageEngine and persisting incremental-sync progress via CursorStore.
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
}

// NewSyncer creates a Syncer. At least one of cfg.V2Endpoint/V3Endpoint
// must be set, or every SyncOnce call is a no-op.
func NewSyncer(cfg config.TheGraphConfig, chainType string, storage types.StorageEngine, cursors types.CursorStore) (*Syncer, error) {
	if storage == nil {
		return nil, fmt.Errorf("thegraph: storage engine is required")
	}
	if cursors == nil {
		return nil, fmt.Errorf("thegraph: cursor store is required")
	}

	s := &Syncer{
		chainType: chainType,
		cfg:       cfg,
		storage:   storage,
		cursors:   cursors,
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

// SyncOnce runs a single incremental sync pass for every configured
// subgraph version. It does not return on the first error — V2 and V3 are
// independent, so a failure in one shouldn't block the other — but
// aggregates and returns all errors encountered.
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

// fetchFunc matches the signature shared by fetchV2PairsSince and
// FetchV3PoolsSince, letting syncVersion stay version-agnostic.
type fetchFunc func(ctx context.Context, sinceUnix int64) ([]model.Pool, []model.Token, error)

// syncVersion runs the fetch → store → advance-cursor sequence for one
// subgraph version (V2 or V3).
func (s *Syncer) syncVersion(ctx context.Context, protocol string, fetch fetchFunc) error {
	since := s.cfg.InitialSince

	if saved, ok, err := s.cursors.GetCursor(ctx, source, s.chainType, protocol, cursorKey); err != nil {
		// Cursor read failure shouldn't block the sync entirely — fall back
		// to InitialSince (or 0), which is safe, just potentially a bigger
		// re-fetch. Log loudly since silently losing incremental state is
		// exactly the kind of thing that should be visible in ops.
		log.Warnf("[thegraph] %s: failed to read cursor, falling back to initial_since=%d: %v", protocol, since, err)
	} else if ok {
		if parsed, err := strconv.ParseInt(saved, 10, 64); err == nil {
			since = parsed
		} else {
			log.Warnf("[thegraph] %s: stored cursor %q is not a valid integer, falling back to initial_since=%d", protocol, saved, since)
		}
	}

	pools, tokens, err := fetch(ctx, since)
	if err != nil {
		return fmt.Errorf("fetch since %d: %w", since, err)
	}

	if len(pools) == 0 {
		log.Infof("[thegraph] %s: no new pools since %d", protocol, since)
		return nil
	}

	if err := s.storage.StoreDexData(ctx, &types.DexData{Pools: pools, Tokens: tokens}); err != nil {
		return fmt.Errorf("store %d pools / %d tokens: %w", len(pools), len(tokens), err)
	}

	// Advance the cursor to the max createdAtTimestamp actually seen in
	// this batch (not "now") — see fetchV2PairsSince's doc comment on why
	// sinceUnix is inclusive and safe to re-request.
	var maxCreatedAt uint64
	for _, p := range pools {
		if p.Extra != nil && p.Extra.Time > maxCreatedAt {
			maxCreatedAt = p.Extra.Time
		}
	}
	if maxCreatedAt > 0 {
		if err := s.cursors.SetCursor(ctx, source, s.chainType, protocol, cursorKey, strconv.FormatUint(maxCreatedAt, 10)); err != nil {
			// Storing already succeeded — a failed cursor write just means
			// the next run re-fetches this batch (harmless re-upsert), so
			// this is a warning, not a sync failure.
			log.Warnf("[thegraph] %s: synced %d pools but failed to advance cursor: %v", protocol, len(pools), err)
		}
	}

	log.Infof("[thegraph] %s: synced %d pools, %d tokens, cursor now %d", protocol, len(pools), len(tokens), maxCreatedAt)
	return nil
}
