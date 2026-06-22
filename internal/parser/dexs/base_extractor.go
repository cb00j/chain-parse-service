package dex

import (
	"context"
	"sync"
	"time"

	"unified-tx-parser/internal/model"
	"unified-tx-parser/internal/types"

	"github.com/sirupsen/logrus"
)

// BaseDexExtractor provides common functionality for all DEX extractors
type BaseDexExtractor struct {
	protocols       []string
	supportedChains []types.ChainType
	quoteAssets     map[string]int
	log             *logrus.Entry

	// Optional caching
	enableTokenCache bool
	enablePoolCache  bool
	tokenCacheTTL    time.Duration
	poolCacheTTL     time.Duration
	cacheMutex       sync.RWMutex
}

// BaseDexExtractorConfig configures the base extractor
type BaseDexExtractorConfig struct {
	Protocols           []string
	SupportedChains     []types.ChainType
	QuoteAssets         map[string]int
	EnableTokenCache    bool
	EnablePoolCache     bool
	TokenCacheTTL       time.Duration
	PoolCacheTTL        time.Duration
	LoggerModuleName    string
}

// NewBaseDexExtractor creates a base extractor with given configuration
func NewBaseDexExtractor(cfg *BaseDexExtractorConfig) *BaseDexExtractor {
	if cfg == nil {
		cfg = &BaseDexExtractorConfig{
			SupportedChains: []types.ChainType{},
			QuoteAssets:     make(map[string]int),
		}
	}

	moduleName := cfg.LoggerModuleName
	if moduleName == "" {
		moduleName = "dex-extractor"
	}

	return &BaseDexExtractor{
		protocols:        cfg.Protocols,
		supportedChains:  cfg.SupportedChains,
		quoteAssets:      cfg.QuoteAssets,
		log:              logrus.WithFields(logrus.Fields{"service": "parser", "module": moduleName}),
		enableTokenCache: cfg.EnableTokenCache,
		enablePoolCache:  cfg.EnablePoolCache,
		tokenCacheTTL:    cfg.TokenCacheTTL,
		poolCacheTTL:     cfg.PoolCacheTTL,
		cacheMutex:       sync.RWMutex{},
	}
}

// GetSupportedProtocols returns the protocols supported by this extractor
func (b *BaseDexExtractor) GetSupportedProtocols() []string {
	return b.protocols
}

// GetSupportedChains returns the chains supported by this extractor
func (b *BaseDexExtractor) GetSupportedChains() []types.ChainType {
	return b.supportedChains
}

// SetQuoteAssets sets the quote assets (e.g., stablecoins) for price calculation
func (b *BaseDexExtractor) SetQuoteAssets(assets map[string]int) {
	if len(assets) > 0 {
		b.cacheMutex.Lock()
		defer b.cacheMutex.Unlock()
		b.quoteAssets = assets
	}
}

// GetQuoteAssets returns the configured quote assets
func (b *BaseDexExtractor) GetQuoteAssets() map[string]int {
	b.cacheMutex.RLock()
	defer b.cacheMutex.RUnlock()
	assets := make(map[string]int, len(b.quoteAssets))
	for k, v := range b.quoteAssets {
		assets[k] = v
	}
	return assets
}

// IsChainSupported checks if a chain type is supported by this extractor
func (b *BaseDexExtractor) IsChainSupported(chainType types.ChainType) bool {
	for _, chain := range b.supportedChains {
		if chain == chainType {
			return true
		}
	}
	return false
}

// GetLogger returns the logger for this extractor
func (b *BaseDexExtractor) GetLogger() *logrus.Entry {
	return b.log
}

// ExtractDexData is meant to be overridden by subclasses
func (b *BaseDexExtractor) ExtractDexData(ctx context.Context, blocks []types.UnifiedBlock) (*types.DexData, error) {
	return &types.DexData{
		Pools:        make([]model.Pool, 0),
		Transactions: make([]model.Transaction, 0),
		Liquidities:  make([]model.Liquidity, 0),
		Reserves:     make([]model.Reserve, 0),
		Tokens:       make([]model.Token, 0),
	}, nil
}

// SupportsBlock is meant to be overridden by subclasses
func (b *BaseDexExtractor) SupportsBlock(block *types.UnifiedBlock) bool {
	return false
}

// MergeQuoteAssets merges external quote assets with existing ones
// External assets take precedence
func (b *BaseDexExtractor) MergeQuoteAssets(external map[string]int) {
	b.cacheMutex.Lock()
	defer b.cacheMutex.Unlock()
	for addr, rank := range external {
		b.quoteAssets[addr] = rank
	}
}

// GetQuoteAssetRank returns the rank of a quote asset, -1 if not found
func (b *BaseDexExtractor) GetQuoteAssetRank(assetAddr string) int {
	b.cacheMutex.RLock()
	defer b.cacheMutex.RUnlock()
	rank, exists := b.quoteAssets[assetAddr]
	if !exists {
		return -1
	}
	return rank
}
