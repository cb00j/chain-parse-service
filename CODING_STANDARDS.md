# Chain Parse Service - Coding Standards & Conventions

This document defines the coding standards, conventions, and best practices for the Chain Parse Service project.

## Table of Contents

1. [Go Code Style](#go-code-style)
2. [Error Handling](#error-handling)
3. [Logging](#logging)
4. [Testing](#testing)
5. [Git Commits](#git-commits)
6. [Adding New Chains/DEXs](#adding-new-chainsdexs)
7. [Code Review Checklist](#code-review-checklist)

---

## Go Code Style

### Naming Conventions

#### Functions and Methods
- Use **CamelCase** for exported functions (public)
- Use **camelCase** for unexported functions (private)
- Use descriptive names that indicate purpose

```go
// ✓ Good
func (e *UniswapExtractor) ExtractDexData(ctx context.Context, blocks []types.UnifiedBlock) (*types.DexData, error) {}
func (p *PancakeSwapExtractor) parseV2Swap(log *ethtypes.Log) *model.Transaction {}
func getEventType(topic string) string {}

// ✗ Bad
func (e *UniswapExtractor) extract(ctx context.Context, blocks []types.UnifiedBlock) (*types.DexData, error) {}
func (p *PancakeSwapExtractor) parse(log *ethtypes.Log) *model.Transaction {}
func getType(t string) string {}
```

#### Variables and Constants
- Use **camelCase** for variables
- Use **UPPER_SNAKE_CASE** for package-level constants
- Use descriptive names; avoid single-letter variables except for indexes

```go
// ✓ Good
const (
    PANCAKE_SWAP_V2_FACTORY_ADDR = "0xcA143Ce32Fe78f1f7019d7d551a6402fC5350c73"
    SWAP_EVENT_SIGNATURE = "0xd78ad95fa46c994b6551d0da85fc275fe613ce37657fb8d5e3d130840159d822"
)

var supportedChains []types.ChainType

// ✗ Bad
const PANCAKE_V2 = "0xcA143Ce32Fe78f1f7019d7d551a6402fC5350c73"
var s []types.ChainType
for i, v := range items {} // OK for simple loops
```

#### Interfaces
- Use descriptive names ending with `-er` or `-or`

```go
// ✓ Good
type DexExtractors interface {}
type ChainProcessor interface {}
type StorageEngine interface {}

// ✗ Bad
type DEXer interface {}
type Processor interface {}
```

#### Package Names
- Use short, lowercase names
- Avoid underscores in package names
- Use plural for collection packages

```
internal/
├── parser/
│   ├── chains/    # chain processors
│   ├── dexs/      # dex extractors
│   └── engine/    # processing engine
├── storage/       # storage interfaces and implementations
├── types/         # type definitions
├── errors/        # error handling
└── logger/        # logging utilities
```

### Import Organization

Organize imports in three groups, separated by blank lines:

```go
package dex

import (
	// Standard library
	"context"
	"fmt"
	"math/big"
	"strings"

	// External dependencies
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sirupsen/logrus"

	// Local imports
	"unified-tx-parser/internal/model"
	"unified-tx-parser/internal/types"
	"unified-tx-parser/internal/utils"
)
```

**Rules:**
1. Standard library imports first
2. External dependencies second
3. Local project imports last
4. Alphabetically sorted within each group
5. Use meaningful aliases for long import paths

### Code Organization

#### File Structure
- One struct per file (unless they're tightly related)
- Group related methods together
- Place interface implementation at the top of the file

```go
// pancakeswap.go
package dex

type PancakeSwapExtractor struct { ... }

// Interface Implementation
func (p *PancakeSwapExtractor) GetSupportedProtocols() []string { ... }
func (p *PancakeSwapExtractor) GetSupportedChains() []types.ChainType { ... }
func (p *PancakeSwapExtractor) ExtractDexData(ctx context.Context, blocks []types.UnifiedBlock) (*types.DexData, error) { ... }
func (p *PancakeSwapExtractor) SupportsBlock(block *types.UnifiedBlock) bool { ... }

// Private Methods
func (p *PancakeSwapExtractor) parseV2Swap(log *ethtypes.Log) *model.Transaction { ... }
func (p *PancakeSwapExtractor) parseV3Swap(log *ethtypes.Log) *model.Transaction { ... }
```

#### Struct Fields
- Public fields at the top
- Private fields at the bottom
- Group related fields together
- Use meaningful field names

```go
// ✓ Good
type UniswapExtractor struct {
	// Interface implementation fields
	supportedChains []types.ChainType
	protocols       []string

	// Configuration
	factoryAddr string
	routerAddr  string
	eventSigs   map[string]string

	// Runtime state
	log     *logrus.Entry
	cache   *TokenCache
	mutex   sync.RWMutex
}

// ✗ Bad
type UniswapExtractor struct {
	fc string          // cryptic abbreviation
	ra string          // unclear what this is
	evts map[string]string // non-standard naming
	logger *logrus.Entry
	c *TokenCache
	m sync.RWMutex
}
```

### Code Formatting

- Use `gofmt` for automatic formatting
- Line length: max 120 characters (readability over strict limits)
- Use blank lines to separate logical sections
- Use meaningful spacing around operators

```go
// ✓ Good
result := new(big.Int).Add(amount0, amount1)
price := new(big.Float).Quo(out, in).Float64()

if len(log.Data) < 128 {
	return nil
}

// ✗ Bad
result:=new(big.Int).Add(amount0,amount1)
price:=new(big.Float).Quo(out,in).Float64()

if len(log.Data)<128{return nil}
```

---

## Error Handling

### General Principles

1. **Always check error returns** - No unchecked errors
2. **Wrap errors with context** - Use `fmt.Errorf` with `%w` verb
3. **Return early** - Check errors immediately, don't nest
4. **Don't hide errors** - Log at appropriate level when returning errors
5. **Use custom error types** when needed for specific error handling

### Error Wrapping Pattern

```go
// ✓ Good - wrap with context
func (e *Extractor) GetTokenMetadata(ctx context.Context, addr string) (*model.Token, error) {
	token, err := e.fetchFromChain(addr)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch token metadata for %s: %w", addr, err)
	}
	return token, nil
}

// ✗ Bad - loses error context
func (e *Extractor) GetTokenMetadata(ctx context.Context, addr string) (*model.Token, error) {
	token, err := e.fetchFromChain(addr)
	if err != nil {
		return nil, err  // No context about what failed
	}
	return token, nil
}

// ✗ Bad - shadows error
func (e *Extractor) GetTokenMetadata(ctx context.Context, addr string) (*model.Token, error) {
	token, err := e.fetchFromChain(addr)
	if err != nil {
		e.log.Errorf("failed: %v", err)
		// error silently ignored, returns nil without error
		return nil, nil
	}
	return token, nil
}
```

### Panic vs Error

- **Use panic only** for truly exceptional conditions:
  - Programmer error during initialization
  - System configuration problems that prevent startup
  - Assertions about immutable invariants

- **Never panic in**:
  - Event processing logic
  - Network operations
  - User input handling

```go
// ✓ Good - panic during initialization for config issues
func NewDEXExtractor(cfg Config) *DEXExtractor {
	if cfg.FactoryAddr == "" {
		panic("factory address must be configured")
	}
	// ...
}

// ✓ Good - return error for operational failures
func (e *DEXExtractor) ExtractDexData(ctx context.Context, blocks []types.UnifiedBlock) (*types.DexData, error) {
	if blocks == nil {
		return nil, fmt.Errorf("blocks cannot be nil")
	}
	// ...
}

// ✗ Bad - panic in operational code
func (e *DEXExtractor) parseLog(log *ethtypes.Log) *model.Transaction {
	if log == nil {
		panic("log cannot be nil")  // Should return error instead
	}
	// ...
}
```

### Custom Error Types

Use custom error types for errors that need specific handling:

```go
// ✓ Good
type ParseError struct {
	LogIndex int
	Reason   string
}

func (e ParseError) Error() string {
	return fmt.Sprintf("parse error at log %d: %s", e.LogIndex, e.Reason)
}

// Usage
if err := parseLog(log); err != nil {
	if parseErr, ok := err.(ParseError); ok {
		e.log.Warnf("recoverable parse error: %v", parseErr)
		continue
	}
	return nil, err
}
```

---

## Logging

### Logger Setup

- Use structured logging with logrus
- Always include `service` and `module` fields
- Use appropriate log levels

```go
// Module logger
var extractorLog = logrus.WithFields(logrus.Fields{
	"service": "parser",
	"module":  "dex-extractor",
})

// In methods
func (e *Extractor) SomeMethod() {
	e.log.WithFields(logrus.Fields{
		"tx_hash": tx.TxHash,
		"block":   block.Number.String(),
	}).Infof("processing transaction")
}
```

### Log Levels

- **Debug**: Detailed diagnostic information (variable values, intermediate steps)
- **Info**: General informational messages (transaction processed, block synced)
- **Warn**: Warning conditions (parse errors, retries, unexpected values)
- **Error**: Error conditions (failed operations, should be investigated)

```go
// ✓ Good
e.log.Debugf("parsed amount: %s, price: %.6f", amount.String(), price)
e.log.Infof("processed %d transactions in block %d", len(txs), blockNum)
e.log.Warnf("swap log data too short: %d bytes, expected >=128", len(log.Data))
e.log.Errorf("failed to fetch token metadata: %w", err)

// ✗ Bad
e.log.Infof("amount: %s, price: %.6f, data: %v", amount, price, someData)  // Too verbose for Info
e.log.Warnf("retry attempt %d", attempt)  // Should be Debug
e.log.Errorf("chunk size exceeded")  // Too vague
```

### Structured Logging

Add relevant context to every log:

```go
// ✓ Good
e.log.WithFields(logrus.Fields{
	"tx_hash":     tx.TxHash,
	"block":       block.BlockNumber.String(),
	"extractor":   "pancakeswap",
	"event_type":  "swap",
	"log_index":   logIdx,
	"swap_index":  swapIdx,
}).Infof("processing swap event")

// ✗ Bad
e.log.Infof("processing transaction")  // No context
```

### Production Logging

In production, avoid excessive logging:

```go
// ✓ Good - only log significant events
func (e *Extractor) ExtractDexData(ctx context.Context, blocks []types.UnifiedBlock) (*types.DexData, error) {
	for _, block := range blocks {
		for _, tx := range block.Transactions {
			ethLogs := e.ExtractEVMLogs(&tx)
			// Don't log every transaction, only on errors or significant events
			if len(ethLogs) > 0 {
				e.log.Debugf("found %d eth logs in tx %s", len(ethLogs), tx.TxHash)
			}
		}
	}
}

// ✗ Bad - too many logs
func (e *Extractor) ExtractDexData(ctx context.Context, blocks []types.UnifiedBlock) (*types.DexData, error) {
	for _, block := range blocks {
		e.log.Infof("processing block %d", block.BlockNumber.Int64())
		for i, tx := range block.Transactions {
			e.log.Infof("transaction %d/%d (hash: %s)", i+1, len(block.Transactions), tx.TxHash)
			ethLogs := e.ExtractEVMLogs(&tx)
			for _, log := range ethLogs {
				e.log.Infof("found log at index %d", log.Index)
			}
		}
	}
}
```

---

## Testing

### Test File Organization

- Place tests in `*_test.go` files in the same package
- One test file per source file or logical group
- Use table-driven tests for multiple scenarios

```
pancakeswap.go        → pancakeswap_test.go
uniswap.go            → uniswap_test.go
utils.go              → utils_test.go
```

### Test Function Naming

```go
// ✓ Good
func TestPancakeSwapExtractor_ExtractDexData(t *testing.T) {}
func TestPancakeSwapExtractor_ParseV2Swap(t *testing.T) {}
func TestCalcPrice_WithValidInputs(t *testing.T) {}
func TestCalcPrice_WithZeroAmount(t *testing.T) {}

// ✗ Bad
func TestPancakeSwap(t *testing.T) {}  // Too vague
func TestExtract(t *testing.T) {}       // Unclear what is being tested
```

### Table-Driven Tests

Use table-driven tests for multiple input scenarios:

```go
// ✓ Good
func TestCalcPrice(t *testing.T) {
	tests := []struct {
		name        string
		amountIn    *big.Int
		amountOut   *big.Int
		expected    float64
		shouldError bool
	}{
		{
			name:        "normal calculation",
			amountIn:    big.NewInt(1000000),
			amountOut:   big.NewInt(2000000),
			expected:    2.0,
			shouldError: false,
		},
		{
			name:        "zero input",
			amountIn:    big.NewInt(0),
			amountOut:   big.NewInt(1000000),
			expected:    0,
			shouldError: false,
		},
		{
			name:        "nil inputs",
			amountIn:    nil,
			amountOut:   nil,
			expected:    0,
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalcPrice(tt.amountIn, tt.amountOut)
			if result != tt.expected {
				t.Errorf("expected %f, got %f", tt.expected, result)
			}
		})
	}
}
```

### Mock Objects

Use interface-based mocking for external dependencies:

```go
// ✓ Good
type MockTokenProvider interface {
	GetToken(ctx context.Context, addr string) (*model.Token, error)
}

type mockTokenProvider struct {
	tokens map[string]*model.Token
}

func (m *mockTokenProvider) GetToken(ctx context.Context, addr string) (*model.Token, error) {
	if token, ok := m.tokens[addr]; ok {
		return token, nil
	}
	return nil, fmt.Errorf("token not found")
}

// Usage in test
func TestExtractor_WithMockProvider(t *testing.T) {
	mockProvider := &mockTokenProvider{
		tokens: map[string]*model.Token{
			"0xtoken": {Name: "Test Token", Symbol: "TEST"},
		},
	}
	// Test with mock
}
```

### Test Assertions

Use clear assertion messages:

```go
// ✓ Good
if len(data.Transactions) != 1 {
	t.Errorf("expected 1 transaction, got %d", len(data.Transactions))
}

if data.Transactions[0].Price != expectedPrice {
	t.Errorf("expected price %.6f, got %.6f", expectedPrice, data.Transactions[0].Price)
}

// Consider using third-party assertion libraries
require.NoError(t, err)
require.Equal(t, expectedValue, actualValue)
assert.True(t, condition, "condition should be true")
```

### Test Isolation

Each test should be independent:

```go
// ✓ Good
func TestExtractor_CacheBehavior(t *testing.T) {
	// Create fresh extractor for this test
	extractor := NewDexExtractorWithCache()
	defer extractor.cache.Clear()

	// Test cache operations
}

// ✗ Bad - tests depend on execution order
var globalCache = NewTokenCache(time.Hour)

func TestCacheSet(t *testing.T) {
	globalCache.Set("key", token)
}

func TestCacheGet(t *testing.T) {
	// Depends on TestCacheSet running first!
	token, ok := globalCache.Get("key")
}
```

---

## Git Commits

### Commit Message Format

Follow conventional commits format:

```
<type>(<scope>): <subject>

<body>

<footer>
```

### Type

- **feat**: New feature
- **fix**: Bug fix
- **refactor**: Code refactoring without feature changes
- **test**: Test additions or modifications
- **docs**: Documentation changes
- **chore**: Build, CI, or dependency changes
- **perf**: Performance improvements
- **style**: Formatting, missing semicolons, etc.

### Scope

Specify the area of change:
- Package name: `dex`, `parser`, `storage`
- Feature: `pancakeswap`, `uniswap`, `cache`
- Component: `base-extractor`, `evm-logs`

### Subject

- Use imperative mood: "add", "fix", "implement" (not "added", "fixed", "implementing")
- Don't capitalize first letter
- No period at the end
- Max 50 characters

### Body

- Explain **what** and **why**, not **how**
- Wrap at 72 characters
- Separate from subject with blank line
- Use bullet points for multiple changes

### Examples

```
✓ Good:
feat(dex): add FourMeme DEX support with V1/V2 event parsing
- Implement V2 TokenCreate, TokenPurchase, TokenSale events
- Support V1 event format with 128-byte layout
- Add LiquidityAdded event for graduation tracking
- Include quote asset configuration for price calculation

fix(parser): correct Uniswap V3 swap amount handling
- Use toSignedInt256 for signed amount0/amount1 parsing
- Previous code treated negative amounts as large positives
- Fixes price inversion on sell-side swaps

✗ Bad:
feat: add stuff
Fixed uniswap parsing
MAJOR CHANGES
Update code
```

### Commit Guidelines

1. **Atomic commits**: Each commit should be logically independent
2. **No mixing concerns**: Don't refactor and add features in the same commit
3. **Include related tests**: If adding a feature, include tests
4. **Reference issues**: Add `Fixes #123` or `Closes #456` when applicable

```
fix(uniswap): handle pool address parsing in PoolCreated event

Previous implementation used log.Address (Factory) instead of
parsing actual pool address from event data, causing all pools
to be attributed to the factory.

Fixes #42
```

---

## Adding New Chains/DEXs

### Checklist for Adding a New DEX

#### 1. Protocol Analysis
- [ ] Document protocol addresses (factory, router, etc.)
- [ ] Identify all event types to parse (Swap, Mint, Burn, PoolCreated, etc.)
- [ ] Document event signatures and parameter layouts
- [ ] Understand token pair representation (token0/token1 order)
- [ ] Identify any special handling (V2 vs V3, signed vs unsigned)

#### 2. Implementation
- [ ] Create `{protocol}.go` extractor file
- [ ] Define extractor struct extending appropriate base class
- [ ] Implement `DexExtractors` interface:
  - [ ] `GetSupportedProtocols()` - Return protocol names
  - [ ] `GetSupportedChains()` - Return supported chains
  - [ ] `ExtractDexData()` - Main extraction logic
  - [ ] `SupportsBlock()` - Check if block contains protocol events
- [ ] Implement event parsing methods
- [ ] Register in `extractor_factory.go`

#### 3. Testing
- [ ] Create `{protocol}_test.go` test file
- [ ] Add unit tests for each event type:
  - [ ] Normal case with valid data
  - [ ] Edge cases (zero amounts, max values)
  - [ ] Error cases (malformed data, truncated logs)
- [ ] Add integration test with real block data
- [ ] Test suite passes with `go test -v -race ./...`

#### 4. Documentation
- [ ] Add protocol-specific docs to `ARCHITECTURE.md`
- [ ] Document event signatures and layouts
- [ ] Add code comments for non-obvious logic
- [ ] Include examples in commit message

#### 5. Code Review
- [ ] Pass linter: `go vet ./...`
- [ ] Code follows project conventions
- [ ] All tests pass
- [ ] No unnecessary dependencies added
- [ ] Error handling is complete

### Step-by-Step Example: Adding SushiSwap

#### Step 1: Protocol Analysis
```go
// Analyze and document protocol
const (
	// SushiSwap AMM router
	SUSHISWAP_ROUTER_ADDR = "0xd9e1cE17f2641f24aE5D4d5a9f2779199aa8aBEA"

	// Event signatures
	SUSHISWAP_SWAP_EVENT_SIG = "0xd78ad95fa46c994b6551d0da85fc275fe613ce37657fb8d5e3d130840159d822"
	// ... other events
)
```

#### Step 2: Create Extractor
```go
// internal/parser/dexs/sushiswap.go
type SushiSwapExtractor struct {
	*EVMDexExtractor
	routerAddr string
}

func NewSushiSwapExtractor() *SushiSwapExtractor {
	cfg := &BaseDexExtractorConfig{
		Protocols:       []string{"sushiswap"},
		SupportedChains: []types.ChainType{types.ChainTypeEthereum, types.ChainTypeBSC},
		LoggerModuleName: "dex-sushiswap",
	}

	return &SushiSwapExtractor{
		EVMDexExtractor: NewEVMDexExtractor(cfg),
		routerAddr:      SUSHISWAP_ROUTER_ADDR,
	}
}

// Implement interface methods...
```

#### Step 3: Write Tests
```go
// internal/parser/dexs/sushiswap_test.go
func TestSushiSwapExtractor_ParseSwap(t *testing.T) {
	tests := []struct {
		name string
		logData []byte
		expected *model.Transaction
	}{
		// Table-driven tests...
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test implementation...
		})
	}
}
```

#### Step 4: Register Extractor
```go
// internal/parser/dexs/extractor_factory.go
func CreateDefaultFactory() *ExtractorFactory {
	factory := NewExtractorFactory()
	// ... existing extractors ...
	factory.RegisterExtractor("sushiswap", NewSushiSwapExtractor())
	return factory
}
```

#### Step 5: Commit
```
feat(dex): add SushiSwap DEX support

- Implement Swap event parsing for both V1 and V2 protocols
- Add Mint/Burn liquidity event handling
- Support PoolCreated events for pool tracking
- Include quote asset management for stablecoin prices

Supports Ethereum and BSC chains.
```

---

## Code Review Checklist

### Before Submitting for Review

- [ ] Code compiles without warnings: `go build ./...`
- [ ] All tests pass: `go test -v ./...`
- [ ] No race conditions: `go test -race ./...`
- [ ] Code formatted: `go fmt ./...`
- [ ] Linter passes: `go vet ./...`
- [ ] Commit messages follow conventions
- [ ] No TODO/FIXME comments without context
- [ ] Related tests are included
- [ ] Error handling is complete
- [ ] No unnecessary dependencies added

### During Review

#### Functionality
- [ ] Does the code do what it claims to do?
- [ ] Are all edge cases handled?
- [ ] Are error cases handled appropriately?
- [ ] Is the logic correct?

#### Code Quality
- [ ] Is the code clear and understandable?
- [ ] Does it follow project conventions?
- [ ] Are variable names meaningful?
- [ ] Is the code DRY (not repeating)?
- [ ] Are there unnecessary comments?

#### Performance
- [ ] Are there any obvious performance issues?
- [ ] Is caching used appropriately?
- [ ] Are there unnecessary allocations?
- [ ] Is concurrency handled correctly?

#### Testing
- [ ] Are all critical paths tested?
- [ ] Are edge cases tested?
- [ ] Are tests isolated and independent?
- [ ] Do tests provide good coverage?

#### Documentation
- [ ] Are complex algorithms explained?
- [ ] Are public APIs documented?
- [ ] Are potential gotchas noted?
- [ ] Is the change reflected in relevant docs?

### Review Template

```markdown
## Summary
Brief description of changes

## Changes
- Change 1
- Change 2

## Testing
- [ ] Tested locally
- [ ] All tests pass
- [ ] No race conditions

## Notes
- Consider X for future improvement
- This approach was chosen because Y

## Sign-off
- [ ] Approved
- [ ] Needs changes
```

### Comments Style

```markdown
✓ Good:
Why should this be changed?
```
if err != nil {
    return fmt.Errorf("meaningful error context: %w", err)
}
```
This provides error context which helps with debugging.

✗ Bad:
"change this"
"add error handling"
"this is wrong"
```

---

## Project Structure Summary

```
chain-parse-service/
├── internal/
│   ├── errors/              # Error types and handling
│   ├── logger/              # Logging configuration
│   ├── model/               # Data models
│   ├── parser/
│   │   ├── chains/          # Chain processors (Sui, BSC, Ethereum, Solana)
│   │   ├── dexs/            # DEX extractors
│   │   │   ├── base_extractor.go
│   │   │   ├── evm_extractor.go
│   │   │   ├── solana_extractor.go
│   │   │   ├── utils.go
│   │   │   ├── cache.go
│   │   │   ├── pancakeswap.go
│   │   │   ├── uniswap.go
│   │   │   └── ...
│   │   └── engine/          # Processing engine
│   ├── storage/             # Storage layer
│   ├── types/               # Type definitions and interfaces
│   └── utils/               # General utilities
├── cmd/                     # Command-line applications
├── configs/                 # Configuration files
├── CODING_STANDARDS.md      # This file
└── Makefile
```

---

## Style Guide Quick Reference

| Category | Standard |
|----------|----------|
| **Function naming** | CamelCase (exported), camelCase (private) |
| **Constants** | UPPER_SNAKE_CASE |
| **Imports** | Grouped: stdlib, external, local |
| **Line length** | 120 chars max |
| **Error handling** | Always wrap with fmt.Errorf |
| **Logging level** | Debug < Info < Warn < Error |
| **Test naming** | TestType_Method_Scenario |
| **Commits** | type(scope): subject format |
| **Comments** | Explain why, not what |
| **Panic** | Only during init, not in operations |

---

## Additional Resources

- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- [Effective Go](https://golang.org/doc/effective_go)
- [Conventional Commits](https://www.conventionalcommits.org/)
- [Project ARCHITECTURE.md](./internal/parser/dexs/ARCHITECTURE.md)
- [Project IMPLEMENTATION_GUIDE.md](./internal/parser/dexs/IMPLEMENTATION_GUIDE.md)

---

**Last Updated**: 2026-03-05
**Version**: 1.0
