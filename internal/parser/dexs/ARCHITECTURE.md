# DEX Extractor Architecture

This document describes the architecture of the DEX extractor system and how to implement new extractors efficiently.

## Architecture Overview

```
DexExtractors Interface
    ↓
BaseDexExtractor (shared functionality for all extractors)
    ├── EVMDexExtractor (for Ethereum, BSC, etc.)
    │   ├── UniswapExtractor
    │   ├── PancakeSwapExtractor
    │   └── FourMemeExtractor
    │
    └── SolanaDexExtractor (for Solana)
        ├── PumpFunExtractor
        └── PumpSwapExtractor

SuiDexExtractor (for Sui chain)
    ├── BluefinExtractor
    └── CetusExtractor
```

## Base Classes

### 1. BaseDexExtractor
Provides common functionality for all DEX extractors:
- Protocol and chain type tracking
- Quote asset management (stablecoin rankings)
- Logger setup
- Thread-safe access to configuration

**Key Methods:**
```go
GetSupportedProtocols() []string
GetSupportedChains() []types.ChainType
SetQuoteAssets(assets map[string]int)
GetQuoteAssets() map[string]int
IsChainSupported(chainType types.ChainType) bool
```

### 2. EVMDexExtractor
Specializes BaseDexExtractor for EVM-compatible chains (Ethereum, BSC):
- Unified log extraction from various formats
- Log filtering by event signature
- EVM chain validation
- Price and value calculation helpers

**Key Methods:**
```go
ExtractEVMLogs(tx *types.UnifiedTransaction) []*ethtypes.Log
IsEVMChainSupported(chainType types.ChainType) bool
FilterLogsByTopics(logs []*ethtypes.Log, topicFilter map[string]bool) []*ethtypes.Log
```

### 3. SolanaDexExtractor
Specializes BaseDexExtractor for Solana:
- Base58 public key parsing
- Little-endian byte parsing (u64, i64, u128)
- Borsh string deserialization
- Event discriminator matching

**Key Methods:**
```go
ParseBase58Pubkey(key string) ([]byte, error)
ParseU64LE(data []byte) (uint64, error)
ParseI64LE(data []byte) (int64, error)
ExtractDiscriminator(eventData []byte) ([]byte, error)
MatchDiscriminator(actual, expected []byte) bool
```

## Shared Utilities (utils.go)

### EVM Log Extraction
- `extractEVMLogsFromTransaction()` - Centralized log extraction (shared by all EVM extractors)
- `parseEVMLogsFromInterface()` - Parse logs from JSON deserialization

### Integer Conversion
- `toSignedInt256()` - Convert bytes to signed big.Int (for V3 Swap amounts)
- `toSignedInt64()` - Convert bytes to signed int64

### Price & Value Calculation
- `CalcPrice()` - Calculate price ratio between tokens
- `CalcValue()` - Calculate total value (amount * price)
- `ConvertDecimals()` - Adjust for token decimals

### Address & Data Parsing
- `ParseHexAddress()`, `ParseHexHash()`, `ParseHexBytes()` - Parse hex strings
- `ValidateAddress()` - Validate Ethereum addresses
- `SafeStringConversion()`, `SafeUint256Conversion()` - Safe type conversions

## Cache System (cache.go)

### CacheManager[T]
Generic, thread-safe cache with TTL support:
```go
cache := NewCacheManager[model.Token](1 * time.Hour)
cache.Set("0xtoken", token)
if token, ok := cache.Get("0xtoken"); ok {
    // Use token
}
cache.Cleanup()  // Remove expired entries
```

### Specialized Caches
- `TokenCache` - Cache for token metadata
- `PoolObjectCache` - Cache for Sui pool objects
- `BatchTokenCache` - Manage multiple token caches

## Creating a New DEX Extractor

### For EVM-Based DEX (e.g., new DEX on BSC)

1. **Create the extractor file:**
```go
package dex

import (
	"context"
	"unified-tx-parser/internal/model"
	"unified-tx-parser/internal/types"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

type NewDEXExtractor struct {
	*EVMDexExtractor
	// Protocol-specific fields
	factoryAddr string
	eventSigs   map[string]string  // eventName -> signature
}

func NewNewDEXExtractor() *NewDEXExtractor {
	cfg := &BaseDexExtractorConfig{
		Protocols:       []string{"newdex"},
		SupportedChains: []types.ChainType{types.ChainTypeBSC},
		EnableTokenCache: true,
		TokenCacheTTL:   1 * time.Hour,
	}
	return &NewDEXExtractor{
		EVMDexExtractor: NewEVMDexExtractor(cfg),
		factoryAddr:     "0x...",
		eventSigs: map[string]string{
			"swap": "0x...",
		},
	}
}
```

2. **Implement the DexExtractors interface:**
```go
func (e *NewDEXExtractor) ExtractDexData(ctx context.Context, blocks []types.UnifiedBlock) (*types.DexData, error) {
	dexData := &types.DexData{
		Pools:        make([]model.Pool, 0),
		Transactions: make([]model.Transaction, 0),
		Liquidities:  make([]model.Liquidity, 0),
		Reserves:     make([]model.Reserve, 0),
		Tokens:       make([]model.Token, 0),
	}

	for _, block := range blocks {
		if !e.IsChainSupported(block.ChainType) {
			continue
		}

		for _, tx := range block.Transactions {
			// Use inherited method from EVMDexExtractor
			logs := e.ExtractEVMLogs(&tx)
			if len(logs) == 0 {
				continue
			}

			// Filter logs by event signature
			sigFilter := map[string]bool{
				e.eventSigs["swap"]: true,
			}
			relevantLogs := e.FilterLogsByTopics(logs, sigFilter)

			// Parse each log
			for _, log := range relevantLogs {
				if swap := e.parseSwap(log); swap != nil {
					dexData.Transactions = append(dexData.Transactions, *swap)
				}
			}
		}
	}

	return dexData, nil
}

func (e *NewDEXExtractor) SupportsBlock(block *types.UnifiedBlock) bool {
	if !e.IsChainSupported(block.ChainType) {
		return false
	}
	for _, tx := range block.Transactions {
		logs := e.ExtractEVMLogs(&tx)
		for _, log := range logs {
			if e.isNewDEXLog(log) {
				return true
			}
		}
	}
	return false
}

func (e *NewDEXExtractor) parseSwap(log *ethtypes.Log) *model.Transaction {
	// Parse log.Data and log.Topics to extract swap details
	// Use utilities like toSignedInt256(), CalcPrice(), ConvertDecimals()
	// ...
	return &model.Transaction{
		// Set fields
	}
}
```

3. **Register in extractor_factory.go:**
```go
factory.RegisterExtractor("newdex", NewNewDEXExtractor())
```

### For Solana-Based DEX

1. **Extend SolanaDexExtractor:**
```go
type PumpFunExtractor struct {
	*SolanaDexExtractor
	programID string
	// Other fields
}

func NewPumpFunExtractor() *PumpFunExtractor {
	cfg := &BaseDexExtractorConfig{
		Protocols:       []string{"pumpfun"},
		SupportedChains: []types.ChainType{types.ChainTypeSolana},
	}
	return &PumpFunExtractor{
		SolanaDexExtractor: NewSolanaDexExtractor(cfg),
		programID:         "...",
	}
}
```

2. **Use inherited parsing methods:**
```go
discriminator := e.ExtractDiscriminator(eventData)
if e.MatchDiscriminator(discriminator, expectedDiscriminator) {
	amount, _ := ParseU64LE(eventData[8:16])
	// Process event
}
```

## Design Principles

1. **DRY (Don't Repeat Yourself)**: Shared logic goes in base classes and utilities
2. **Composition over Inheritance**: Use base classes as mixins, not deep hierarchies
3. **Interface Compliance**: All extractors must implement `types.DexExtractors` interface
4. **Thread Safety**: Use sync.RWMutex for concurrent access
5. **Error Handling**: Log errors but don't panic; return gracefully
6. **Caching**: Enable caching for expensive lookups (token metadata, pool data)

## Testing New Extractors

```go
func TestNewDEXExtractor_ParseSwap(t *testing.T) {
	extractor := NewNewDEXExtractor()

	// Create test block with mock transaction
	block := &types.UnifiedBlock{
		ChainType: types.ChainTypeBSC,
		Transactions: []types.UnifiedTransaction{
			// Mock transaction with test logs
		},
	}

	data, err := extractor.ExtractDexData(context.Background(), []types.UnifiedBlock{*block})
	require.NoError(t, err)
	require.Len(t, data.Transactions, 1)
	require.Equal(t, "swap", data.Transactions[0].Type)
}
```

## Migration Guide: Converting Old Extractors

### Before (Duplicated Code)
```go
// In PancakeSwap and Uniswap extractors - DUPLICATED
func extractEVMLogsFromTransaction(tx *types.UnifiedTransaction) []*ethtypes.Log {
	// Same implementation copied in both files
}
```

### After (Using Base Class)
```go
// Create EVMDexExtractor and use inherited method
type UniswapExtractor struct {
	*EVMDexExtractor
	// Protocol-specific fields
}

func (u *UniswapExtractor) ExtractDexData(ctx context.Context, blocks []types.UnifiedBlock) (*types.DexData, error) {
	// Use u.ExtractEVMLogs() instead of local function
	logs := u.ExtractEVMLogs(&tx)
}
```

## Performance Considerations

1. **Log Extraction**: EVMDexExtractor.ExtractEVMLogs() handles multiple formats efficiently
2. **Caching**: Enable token/pool caching to avoid redundant lookups
3. **Batch Processing**: Process logs in batches within a transaction
4. **Cleanup**: Call cache.Cleanup() periodically to remove expired entries

## Solana Processor -> Extractor Data Flow

The Solana Processor builds a `map[string]any` RawData for each transaction. The Solana Extractor consumes it as follows:

```
SolanaProcessor
  │
  ├── getBlockWithRetry(slot)          // Fetch block with V0 tx support + retry
  ├── convertBlockTransactions(block)  // Filter failed txs, build UnifiedTransaction
  │     └── buildRawData(txWithMeta)   // Assemble RawData map
  │
  ▼
UnifiedTransaction.RawData (map[string]any)
  │
  ├── logMessages       → ExtractSolanaEventData() parses "Program data:" lines,
  │                       base64-decodes them to get raw event bytes
  ├── accountKeys       → Available for instruction-level account resolution
  ├── innerInstructions → Available for CPI (Cross-Program Invocation) analysis
  ├── preTokenBalances  → Available for token balance diff calculation
  ├── postTokenBalances → Available for token balance diff calculation
  └── meta              → Fee and error info
  │
  ▼
SolanaDexExtractor (base)
  │  Provides: ExtractDiscriminator, MatchDiscriminator, ParseU64LE, ParsePubkey, etc.
  │
  ├── PumpFunExtractor   → Matches discriminators for Trade/Create/Complete events
  └── PumpSwapExtractor  → Matches discriminators for Buy/Sell/CreatePool/Deposit/Withdraw events
```

**Currently used by extractors**: `logMessages` (via `ExtractSolanaEventData()`).
Other fields (`accountKeys`, `preTokenBalances`, `postTokenBalances`, `innerInstructions`) are available in RawData for future extractors that need instruction-level or balance-diff parsing.

## Future Extensions

- Implement `TokenMetadataProvider` interface for dynamic token lookups
- Create `PriceOracleAdapter` for real-time price feeds
- Build `EventStreamProcessor` for continuous event processing
