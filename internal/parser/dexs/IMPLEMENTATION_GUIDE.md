# DEX Extractor Implementation Guide

## Overview

This guide provides step-by-step instructions for implementing new DEX extractors using the base class architecture.

## Quick Start: Creating a New EVM DEX Extractor

### 1. Define the Extractor Struct

```go
package dex

import (
	"context"
	"time"
	"unified-tx-parser/internal/model"
	"unified-tx-parser/internal/types"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

type MyDEXExtractor struct {
	*EVMDexExtractor
	// Protocol-specific fields
	factoryAddr string
	routerAddr  string
	eventSigs   map[string]string  // Maps event names to signature hashes
}
```

### 2. Implement Constructor

```go
func NewMyDEXExtractor() *MyDEXExtractor {
	cfg := &BaseDexExtractorConfig{
		Protocols:       []string{"mydex"},
		SupportedChains: []types.ChainType{types.ChainTypeBSC},
		QuoteAssets:     make(map[string]int),
		EnableTokenCache: true,
		TokenCacheTTL:   1 * time.Hour,
		LoggerModuleName: "dex-mydex",
	}

	return &MyDEXExtractor{
		EVMDexExtractor: NewEVMDexExtractor(cfg),
		factoryAddr:     "0x...",
		routerAddr:      "0x...",
		eventSigs: map[string]string{
			"swap":        "0xd78ad95fa46c994b6551d0da85fc275fe613ce37657fb8d5e3d130840159d822",
			"mint":        "0x4c209b5fc8ad50758f13e2e1088ba56a560dff690a1c6fef26394f4c03821c4f",
			"burn":        "0xdccd412f0b1252819cb1fd330b93224ca42612892bb3f4f789976e6d81936496",
			"poolCreated": "0x783cca1c0412dd0d695e784568c96da2e9c22ff989357a2e8b1d9b2b4e6b7118",
		},
	}
}
```

### 3. Implement ExtractDexData

```go
func (m *MyDEXExtractor) ExtractDexData(ctx context.Context, blocks []types.UnifiedBlock) (*types.DexData, error) {
	dexData := &types.DexData{
		Pools:        make([]model.Pool, 0),
		Transactions: make([]model.Transaction, 0),
		Liquidities:  make([]model.Liquidity, 0),
		Reserves:     make([]model.Reserve, 0),
		Tokens:       make([]model.Token, 0),
	}

	for _, block := range blocks {
		if !m.IsChainSupported(block.ChainType) {
			continue
		}

		for _, tx := range block.Transactions {
			// Use inherited method to extract logs
			logs := m.ExtractEVMLogs(&tx)
			if len(logs) == 0 {
				continue
			}

			// Process each log
			swapIdx := int64(0)
			for logIdx, log := range logs {
				logType := m.getEventType(log)
				switch logType {
				case "swap":
					if swap := m.parseSwap(log, &tx, int64(logIdx), swapIdx); swap != nil {
						dexData.Transactions = append(dexData.Transactions, *swap)
						swapIdx++
					}
				case "mint":
					if liq := m.parseLiquidity(log, &tx, "add", int64(logIdx)); liq != nil {
						dexData.Liquidities = append(dexData.Liquidities, *liq)
					}
				case "burn":
					if liq := m.parseLiquidity(log, &tx, "remove", int64(logIdx)); liq != nil {
						dexData.Liquidities = append(dexData.Liquidities, *liq)
					}
				case "poolCreated":
					if pool := m.parsePoolCreated(log, &tx); pool != nil {
						dexData.Pools = append(dexData.Pools, *pool)
					}
				}
			}
		}
	}

	return dexData, nil
}
```

### 4. Implement SupportsBlock

```go
func (m *MyDEXExtractor) SupportsBlock(block *types.UnifiedBlock) bool {
	if !m.IsChainSupported(block.ChainType) {
		return false
	}

	for _, tx := range block.Transactions {
		logs := m.ExtractEVMLogs(&tx)
		for _, log := range logs {
			if m.isMyDEXLog(log) {
				return true
			}
		}
	}
	return false
}
```

### 5. Implement Helper Methods

```go
func (m *MyDEXExtractor) isMyDEXLog(log *ethtypes.Log) bool {
	if len(log.Topics) == 0 {
		return false
	}
	topic0 := log.Topics[0].Hex()
	for _, sig := range m.eventSigs {
		if topic0 == sig {
			return true
		}
	}
	return false
}

func (m *MyDEXExtractor) getEventType(log *ethtypes.Log) string {
	if len(log.Topics) == 0 {
		return ""
	}
	topic0 := log.Topics[0].Hex()
	for eventName, sig := range m.eventSigs {
		if topic0 == sig {
			return eventName
		}
	}
	return ""
}

func (m *MyDEXExtractor) parseSwap(log *ethtypes.Log, tx *types.UnifiedTransaction, logIdx, swapIdx int64) *model.Transaction {
	if len(log.Data) < 128 {
		m.GetLogger().Warnf("swap log data too short")
		return nil
	}

	// Parse log data
	amountIn := BytesToBigInt(log.Data[0:32])
	amountOut := BytesToBigInt(log.Data[32:64])

	price := CalcPrice(amountIn, amountOut)
	value := CalcValue(amountIn, price)

	return &model.Transaction{
		Addr:        log.Address.Hex(),
		Router:      m.routerAddr,
		Factory:     m.factoryAddr,
		Pool:        log.Address.Hex(),
		Hash:        tx.TxHash,
		From:        tx.FromAddress,
		Side:        "swap",
		Amount:      amountIn,
		Price:       price,
		Value:       value,
		Time:        uint64(tx.Timestamp.Unix()),
		EventIndex:  logIdx,
		TxIndex:     int64(tx.TxIndex),
		SwapIndex:   swapIdx,
		BlockNumber: GetBlockNumber(tx),
		Extra: &model.TransactionExtra{
			QuotePrice:    fmt.Sprintf("%.18f", price),
			Type:          "swap",
			TokenDecimals: 18,
		},
	}
}

func (m *MyDEXExtractor) parseLiquidity(log *ethtypes.Log, tx *types.UnifiedTransaction, side string, logIdx int64) *model.Liquidity {
	if len(log.Data) < 64 {
		return nil
	}

	amount0 := BytesToBigInt(log.Data[0:32])
	amount1 := BytesToBigInt(log.Data[32:64])
	totalAmount := new(big.Int).Add(amount0, amount1)

	return &model.Liquidity{
		Addr:        log.Address.Hex(),
		Router:      m.routerAddr,
		Factory:     m.factoryAddr,
		Pool:        log.Address.Hex(),
		Hash:        tx.TxHash,
		From:        tx.FromAddress,
		Side:        side,
		Amount:      totalAmount,
		Value:       ConvertDecimals(totalAmount, 18),
		Time:        uint64(tx.Timestamp.Unix()),
		Key:         fmt.Sprintf("%s_%s_%d", tx.TxHash, side, logIdx),
		Extra: &model.LiquidityExtra{
			Key:    fmt.Sprintf("%s_%s_%d", tx.TxHash, side, logIdx),
			Amounts: amount1,
			Values:  []float64{ConvertDecimals(amount0, 18), ConvertDecimals(amount1, 18)},
			Time:   uint64(tx.Timestamp.Unix()),
		},
	}
}

func (m *MyDEXExtractor) parsePoolCreated(log *ethtypes.Log, tx *types.UnifiedTransaction) *model.Pool {
	if len(log.Topics) < 3 || len(log.Data) < 32 {
		return nil
	}

	token0 := ParseHexAddress(log.Topics[1].Hex()).Hex()
	token1 := ParseHexAddress(log.Topics[2].Hex()).Hex()
	poolAddr := log.Address.Hex()

	return &model.Pool{
		Addr:     poolAddr,
		Factory:  m.factoryAddr,
		Protocol: "mydex",
		Tokens:   map[int]string{0: token0, 1: token1},
		Fee:      2500,
		Extra: &model.PoolExtra{
			Hash: tx.TxHash,
			From: tx.FromAddress,
			Time: uint64(tx.Timestamp.Unix()),
		},
	}
}
```

### 6. Register in extractor_factory.go

Add to `CreateDefaultFactory()`:
```go
factory.RegisterExtractor("mydex", NewMyDEXExtractor())
```

Add to `CreateFactoryWithConfig()`:
```go
if _, enabled := config["mydex"]; enabled {
	factory.RegisterExtractor("mydex", NewMyDEXExtractor())
}
```

## Key Utilities Available

### From utils.go:

**Log Extraction:**
- `extractEVMLogsFromTransaction(tx)` - Extract logs from various formats

**Integer Conversion:**
- `toSignedInt256(b)` - Convert bytes to signed big.Int (for V3 swaps)
- `toSignedInt64(b)` - Convert to signed int64
- `BytesToBigInt(b)` - Convert bytes to unsigned big.Int
- `BigIntToBytes(i)` - Convert big.Int to 32-byte array

**Price & Value:**
- `CalcPrice(amountIn, amountOut)` - Calculate token price ratio
- `CalcValue(amount, price)` - Calculate total value
- `CalcV3Price(sqrtPriceX96)` - Calculate price from sqrt format
- `ConvertDecimals(amount, decimals)` - Adjust for token decimals

**Address & Data:**
- `ParseHexAddress(hexStr)` - Parse hex string to address
- `ParseHexHash(hexStr)` - Parse hex string to hash
- `ValidateAddress(addr)` - Validate Ethereum address format
- `GetBlockNumber(tx)` - Safely get block number from transaction

### From EVMDexExtractor:

- `ExtractEVMLogs(tx)` - Extract logs with inherited method
- `FilterLogsByTopics(logs, topicFilter)` - Filter logs by event signature
- `IsEVMChainSupported(chainType)` - Check if chain is supported
- `IsChainSupported(chainType)` - Check if chain is supported (from base)
- `GetLogger()` - Get logger instance (from base)
- `GetQuoteAssets()` - Get quote asset configuration (from base)

## Common Patterns

### Handling Multiple Event Versions (V2 vs V3)

```go
func (e *MyDEXExtractor) parseSwap(log *ethtypes.Log, tx *types.UnifiedTransaction, logIdx, swapIdx int64) *model.Transaction {
	topic0 := log.Topics[0].Hex()

	switch topic0 {
	case e.eventSigs["swapV2"]:
		return e.parseSwapV2(log, tx, logIdx, swapIdx)
	case e.eventSigs["swapV3"]:
		return e.parseSwapV3(log, tx, logIdx, swapIdx)
	}
	return nil
}
```

### Using Quote Assets for Price Calculation

```go
func (e *MyDEXExtractor) isStablecoin(tokenAddr string) bool {
	rank := e.GetQuoteAssetRank(tokenAddr)
	return rank >= 90  // Customize threshold
}

func (e *MyDEXExtractor) getQuotePrice(tokenAddr string) float64 {
	if e.isStablecoin(tokenAddr) {
		return 1.0
	}
	// Calculate price from reserves
	return 0.0
}
```

### Implementing Token Caching

```go
// For extractors that implement caching:
type MyDEXExtractorWithCache struct {
	*EVMDexExtractor
	tokenCache *TokenCache
}

func NewMyDEXExtractorWithCache() *MyDEXExtractorWithCache {
	cfg := &BaseDexExtractorConfig{
		// ...
		EnableTokenCache: true,
		TokenCacheTTL:   1 * time.Hour,
	}

	return &MyDEXExtractorWithCache{
		EVMDexExtractor: NewEVMDexExtractor(cfg),
		tokenCache:      NewTokenCache(1 * time.Hour),
	}
}

func (m *MyDEXExtractorWithCache) getTokenMetadata(addr string) (*model.Token, error) {
	// Check cache first
	if cached, ok := m.tokenCache.Get(addr); ok {
		return &cached, nil
	}

	// Fetch from chain
	token, err := m.fetchTokenFromChain(addr)
	if err != nil {
		return nil, err
	}

	// Store in cache
	m.tokenCache.Set(addr, *token)
	return token, nil
}
```

## Testing Your Extractor

```go
func TestMyDEXExtractor_ExtractDexData(t *testing.T) {
	extractor := NewMyDEXExtractor()

	// Create test block
	block := &types.UnifiedBlock{
		ChainType: types.ChainTypeBSC,
		BlockNumber: big.NewInt(12345),
		Transactions: []types.UnifiedTransaction{
			// Add mock transaction with test logs
		},
	}

	data, err := extractor.ExtractDexData(context.Background(), []types.UnifiedBlock{*block})
	require.NoError(t, err)
	require.NotNil(t, data)
	require.Greater(t, len(data.Transactions), 0)
}
```

## Debugging Tips

1. **Enable debug logging:**
   ```go
   logger := extractor.GetLogger()
   logger.Debugf("Processing log: %v", log.Topics[0])
   ```

2. **Validate log data length:**
   ```go
   if len(log.Data) < expectedLength {
       extractor.GetLogger().Warnf("log data too short: %d bytes", len(log.Data))
       return nil
   }
   ```

3. **Print parsed values:**
   ```go
   amount := BytesToBigInt(log.Data[0:32])
   extractor.GetLogger().Debugf("parsed amount: %s", StringifyBigInt(amount))
   ```

## Common Mistakes to Avoid

1. **Forgetting to check log length** - Always validate before accessing log.Data
2. **Off-by-one errors** - Be careful with byte slice indices
3. **Not handling nil values** - Check for nil big.Int before operations
4. **Wrong chain type** - Verify IsChainSupported before processing
5. **Skipping event index** - Increment swapIdx for each swap within a transaction
