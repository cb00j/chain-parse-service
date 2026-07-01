package model

// PoolSource identifies where a Pool record's data came from. Distinct
// sources matter for debugging data quality issues — e.g. a pool prefetched
// from The Graph should have real token0/token1 addresses (and often
// decimals via the linked Token), while one produced by the lazy-pool
// placeholder path on-chain may not.
type PoolSource string

const (
	PoolSourceOnchain  PoolSource = "onchain"  // scanned from a PairCreated/PoolCreated event
	PoolSourceTheGraph PoolSource = "thegraph" // prefetched from a subgraph, ahead of any on-chain scan
)

type Pool struct {
	Addr     string                 `json:"addr"`     // pool address
	Factory  string                 `json:"factory"`  // bluefin factory address
	Protocol string                 `json:"protocol"` // bluefin
	Tokens   map[int]string         `json:"tokens"`   //
	Args     map[string]interface{} `json:"args,omitempty"`
	Extra    *PoolExtra             `json:"extra,omitempty"`
	Fee      int                    `json:"fee"` // fee in bps
	Source   PoolSource             `json:"source,omitempty"`
}

type PoolExtra struct {
	Hash   string `json:"tx_hash"`
	From   string `json:"tx_from"`
	Time   uint64 `json:"tx_time,omitempty"`
	Stable bool   `json:"stable,omitempty"`
}
