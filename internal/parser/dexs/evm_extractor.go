package dex

import (
	"time"

	"unified-tx-parser/internal/types"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

// EVMDexExtractor provides shared functionality for EVM-based DEX extractors (Ethereum, BSC, etc.)
type EVMDexExtractor struct {
	*BaseDexExtractor
}

// NewEVMDexExtractor creates an EVM DEX extractor with the given configuration
func NewEVMDexExtractor(cfg *BaseDexExtractorConfig) *EVMDexExtractor {
	if cfg == nil {
		cfg = &BaseDexExtractorConfig{}
	}
	return &EVMDexExtractor{
		BaseDexExtractor: NewBaseDexExtractor(cfg),
	}
}

// ExtractEVMLogs extracts Ethereum logs from a unified transaction
// This method centralizes log extraction logic that was previously duplicated
func (e *EVMDexExtractor) ExtractEVMLogs(tx *types.UnifiedTransaction) []*ethtypes.Log {
	if tx == nil || tx.RawData == nil {
		return []*ethtypes.Log{}
	}

	// Try to get logs from RawData
	switch rawData := tx.RawData.(type) {
	case *ethtypes.Receipt:
		if rawData != nil && rawData.Logs != nil {
			return rawData.Logs
		}
	case []*ethtypes.Log:
		return rawData
	}

	return []*ethtypes.Log{}
}

// IsEVMChainSupported checks if an EVM chain is supported
func (e *EVMDexExtractor) IsEVMChainSupported(chainType types.ChainType) bool {
	return chainType == types.ChainTypeEthereum || chainType == types.ChainTypeBSC
}

// FilterLogsByTopics filters logs by their first topic (event signature)
func (e *EVMDexExtractor) FilterLogsByTopics(logs []*ethtypes.Log, topicFilter map[string]bool) []*ethtypes.Log {
	filtered := make([]*ethtypes.Log, 0)
	for _, log := range logs {
		if len(log.Topics) == 0 {
			continue
		}
		topic0 := log.Topics[0].Hex()
		if topicFilter[topic0] {
			filtered = append(filtered, log)
		}
	}
	return filtered
}

// ShouldCacheExpire checks if a cache entry has expired
func ShouldCacheExpire(expiresAt time.Time) bool {
	return time.Now().After(expiresAt)
}

// CalculateCacheExpiration calculates the expiration time for a cache entry
func CalculateCacheExpiration(ttl time.Duration) time.Time {
	return time.Now().Add(ttl)
}
