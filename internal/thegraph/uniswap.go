package thegraph

import (
	"context"
	"fmt"
	"strconv"

	"unified-tx-parser/internal/model"
)

// pageSize is capped by the subgraph itself — The Graph's documented limit
// is 1000 entities per query. Using the max reduces the number of round
// trips for a large backlog.
const pageSize = 1000

// PageResult is delivered to a PageFunc once per page fetched. LastTimestamp
// and LastID are the createdAtTimestamp/id of the last (highest-ordered)
// entity in this page — the caller (Syncer) persists these as the resume
// cursor after successfully storing Pools/Tokens, so an interrupted backfill
// resumes from the last completed page instead of restarting from scratch.
type PageResult struct {
	Pools         []model.Pool
	Tokens        []model.Token
	LastTimestamp int64
	LastID        string
}

// PageFunc is called once per page during a paginated fetch. Returning an
// error stops pagination immediately (the fetch function surfaces it to its
// caller); returning nil continues to the next page.
type PageFunc func(page PageResult) error

// v2PairsQuery fetches Uniswap V2 pairs using cursor-based pagination on
// (createdAtTimestamp, id) instead of $skip.
//
// Why not $skip: The Graph enforces a hard ceiling on skip (documented as
// 5000) — past that, queries fail outright. A backlog like Uniswap V2's
// ~500k historical pairs needs 500+ pages at pageSize=1000, so skip-based
// pagination breaks after page 5. Cursor-based pagination has no such
// limit: each page's "where" clause is anchored to the last row actually
// seen, not an offset count.
//
// The cursor is the pair (createdAtTimestamp, id) rather than just
// createdAtTimestamp alone, because many pairs can share the same
// creation second — using timestamp alone as a ">" cursor could skip
// same-second pairs sorted after the last one returned, or (using ">=")
// re-return the same row forever. id is unique, so (timestamp, id) is a
// stable total order matching orderBy below:
//
//	(createdAtTimestamp > $lastTs) OR (createdAtTimestamp == $lastTs AND id > $lastId)
//
// On the very first page the caller passes lastTs = since-1, lastId = ""
// (empty string sorts before every real hex address), which makes the
// first branch equivalent to "createdAtTimestamp >= since" with no special
// case needed in the query itself.
const v2PairsQuery = `
query Pairs($lastTs: Int!, $lastId: String!, $first: Int!) {
  pairs(
    where: {
      or: [
        { createdAtTimestamp_gt: $lastTs },
        { createdAtTimestamp: $lastTs, id_gt: $lastId }
      ]
    }
    orderBy: createdAtTimestamp
    orderDirection: asc
    first: $first
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
// entity is named "pools" instead of "pairs". Same cursor-pagination
// reasoning applies.
const v3PoolsQuery = `
query Pools($lastTs: Int!, $lastId: String!, $first: Int!) {
  pools(
    where: {
      or: [
        { createdAtTimestamp_gt: $lastTs },
        { createdAtTimestamp: $lastTs, id_gt: $lastId }
      ]
    }
    orderBy: createdAtTimestamp
    orderDirection: asc
    first: $first
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

// fetchV2PairsSince fetches all V2 pairs whose (createdAtTimestamp, id)
// sorts strictly after (resumeTs, resumeID), invoking onPage once per page
// (pageSize entities) as soon as it's decoded — the caller is expected to
// persist each page and advance its own cursor immediately, rather than
// waiting for the whole backfill to finish. This matters for large
// backlogs (Uniswap V2 has ~500k historical pairs): a single all-or-nothing
// fetch would need to hold everything in memory, and any timeout or crash
// partway through would discard all progress. With per-page delivery, an
// interrupted run has already durably stored everything up to the last
// completed page, and the caller (Syncer) can resume from exactly there.
//
// To start from an inclusive timestamp boundary (e.g. "everything from
// unix time T onward") rather than resuming a specific in-progress page,
// pass resumeTs = T-1, resumeID = "" — see Syncer.syncVersion.
func (c *Client) fetchV2PairsSince(ctx context.Context, resumeTs int64, resumeID string, onPage PageFunc) error {
	lastTs, lastID := resumeTs, resumeID

	for {
		var resp v2PairsResponse
		vars := map[string]any{"lastTs": lastTs, "lastId": lastID, "first": pageSize}
		if err := c.Query(ctx, v2PairsQuery, vars, &resp); err != nil {
			return fmt.Errorf("thegraph: fetch v2 pairs (lastTs=%d lastId=%s): %w", lastTs, lastID, err)
		}
		if len(resp.Pairs) == 0 {
			return nil
		}

		pools := make([]model.Pool, 0, len(resp.Pairs))
		var tokens []model.Token
		seenTokens := make(map[string]bool)
		for _, pair := range resp.Pairs {
			pools = append(pools, v2PairToModelPool(pair))
			for _, tf := range [2]tokenField{pair.Token0, pair.Token1} {
				if seenTokens[tf.ID] {
					continue
				}
				seenTokens[tf.ID] = true
				tokens = append(tokens, tokenFieldToModelToken(tf))
			}
		}

		last := resp.Pairs[len(resp.Pairs)-1]
		lastTs, _ = strconv.ParseInt(last.CreatedAtTimestamp, 10, 64) // 0 on parse failure — see mapping.go
		lastID = last.ID

		if err := onPage(PageResult{Pools: pools, Tokens: tokens, LastTimestamp: lastTs, LastID: lastID}); err != nil {
			return err
		}

		if len(resp.Pairs) < pageSize {
			return nil // last page
		}
	}
}

// FetchV3PoolsSince is the V3 counterpart of fetchV2PairsSince — see its
// doc comment for pagination/resume-cursor semantics, which are identical
// here.
func (c *Client) FetchV3PoolsSince(ctx context.Context, resumeTs int64, resumeID string, onPage PageFunc) error {
	lastTs, lastID := resumeTs, resumeID

	for {
		var resp v3PoolsResponse
		vars := map[string]any{"lastTs": lastTs, "lastId": lastID, "first": pageSize}
		if err := c.Query(ctx, v3PoolsQuery, vars, &resp); err != nil {
			return fmt.Errorf("thegraph: fetch v3 pools (lastTs=%d lastId=%s): %w", lastTs, lastID, err)
		}
		if len(resp.Pools) == 0 {
			return nil
		}

		pools := make([]model.Pool, 0, len(resp.Pools))
		var tokens []model.Token
		seenTokens := make(map[string]bool)
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

		last := resp.Pools[len(resp.Pools)-1]
		lastTs, _ = strconv.ParseInt(last.CreatedAtTimestamp, 10, 64)
		lastID = last.ID

		if err := onPage(PageResult{Pools: pools, Tokens: tokens, LastTimestamp: lastTs, LastID: lastID}); err != nil {
			return err
		}

		if len(resp.Pools) < pageSize {
			return nil
		}
	}
}
