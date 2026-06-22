package model

type Pool struct {
	Addr     string                 `json:"addr"`     // pool address
	Factory  string                 `json:"factory"`  // bluefin factory address
	Protocol string                 `json:"protocol"` // bluefin
	Tokens   map[int]string         `json:"tokens"`   //
	Args     map[string]interface{} `json:"args,omitempty"`
	Extra    *PoolExtra             `json:"extra,omitempty"`
	Fee      int                    `json:"fee"` // fee in bps
}

type PoolExtra struct {
	Hash   string `json:"tx_hash"`
	From   string `json:"tx_from"`
	Time   uint64 `json:"tx_time,omitempty"`
	Stable bool   `json:"stable,omitempty"`
}
