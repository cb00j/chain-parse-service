package thegraph

import (
	"context"
	"fmt"
	"unified-tx-parser/internal/model"
)

// pageSize is capped by the subgraph itself — The Graph's documented limit
// is 1000 entities per query (see project history: this was verified
// against Uniswap's own subgraph docs). Using the max reduces the number of
// round trips for a large backlog.
const pageSize = 1000

// v2PairsQuery fetches Uniswap V2 pairs created at or after $since, ordered
// by createdAtTimestamp so that pagination via $skip is stable even if new
// pairs are created between pages (new pairs sort after whatever page we're
// currently on, so they don't shift earlier rows out from under us).
//
// token0/token1 are nested objects because the V2 subgraph's Pair entity
// embeds them directly — one query gets both the pair and its two tokens'
// full metadata (id/symbol/name/decimals), which is why this single query
// can populate both dex_pools and dex_tokens without a second round trip.
const v2PairsQuery = `
query Pairs($since: Int!, $skip: Int!, $first: Int!) {
  pairs(
    where: { createdAtTimestamp_gte: $since }
    orderBy: createdAtTimestamp
    orderDirection: asc
    first: $first
    skip: $skip
  ) {
    id
    createdAtTimestamp
    token0 { id symbol name decimals }
    token1 { id symbol name decimals }
  }
}`

type v2PairsResponse struct {
	Pairs []v2Pair `json:"pairs"`
}

type v2Pair struct {
	ID                 string     `json:"id"`
	CreatedAtTimestamp string     `json:"createdAtTimestamp"` // subgraph BigInt fields are returned as strings
	Token0             tokenField `json:"token0"`
	Token1             tokenField `json:"token1"`
}

type tokenField struct {
	ID       string `json:"id"`
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Decimals string `json:"decimals"` // also a BigInt-as-string in the subgraph schema
}

// v3PoolsQuery mirrors v2PairsQuery but for the V3 subgraph, where the
// entity is named "pools" instead of "pairs". Field shape is otherwise the
// same for our purposes.
const v3PoolsQuery = `
query Pools($since: Int!, $skip: Int!, $first: Int!) {
  pools(
    where: { createdAtTimestamp_gte: $since }
    orderBy: createdAtTimestamp
    orderDirection: asc
    first: $first
    skip: $skip
  ) {
    id
    createdAtTimestamp
    feeTier
    token0 { id symbol name decimals }
    token1 { id symbol name decimals }
  }
}`

type v3PoolsResponse struct {
	Pools []v3Pool `json:"pools"`
}

type v3Pool struct {
	ID                 string     `json:"id"`
	CreatedAtTimestamp string     `json:"createdAtTimestamp"`
	FeeTier            string     `json:"feeTier"`
	Token0             tokenField `json:"token0"`
	Token1             tokenField `json:"token1"`
}

// FetchV2PairsSince fetches all V2 pairs created at or after sinceUnix,
// paginating through the subgraph's 1000-entity-per-query limit until
// exhausted. Returns pools and their tokens as model structs, ready to be
// written to dex_pools/dex_tokens — callers don't need to know anything
// about the subgraph's schema.
//
// sinceUnix is inclusive (createdAtTimestamp_gte) so callers doing
// incremental sync can safely re-request the last synced timestamp on
// every run without gaps — at worst this re-fetches (and harmlessly
// re-upserts) the pairs from that exact second again.
func (c *Client) fetchV2PairsSince(ctx context.Context, sinceUnix int64) ([]model.Pool, []model.Token, error) {
	var pools []model.Pool
	var tokens []model.Token
	seenTokens := make(map[string]bool)

	skip := 0

	for {
		var resp v2PairsResponse
		vars := map[string]any{"since": sinceUnix, "skip": skip, "first": pageSize}
		if err := c.Query(ctx, v2PairsQuery, vars, &resp); err != nil {
			return nil, nil, fmt.Errorf("thegraph: fetch v2 pairs (skip=%d): %w", skip, err)
		}
		for _, pair := range resp.Pairs {
			pools = append(pools, v2PairToModelPool(pair))
			for _, tf := range [2]tokenField{pair.Token0, pair.Token1} {
				if seenTokens[tf.ID] {
					continue
				}
				seenTokens[tf.ID] = true
				tokens = append(tokens, tokenFiledToModelToken(tf))
			}
		}

		if len(resp.Pairs) < pageSize {
			break // last page
		}

		skip += pageSize
	}

	return pools, tokens, nil
}

// FetchV3PoolsSince is the V3 counterpart of FetchV2PairsSince — see its
// doc comment for pagination/incremental-sync semantics, which are
// identical here.
func (c *Client) FetchV3PoolsSince(ctx context.Context, sinceUnix int64) ([]model.Pool, []model.Token, error) {
	var pools []model.Pool
	var tokens []model.Token
	seenTokens := make(map[string]bool)

	skip := 0
	for {
		var resp v3PoolsResponse
		vars := map[string]any{"since": sinceUnix, "skip": skip, "first": pageSize}
		if err := c.Query(ctx, v3PoolsQuery, vars, &resp); err != nil {
			return nil, nil, fmt.Errorf("thegraph: fetch v3 pools (skip=%d): %w", skip, err)
		}

		for _, pool := range resp.Pools {
			pools = append(pools, v3PoolToModelPool(pool))
			for _, tf := range [2]tokenField{pool.Token0, pool.Token1} {
				if seenTokens[tf.ID] {
					continue
				}
				seenTokens[tf.ID] = true
				tokens = append(tokens, tokenFieldToModelToken(tf))
			}
		}

		if len(resp.Pools) < pageSize {
			break
		}
		skip += pageSize
	}

	return pools, tokens, nil
}
