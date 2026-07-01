package thegraph

import (
	"strconv"

	"unified-tx-parser/internal/model"
)

// uniswapV2FactoryAddr / uniswapV3FactoryAddr mirror the constants in
// internal/parser/dexs/eth/uniswap.go. Duplicated here rather than
// imported to avoid this package depending on the eth extractor package
// (which would be a backwards, storage-layer-depending-on-extractor
// dependency) — these are well-known, static addresses that don't belong
// to either package specifically.
const (
	uniswapV2FactoryAddr = "0x5C69bEe701ef814a2B6a3EDD4B1652CB9cc5aA6f"
	uniswapV3FactoryAddr = "0x1F98431c8aD98523631AE4a59f267346ea31F984"
)

// v2PairToModelPool converts a subgraph Pair into model.Pool.
// Fee is fixed at 3000 (0.3%) because that's the only fee tier V2 pairs
// have — there's no feeTier field on the V2 subgraph's Pair entity because
// the protocol itself doesn't have variable fees.
func v2PairToModelPool(pair v2Pair) model.Pool {
	createdAt, _ := strconv.ParseUint(pair.CreatedAtTimestamp, 10, 64) // 0 on parse failure; only used to advance the sync cursor, not persisted as authoritative
	return model.Pool{
		Addr:     pair.ID,
		Factory:  uniswapV2FactoryAddr,
		Protocol: "uniswap_v2",
		Tokens: map[int]string{
			0: pair.Token0.ID,
			1: pair.Token1.ID,
		},
		Fee:    3000,
		Source: model.PoolSourceTheGraph,
		Extra:  &model.PoolExtra{Time: createdAt},
	}
}

// v3PoolToModelPool converts a subgraph Pool into model.Pool. Unlike V2,
// feeTier is a real per-pool value (500/3000/10000 etc), parsed from the
// subgraph's string representation.
func v3PoolToModelPool(pool v3Pool) model.Pool {
	createdAt, _ := strconv.ParseUint(pool.CreatedAtTimestamp, 10, 64)
	fee, _ := strconv.Atoi(pool.FeeTier) // defaults to 0 on parse failure — see note below
	return model.Pool{
		Addr:     pool.ID,
		Factory:  uniswapV3FactoryAddr,
		Protocol: "uniswap_v3",
		Tokens: map[int]string{
			0: pool.Token0.ID,
			1: pool.Token1.ID,
		},
		Fee:    fee,
		Source: model.PoolSourceTheGraph,
		Extra:  &model.PoolExtra{Time: createdAt},
	}
}

// tokenFieldToModelToken converts a subgraph token field into model.Token.
//
// Decimals parse failure intentionally falls back to 0, not 18: a
// prefetched record with decimals=0 is easy to spot as "something went
// wrong with this specific token" (0-decimal tokens are rare enough to be
// suspicious) and won't silently misprice everything the way a wrong "18
// looked plausible" default would — this mirrors the same
// don't-guess-quietly principle behind not normalizing raw amounts at
// write time elsewhere in this codebase.
func tokenFieldToModelToken(tf tokenField) model.Token {
	decimals, err := strconv.Atoi(tf.Decimals)
	if err != nil {
		decimals = 0
	}
	return model.Token{
		Addr:     tf.ID,
		Name:     tf.Name,
		Symbol:   tf.Symbol,
		Decimals: decimals,
	}
}
