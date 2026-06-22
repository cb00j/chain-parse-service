package model

type Token struct {
	Addr         string  `json:"addr"`
	Name         string  `json:"name"`
	Symbol       string  `json:"symbol"`
	Decimals     int     `json:"decimals"`
	TRLThreshold int     `json:"trlThreshold,omitempty"` // TRL threshold for this token
	IsStable     bool    `json:"is_stable"`
	CreatedAt    string  `json:"created_at,omitempty"` // ISO 8601 format
	UsdPrice     float64 `json:"price_usd,omitempty"`  // USD price of the token
}
