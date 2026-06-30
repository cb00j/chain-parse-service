package influxdb

import "unified-tx-parser/internal/model"

// SwapTimeOffset is the explicit, single source of truth for how each
// SwapRecord's role maps to its position within a log_index's reserved
// 10-slot window. Anything in this package that needs to know "what offset
// does a token0 row get" must call this — never hardcode 0/1 elsewhere, or
// the role tag and the timestamp offset can drift out of sync (identified
// during design as the one thing that must not happen).
//
// This function — and SyntheticTimestampNanos below — exist only because
// InfluxDB has no real primary key: a point's identity is entirely
// determined by measurement + tags + timestamp, and overwriting an
// existing point requires reconstructing that exact identity. A relational
// store (PostgreSQL, ClickHouse, etc) would instead give each SwapRecord a
// real primary key like (tx_hash, log_index, role) and a plain UPDATE
// statement, with no need for any of this — which is why this logic lives
// in this package and not in internal/model alongside SwapRecord itself.
//
// Slots 0-1 are used today (the two rows of a plain swap). Slots 2-9 are
// reserved for future row types (e.g. splitting out LP/protocol fees into
// their own rows) without needing to redesign the timestamp scheme.
func SwapTimeOffset(role model.SwapRecordRole) int64 {
	switch role {
	case model.RoleToken0:
		return 0
	case model.RoleToken1:
		return 1
	default:
		// Should be unreachable — RoleToken0/RoleToken1 are the only valid
		// constructors of model.SwapRecordRole. Returning 9 (last reserved
		// slot) rather than panicking keeps a malformed caller from
		// corrupting an existing, valid slot if this is ever hit in
		// production.
		return 9
	}
}

// SyntheticTimestampNanos computes the InfluxDB point timestamp for a
// SwapRecord: blockTime*1e9 + logIndex*10 + offset(role).
//
// Why this exists:
//   - log_index is globally unique within a block, so two distinct swap
//     events never collide here.
//   - The *10 spacing reserves slots 2-9 per log_index for future row types
//     (e.g. fee rows) without needing a new timestamp scheme.
//   - The role-based offset is also the last line of defense against a
//     pathological/non-standard pool where token0 == token1: without it,
//     the two rows of such a swap would share identical tags+timestamp and
//     one would silently overwrite the other in InfluxDB.
//
// Overflow check: blockTime (~1.7e9 today) * 1e9 ≈ 1.7e18, comfortably
// under the int64 ceiling (~9.2e18) even after adding logIndex*10+offset
// (at most a few thousand). This holds for the foreseeable future of
// Ethereum block timestamps; revisit if blockTime itself ever approaches
// 9.2e9 (year ~2261 at 1 second per block — not a near-term concern).
func SyntheticTimestampNanos(blockTime uint64, logIndex int64, role model.SwapRecordRole) int64 {
	return int64(blockTime)*1_000_000_000 + logIndex*10 + SwapTimeOffset(role)
}
