package eth

import (
	"context"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"

	"unified-tx-parser/internal/dexcache"
	"unified-tx-parser/internal/model"
	dex "unified-tx-parser/internal/parser/dexs"
	"unified-tx-parser/internal/types"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/redis/go-redis/v9"
)

const (
	// Uniswap V2 contract addresses
	uniswapV2FactoryAddr = "0x5C69bEe701ef814a2B6a3EDD4B1652CB9cc5aA6f"

	// Uniswap V3 contract addresses
	uniswapV3FactoryAddr = "0x1F98431c8aD98523631AE4a59f267346ea31F984"

	// Event signatures (shared with PancakeSwap)
	swapV2EventSig      = "0xd78ad95fa46c994b6551d0da85fc275fe613ce37657fb8d5e3d130840159d822"
	swapV3EventSig      = "0xc42079f94a6350d7e6235f29174924f928cc2ac818eb64fed8004e115fbcca67"
	mintV2EventSig      = "0x4c209b5fc8ad50758f13e2e1088ba56a560dff690a1c6fef26394f4c03821c4f"
	burnV2EventSig      = "0xdccd412f0b1252819cb1fd330b93224ca42612892bb3f4f789976e6d81936496"
	mintV3EventSig      = "0x7a53080ba414158be7ec69b987b5fb7d07dee101fe85488f0853ae16239d0bde"
	burnV3EventSig      = "0x0c396cd989a39f4459b5fa1aed6a9a8dcdbc45908acfd67e028cd568da98982c"
	syncV2EventSig      = "0x1c411e9a96e071241c2f21f7726b17ae89e3cab4c78be50e062b03a9fffbbad1"
	pairCreatedEventSig = "0x0d3648bd0f6ba80134a33ba9275ac585d9d315f0ad8355cddefde31afa28d0e9"
	poolCreatedEventSig = "0x783cca1c0412dd0d695e784568c96da2e9c22ff989357a2e8b1d9b2b4e6b7118"
)

// UniswapExtractor parses Uniswap V2/V3 DEX events on Ethereum and BSC.
type UniswapExtractor struct {
	*dex.EVMDexExtractor
	// seenPools 缓存已知池子的 token 地址信息,key=pool地址,value=poolMeta。
	// 由 PairCreated/PoolCreated 事件填充,供 swap 解析时查 decimals 用。
	// 使用 sync.Map 保证并发安全。
	seenPools sync.Map // map[string]*poolMeta
	// ethClient 由引擎在注册阶段通过 SetEVMProcessor 注入,用于 eth_call 查询 token 元数据。
	// nil 表示尚未注入(降级为不归一化)
	ethClient *ethclient.Client
	// tokenCache 缓存已查询过的 token 元数据(addr -> model.Token)。
	// token 元数据(decimals/symbol/name)不会变,所以 TTL 设为 30 天近似永不过期。
	// 使用 CacheManager 保证并发安全。
	tokenCache *dex.CacheManager[model.Token]
	// redisClient 由引擎在注册阶段通过 SetTokenCacheRedis 注入,作为 tokenCache
	// 和 dex_tokens 表之间的跨进程共享层。nil 表示降级为仅用本地内存缓存
	// (单进程内有效,跨进程/跨重启不共享)。
	redisClient *redis.Client
}

// poolMeta holds the token addresses known for a pool, populated by PairCreated/PoolCreated events.
type poolMeta struct {
	token0 string
	token1 string
}

// ethereumQuoteAssets defines the quote asset ranking for Ethereum mainnet.
// Higher rank = more preferred as the price denominator.
// rank >= 90 = USD stablecoin; rank < 90 = non-stable quote (e.g. WETH).
// Addresses are lowercased; comparison is case-insensitive via strings.ToLower.
var ethereumQuoteAssets = map[string]int{
	// USD stablecoins
	strings.ToLower("0xdAC17F958D2ee523a2206206994597C13D831ec7"): 100, // USDT
	strings.ToLower("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"): 100, // USDC
	strings.ToLower("0x6B175474E89094C44Da98b954EedeAC495271d0F"): 95,  // DAI
	strings.ToLower("0x4Fabb145d64652a948d72533023f6E7A623C7C53"): 90,  // BUSD
	// Non-stable quotes
	strings.ToLower("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"): 80, // WETH
	strings.ToLower("0x2260FAC5E5542a773Aa44fBCfeDf7C193bc2C599"): 75, // WBTC
}

// NewUniswapExtractor creates a Uniswap extractor with EVM base class.
func NewUniswapExtractor() *UniswapExtractor {
	cfg := &dex.BaseDexExtractorConfig{
		Protocols:        []string{"uniswap", "uniswap-v2", "uniswap-v3"},
		SupportedChains:  []types.ChainType{types.ChainTypeEthereum},
		LoggerModuleName: "dex-uniswap",
		QuoteAssets:      ethereumQuoteAssets,
	}
	return &UniswapExtractor{
		EVMDexExtractor: dex.NewEVMDexExtractor(cfg),
		tokenCache:      dex.NewCacheManager[model.Token](30 * 24 * time.Hour),
	}
}

func (u *UniswapExtractor) ExtractDexData(ctx context.Context, blocks []types.UnifiedBlock) (*types.DexData, error) {
	dexData := &types.DexData{
		Pools:        make([]model.Pool, 0),
		Transactions: make([]model.Transaction, 0),
		Liquidities:  make([]model.Liquidity, 0),
		Reserves:     make([]model.Reserve, 0),
		Tokens:       make([]model.Token, 0),
	}

	for _, block := range blocks {
		if !u.IsChainSupported(block.ChainType) {
			continue
		}

		u.GetLogger().Debugf("processing block %s with %d transactions", block.BlockNumber.String(), len(block.Transactions))

		for _, tx := range block.Transactions {
			// FIX #4: Use shared ExtractEVMLogsFromTransaction instead of duplicate code
			ethLogs := dex.ExtractEVMLogsFromTransaction(&tx)
			if len(ethLogs) == 0 {
				continue
			}

			// FIX #2: Track swapIdx per transaction, pass logIdx as eventIndex
			swapIdx := int64(0)
			for _, log := range ethLogs {
				if !u.isUniswapLog(log) {
					continue
				}

				logType := u.getLogType(log)
				eventIndex := dex.ExtractEventIndex(log)
				u.GetLogger().Debugf("found uniswap log, type: %s, address: %s", logType, log.Address.Hex())

				switch logType {
				case "swap_v2":
					if modelTx := u.parseV2Swap(ctx, log, &tx, eventIndex, swapIdx); modelTx != nil {
						dexData.Transactions = append(dexData.Transactions, *modelTx)
						swapIdx++
					}
					if pool := u.lazyPool(log.Address.Hex(), uniswapV2FactoryAddr, "uniswap_v2", &tx); pool != nil {
						dexData.Pools = append(dexData.Pools, *pool)
					}
				case "swap_v3":
					if modelTx := u.parseV3Swap(ctx, log, &tx, eventIndex, swapIdx); modelTx != nil {
						dexData.Transactions = append(dexData.Transactions, *modelTx)
						swapIdx++
					}
					if reserve := u.parseV3Reserve(ctx, log, &tx); reserve != nil {
						dexData.Reserves = append(dexData.Reserves, *reserve)
					}
					if pool := u.lazyPool(log.Address.Hex(), uniswapV3FactoryAddr, "uniswap_v3", &tx); pool != nil {
						dexData.Pools = append(dexData.Pools, *pool)
					}
				case "mint":
					if liq := u.parseLiquidity(log, &tx, "add", eventIndex); liq != nil {
						dexData.Liquidities = append(dexData.Liquidities, *liq)
					}
				case "burn":
					if liq := u.parseLiquidity(log, &tx, "remove", eventIndex); liq != nil {
						dexData.Liquidities = append(dexData.Liquidities, *liq)
					}
				case "sync":
					if reserve := u.parseSync(ctx, log, &tx); reserve != nil {
						dexData.Reserves = append(dexData.Reserves, *reserve)
					}
				case "pair_created":
					if pool := u.parseV2PairCreated(log, &tx); pool != nil {
						dexData.Pools = append(dexData.Pools, *pool)
						for _, tokenAddr := range pool.Tokens {
							if tokenAddr == "" {
								continue
							}
							if _, cached := u.tokenCache.Get(tokenAddr); !cached {
								token := u.fetchTokenMeta(ctx, tokenAddr)
								dexData.Tokens = append(dexData.Tokens, token)
							}
						}
					}
				case "pool_created":
					if pool := u.parseV3PoolCreated(log, &tx); pool != nil {
						dexData.Pools = append(dexData.Pools, *pool)
						for _, tokenAddr := range pool.Tokens {
							if tokenAddr == "" {
								continue
							}
							if _, cached := u.tokenCache.Get(tokenAddr); !cached {
								token := u.fetchTokenMeta(ctx, tokenAddr)
								dexData.Tokens = append(dexData.Tokens, token)
							}
						}
					}
				}
			}
		}
	}

	return dexData, nil
}

func (u *UniswapExtractor) SupportsBlock(block *types.UnifiedBlock) bool {
	if !u.IsChainSupported(block.ChainType) {
		return false
	}
	for _, tx := range block.Transactions {
		ethLogs := dex.ExtractEVMLogsFromTransaction(&tx)
		for _, log := range ethLogs {
			if u.isUniswapLog(log) {
				return true
			}
		}
	}
	return false
}

func (u *UniswapExtractor) isUniswapLog(log *ethtypes.Log) bool {
	if len(log.Topics) == 0 {
		return false
	}
	topic0 := log.Topics[0].Hex()
	return topic0 == swapV2EventSig ||
		topic0 == swapV3EventSig ||
		topic0 == mintV2EventSig ||
		topic0 == burnV2EventSig ||
		topic0 == mintV3EventSig ||
		topic0 == burnV3EventSig ||
		topic0 == syncV2EventSig ||
		topic0 == pairCreatedEventSig ||
		topic0 == poolCreatedEventSig
}

func (u *UniswapExtractor) getLogType(log *ethtypes.Log) string {
	if len(log.Topics) == 0 {
		return ""
	}
	topic0 := log.Topics[0].Hex()
	switch topic0 {
	case swapV2EventSig:
		return "swap_v2"
	case swapV3EventSig:
		return "swap_v3"
	case mintV2EventSig, mintV3EventSig:
		return "mint"
	case burnV2EventSig, burnV3EventSig:
		return "burn"
	case syncV2EventSig:
		return "sync"
	case pairCreatedEventSig:
		return "pair_created"
	case poolCreatedEventSig:
		return "pool_created"
	default:
		return ""
	}
}

// SetEVMProcessor implements engine.EVMProcessorInjectable.
// The engine calls this at startup to hand the EVM chain client to the extractor,
// enabling eth_call queries for token metadata (decimals/symbol/name).
func (u *UniswapExtractor) SetEVMProcessor(processor interface{}) {
	type ethClientGetter interface {
		GetEthClient() *ethclient.Client
	}

	if p, ok := processor.(ethClientGetter); ok {
		u.ethClient = p.GetEthClient()
		u.GetLogger().Info("EVM processor injected, token metadata queries enabled")
	}
}

// WarmupPoolTokens pre-populates seenPools from previously-resolved pool
// data so that a process restart does not re-trigger eth_call lookups for
// pools whose token0/token1 are already known from a prior run.
//
// Callers fetch the {addr: {token0, token1}} map via
// StorageEngine.GetAllPoolTokens and pass it here before the engine starts
// processing blocks. Existing seenPools entries are not overwritten — this
// only fills in pools the cache doesn't already have.
func (u *UniswapExtractor) WarmupPoolTokens(poolTokens map[string][2]string) int {
	warmed := 0
	for addr, tokens := range poolTokens {
		if tokens[0] == "" || tokens[1] == "" {
			continue
		}
		if _, loaded := u.seenPools.LoadOrStore(addr, &poolMeta{token0: tokens[0], token1: tokens[1]}); !loaded {
			warmed++
		}
	}
	u.GetLogger().WithField("count", warmed).Info("warmed up pool token cache from storage")
	return warmed
}

// WarmupTokenMeta pre-populates tokenCache from dex_tokens so that a
// process restart reuses previously-resolved token metadata (decimals,
// symbol, name) instead of either re-issuing eth_call (when enabled) or
// silently falling back to the 18-decimals/empty-string defaults for every
// token until it's re-resolved.
//
// dex_tokens is the authoritative, persistent store for token metadata —
// decimals, symbol, and name are all written there by fetchTokenMeta, but
// until this warmup existed, nothing ever read any of them back. Callers
// fetch the {addr: model.Token} map via StorageEngine.GetAllTokenMeta and
// pass it here before the engine starts processing blocks. Existing
// tokenCache entries are not
// overwritten.
// WarmupTokenMeta pre-populates tokenCache from dex_tokens so that a
// process restart reuses previously-resolved token metadata (decimals,
// symbol, name) instead of re-issuing eth_call for every token, or
// silently falling back to the 18-decimals/empty-string defaults until
// it's re-resolved.
//
// dex_tokens is the authoritative, persistent store for token metadata —
// decimals, symbol, and name are all written there by fetchTokenMeta but,
// until this warmup existed, nothing ever read any of them back. Callers
// fetch the {addr: model.Token} map via StorageEngine.GetAllTokenMeta and
// pass it here before the engine starts processing blocks. Existing
// tokenCache entries are not overwritten.
func (u *UniswapExtractor) WarmupTokenMeta(tokenMeta map[string]model.Token) int {
	warmed := 0
	for addr, token := range tokenMeta {
		if _, ok := u.tokenCache.Get(addr); ok {
			continue
		}
		u.tokenCache.Set(addr, token)
		warmed++
	}
	u.GetLogger().WithField("count", warmed).Info("warmed up token metadata cache from storage")
	return warmed
}

// erc20ABI is the minimal ABI needed to call decimals(), symbol(), and name().
var erc20ABI, _ = abi.JSON(strings.NewReader(`[
	{"constant":true,"inputs":[],"name":"decimals","outputs":[{"name":"","type":"uint8"}],"type":"function"},
	{"constant":true,"inputs":[],"name":"symbol","outputs":[{"name":"","type":"string"}],"type":"function"},
	{"constant":true,"inputs":[],"name":"name","outputs":[{"name":"","type":"string"}],"type":"function"}
]`))

// multicall3Address is the Multicall3 contract's deployment address — the
// same address on virtually every EVM chain (Ethereum mainnet included),
// since it's deployed via a deterministic CREATE2 factory. See
// https://www.multicall3.com/ for the deployment list.
const multicall3Address = "0xcA11bde05977b3631167028862bE2a173976CA11"

// multicall3ABI only declares aggregate3 — the one function this package
// needs. allowFailure=true per call means one bad token (missing name(),
// reverting, whatever) doesn't sink the other calls in the same batch;
// each result carries its own Success flag instead.
var multicall3ABI, _ = abi.JSON(strings.NewReader(`[{
	"inputs": [{
		"components": [
			{"internalType": "address", "name": "target", "type": "address"},
			{"internalType": "bool", "name": "allowFailure", "type": "bool"},
			{"internalType": "bytes", "name": "callData", "type": "bytes"}
		],
		"internalType": "struct Multicall3.Call3[]",
		"name": "calls",
		"type": "tuple[]"
	}],
	"name": "aggregate3",
	"outputs": [{
		"components": [
			{"internalType": "bool", "name": "success", "type": "bool"},
			{"internalType": "bytes", "name": "returnData", "type": "bytes"}
		],
		"internalType": "struct Multicall3.Result[]",
		"name": "returnData",
		"type": "tuple[]"
	}],
	"stateMutability": "payable",
	"type": "function"
}]`))

// multicall3Call / multicall3Result mirror aggregate3's Call3/Result tuple
// components — struct field names must match the ABI component names
// (capitalized) for go-ethereum's abi package to pack/unpack them via
// reflection; field order must match the ABI too.
type multicall3Call struct {
	Target       common.Address
	AllowFailure bool
	CallData     []byte
}

type multicall3Result struct {
	Success    bool
	ReturnData []byte
}

func (u *UniswapExtractor) fetchTokenMeta(ctx context.Context, tokenAddr string) model.Token {
	if cached, ok := u.tokenCache.Get(tokenAddr); ok {
		return cached
	}

	// Redis layer: shared across processes/restarts, sits between the local
	// memory cache and dex_tokens. A hit here means some other run (or a
	// prior instance of this one) already resolved this token's metadata —
	// reuse it and backfill the memory cache so subsequent calls in this
	// process skip Redis entirely.
	if u.redisClient != nil {
		if token, ok := u.getTokenMetaFromRedis(ctx, tokenAddr); ok {
			u.tokenCache.Set(tokenAddr, token)
			return token
		}
	}

	// Default: assume 18 decimals. Callers get a usable value even if RPC fails.
	token := model.Token{
		Addr:     tokenAddr,
		Decimals: 18,
	}

	if u.ethClient == nil {
		u.tokenCache.Set(tokenAddr, token)
		return token
	}

	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	addr := common.HexToAddress(tokenAddr)

	// One RPC round trip for all three fields, via Multicall3, instead of
	// three sequential eth_calls. Under load against a public RPC node,
	// each individual eth_call round trip is where the latency actually
	// goes (not the ABI encode/decode) — collapsing decimals()/symbol()/
	// name() into a single aggregate3 call cuts that 3x down to 1x, which
	// matters a lot when this runs for every never-before-seen token.
	decimalsData, decErr := erc20ABI.Pack("decimals")
	symbolData, symErr := erc20ABI.Pack("symbol")
	nameData, nameErr := erc20ABI.Pack("name")

	if decErr == nil && symErr == nil && nameErr == nil {
		calls := []multicall3Call{
			{Target: addr, AllowFailure: true, CallData: decimalsData},
			{Target: addr, AllowFailure: true, CallData: symbolData},
			{Target: addr, AllowFailure: true, CallData: nameData},
		}

		if packed, err := multicall3ABI.Pack("aggregate3", calls); err == nil {
			mcAddr := common.HexToAddress(multicall3Address)
			msg := ethereum.CallMsg{To: &mcAddr, Data: packed}
			if out, err := u.ethClient.CallContract(callCtx, msg, nil); err == nil {
				var results []multicall3Result
				if err := multicall3ABI.UnpackIntoInterface(&results, "aggregate3", out); err == nil && len(results) == 3 {
					if r := results[0]; r.Success && len(r.ReturnData) >= 32 {
						token.Decimals = int(new(big.Int).SetBytes(r.ReturnData[len(r.ReturnData)-32:]).Uint64())
					}
					if r := results[1]; r.Success {
						if vals, err := erc20ABI.Unpack("symbol", r.ReturnData); err == nil && len(vals) > 0 {
							if s, ok := vals[0].(string); ok {
								token.Symbol = s
							}
						}
					}
					if r := results[2]; r.Success {
						if vals, err := erc20ABI.Unpack("name", r.ReturnData); err == nil && len(vals) > 0 {
							if s, ok := vals[0].(string); ok {
								token.Name = s
							}
						}
					}
				}
			}
		}
	}

	u.tokenCache.Set(tokenAddr, token)
	if u.redisClient != nil {
		u.setTokenMetaInRedis(ctx, token)
	}
	return token
}

// SetTokenCacheRedis implements an injectable interface (see
// registerDexExtractors in cmd/parser) for wiring an existing *redis.Client
// into this extractor's token cache. Redis sits between the per-process
// tokenCache and the dex_tokens table: a metadata lookup miss in tokenCache
// checks Redis before falling through to eth_call (or the 18-decimals
// default), and a successful resolution is written back to Redis so other
// processes — or this one after a restart, before WarmupTokenMeta has
// run — can reuse it without re-deriving it.
func (u *UniswapExtractor) SetTokenCacheRedis(client *redis.Client) {
	u.redisClient = client
	u.GetLogger().Info("Redis token cache layer enabled")
}

const tokenMetaRedisPrefix = "token_meta:"

// getTokenMetaFromRedis reads cached metadata for tokenAddr.
// Returns ok=false on any miss or error (key not found, Redis unavailable,
// malformed value) — callers should treat this the same as a cache miss and
// fall through to the next resolution step, not as a fatal error.
// getTokenMetaFromRedis reads cached metadata for tokenAddr, stored as a
// Redis hash with decimals/symbol/name fields. Returns ok=false on any miss
// or error (key not found, Redis unavailable, malformed decimals value) —
// callers should treat this the same as a cache miss and fall through to
// the next resolution step, not as a fatal error.
func (u *UniswapExtractor) getTokenMetaFromRedis(ctx context.Context, tokenAddr string) (model.Token, bool) {
	callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	key := tokenMetaRedisPrefix + strings.ToLower(tokenAddr)
	vals, err := u.redisClient.HGetAll(callCtx, key).Result()
	if err != nil || len(vals) == 0 {
		return model.Token{}, false // includes redis.Nil (key doesn't exist)
	}
	decimals, err := strconv.Atoi(vals["decimals"])
	if err != nil {
		return model.Token{}, false
	}
	return model.Token{
		Addr:     tokenAddr,
		Decimals: decimals,
		Symbol:   vals["symbol"],
		Name:     vals["name"],
	}, true
}

// setTokenMetaInRedis writes resolved metadata for tokenAddr. Errors are
// logged but not returned — Redis is a best-effort acceleration layer
// here, not a source of truth, so a failed write should not affect the
// caller's ability to use the metadata it just resolved.
func (u *UniswapExtractor) setTokenMetaInRedis(ctx context.Context, token model.Token) {
	callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	key := tokenMetaRedisPrefix + strings.ToLower(token.Addr)
	fields := map[string]interface{}{
		"decimals": token.Decimals,
		"symbol":   token.Symbol,
		"name":     token.Name,
	}
	if err := u.redisClient.HSet(callCtx, key, fields).Err(); err != nil {
		u.GetLogger().WithField("token", token.Addr).Warnf("failed to write token meta to redis: %v", err)
		return
	}
	u.redisClient.Expire(callCtx, key, 30*24*time.Hour)
}

// determineSide decides whether a swap is a "buy" or "sell" based on quote asset ranking.
//
// Convention (consistent with Cetus extractor):
//   - "buy"  = user spent a quote asset to acquire the base token
//     (e.g. paid USDC to get ETH)
//   - "sell" = user spent the base token to acquire a quote asset
//     (e.g. paid ETH to get USDC)
//   - "swap" = neither token is a known quote asset (cannot determine direction)
//
// When both tokens are quote assets (e.g. USDC/USDT), the higher-ranked one
// is treated as the quote; the lower-ranked one plays the base role.
//
// tokenIn  = address of the token the user paid
// tokenOut = address of the token the user received
func (u *UniswapExtractor) determineSide(tokenIn, tokenOut string) string {
	rankIn := u.GetQuoteAssetRank(strings.ToLower(tokenIn))
	rankOut := u.GetQuoteAssetRank(strings.ToLower(tokenOut))

	inIsQuote := rankIn >= 0
	outIsQuote := rankOut >= 0

	switch {
	case outIsQuote && !inIsQuote:
		// User sold base token, received quote → sell
		return "sell"
	case inIsQuote && !outIsQuote:
		// User spent quote token, received base → buy
		return "buy"
	case inIsQuote && outIsQuote:
		// Both are quote assets (e.g. USDC → USDT); higher rank is the "real" quote.
		if rankOut >= rankIn {
			return "sell" // spent lower-rank quote to get higher-rank quote
		}
		return "buy"
	default:
		// Neither token is a known quote asset
		return "swap"
	}
}

// resolvePoolTokens looks up a pool's token0/token1 addresses via eth_call,
// independent of whether PairCreated/PoolCreated has been scanned yet.
//
// This closes the gap that caused raw, unnormalized value/price for a pool's
// first-seen swap: previously, seenPools was only populated by (a) the
// PairCreated/PoolCreated event handler, or (b) lazyPool's placeholder entry
// (which has empty token addresses). If a pool's first swap was scanned
// before its creation event — or before any creation event was scanned at
// all — parseV2Swap/parseV3Swap had no token addresses to normalize with,
// and fell through to the raw-value fallback branch.
//
// Result is cached in seenPools so subsequent swaps for the same pool reuse
// it without a repeat RPC call. Safe for concurrent first-time lookups via
// LoadOrStore on a per-pool "resolving" marker — only one goroutine performs
// the actual eth_call per pool.
func (u *UniswapExtractor) resolvePoolTokens(ctx context.Context, poolAddr string) *poolMeta {
	if meta, ok := u.seenPools.Load(poolAddr); ok {
		pm := meta.(*poolMeta)
		if pm.token0 != "" && pm.token1 != "" {
			return pm
		}
	}

	// Redis layer: sits between the local seenPools cache and the disabled
	// eth_call path below. A hit here means thegraph.Syncer (or a prior
	// run's warmup) already resolved this pool's tokens — reuse it and
	// backfill seenPools so subsequent swaps for the same pool skip Redis
	// entirely, same pattern as fetchTokenMeta's Redis layer above. This
	// is a cheap, bounded lookup (2s timeout inside dexcache, not the
	// eth_call path's RPC round trip), so it's always worth trying before
	// falling through to eth_call below — a Redis hit means this pool has
	// been resolved before (by any process), so we skip an RPC call
	// entirely for anything except a genuinely first-ever-seen pool.
	if u.redisClient != nil {
		if pool, ok := dexcache.GetPool(ctx, u.redisClient, poolAddr); ok {
			if token0, token1 := pool.Tokens[0], pool.Tokens[1]; token0 != "" && token1 != "" {
				pm := &poolMeta{token0: token0, token1: token1}
				u.seenPools.Store(poolAddr, pm)
				return pm
			}
		}
	}

	// No eth_call here. Unlike token decimals (which genuinely can't be
	// known without either an eth_call or a prior subgraph sync), a
	// pool's token0/token1 addresses are always available for free from
	// the PairCreated/PoolCreated event itself — see parseV2PairCreated/
	// parseV3PoolCreated, which populate seenPools directly from the log,
	// zero RPC calls. So a pool reaching this point (miss on both
	// seenPools and Redis) means its creation event hasn't been scanned
	// yet (e.g. it predates this process's start_block and hasn't been
	// backfilled by thegraph-sync/warmup) — there's nothing to eth_call
	// for the tokens; token0()/token1() would just tell us what the event
	// log will tell us for free once it's actually scanned. Swaps for
	// such a pool fall through to the raw-value fallback until then.
	return nil
}

// lazyPool 在 seenPools 缓存里没有这个地址时,产出一条最小 Pool 记录并缓存。
// Swap 日志里拿不到 token0/token1 地址(只有 PairCreated/PoolCreated 才有),
// 所以 Tokens 留空——表达「池子存在」已够做协议/factory 维度的统计。
// 等后续扫到建池事件或补全 token 元数据时,存储层的 upsert 会覆盖补全。
func (u *UniswapExtractor) lazyPool(poolAddr, factory, protocol string, tx *types.UnifiedTransaction) *model.Pool {
	if _, loaded := u.seenPools.LoadOrStore(poolAddr, &poolMeta{}); loaded {
		// 已产出过,本批次无需再产出
		return nil
	}
	return &model.Pool{
		Addr:     poolAddr,
		Factory:  factory,
		Protocol: protocol,
		Tokens:   map[int]string{}, // token 地址待建池事件或 token 解析时回填
		Fee:      0,                // 费率待建池事件回填
		Extra: &model.PoolExtra{
			Hash: tx.TxHash,
			From: tx.FromAddress,
			Time: uint64(tx.Timestamp.Unix()),
		},
	}
}

// parseV2Swap parses V2 Swap(address indexed sender, uint256 amount0In, uint256 amount1In, uint256 amount0Out, uint256 amount1Out, address indexed to)
func (u *UniswapExtractor) parseV2Swap(ctx context.Context, log *ethtypes.Log, tx *types.UnifiedTransaction, logIdx, swapIdx int64) *model.Transaction {
	if len(log.Data) < 128 {
		u.GetLogger().WithField("tx_hash", tx.TxHash).Warn("V2 swap log data too short")
		return nil
	}

	amount0In := new(big.Int).SetBytes(log.Data[0:32])
	amount1In := new(big.Int).SetBytes(log.Data[32:64])
	amount0Out := new(big.Int).SetBytes(log.Data[64:96])
	amount1Out := new(big.Int).SetBytes(log.Data[96:128])

	token0IsIn := amount0In.Sign() > 0
	var amountIn, amountOut *big.Int
	if token0IsIn {
		amountIn = amount0In
		amountOut = amount1Out
	} else {
		amountIn = amount1In
		amountOut = amount0Out
	}

	poolAddr := log.Address.Hex()

	// Price/Value are always computed from raw amounts — no decimals
	// lookup, no normalization, regardless of whether this pool's tokens
	// are known. This means Price/Value are in raw smallest-unit terms,
	// not human-readable units; normalizing into a human-readable price
	// is a read-time concern for whatever consumes this data (join
	// against dex_tokens.decimals), not something resolved here.
	price := dex.CalcPrice(amountIn, amountOut)
	value := dex.CalcValue(amountIn, price)

	// Side (buy/sell) classification is independent of decimals — it only
	// needs to know *which* token was paid in, not how many decimals it
	// has — so this still uses the cache-only resolvePoolTokens (no
	// eth_call) rather than being removed along with the normalization
	// logic above.
	side := "swap" // fallback when pool token addresses are unknown
	if pm := u.resolvePoolTokens(ctx, poolAddr); pm != nil {
		var tokenIn, tokenOut string
		if token0IsIn {
			tokenIn, tokenOut = pm.token0, pm.token1
		} else {
			tokenIn, tokenOut = pm.token1, pm.token0
		}
		side = u.determineSide(tokenIn, tokenOut)
	}

	return &model.Transaction{
		Addr:        poolAddr,
		Router:      tx.ToAddress,
		Factory:     uniswapV2FactoryAddr,
		Protocol:    "uniswap_v2",
		Pool:        poolAddr,
		Hash:        tx.TxHash,
		From:        tx.FromAddress,
		Side:        side,
		Amount:      amountIn,
		Price:       price,
		Value:       value,
		Time:        uint64(tx.Timestamp.Unix()),
		EventIndex:  logIdx,
		TxIndex:     int64(tx.TxIndex),
		SwapIndex:   swapIdx,
		BlockNumber: dex.GetBlockNumber(tx),
		Extra: &model.TransactionExtra{
			QuotePrice: fmt.Sprintf("%.18f", price),
			Type:       "swap",
		},
	}
}

// parseV3Swap parses V3 Swap(address indexed sender, address indexed recipient, int256 amount0, int256 amount1, uint160 sqrtPriceX96, uint128 liquidity, int24 tick)
func (u *UniswapExtractor) parseV3Swap(ctx context.Context, log *ethtypes.Log, tx *types.UnifiedTransaction, logIdx, swapIdx int64) *model.Transaction {
	if len(log.Data) < 160 {
		u.GetLogger().WithField("tx_hash", tx.TxHash).Warn("V3 swap log data too short")
		return nil
	}

	// V3 Swap amounts are signed int256:
	//   positive = flows into pool (user pays)
	//   negative = flows out of pool (user receives)
	amount0 := dex.ToSignedInt256(log.Data[0:32])
	amount1 := dex.ToSignedInt256(log.Data[32:64])
	sqrtPriceX96 := new(big.Int).SetBytes(log.Data[64:96])

	// token0IsIn: amount0 > 0 means user paid token0
	token0IsIn := amount0.Sign() > 0
	// Pick the positive amount as amountIn (what user paid)
	amountIn := new(big.Int).Abs(amount0)
	amountOut := new(big.Int).Abs(amount1)
	if !token0IsIn {
		amountIn, amountOut = amountOut, amountIn
	}

	poolAddr := log.Address.Hex()

	// Price/Value always computed from raw sqrtPriceX96/amounts — see
	// parseV2Swap's doc comment on why (no decimals lookup, no
	// normalization, regardless of whether this pool's tokens are known).
	price := dex.CalcV3Price(sqrtPriceX96)
	value := dex.CalcValue(amountIn, price)

	// Side classification doesn't need decimals — see parseV2Swap.
	side := "swap"
	if pm := u.resolvePoolTokens(ctx, poolAddr); pm != nil {
		var tokenIn, tokenOut string
		if token0IsIn {
			tokenIn, tokenOut = pm.token0, pm.token1
		} else {
			tokenIn, tokenOut = pm.token1, pm.token0
		}
		side = u.determineSide(tokenIn, tokenOut)
	}

	return &model.Transaction{
		Addr:        poolAddr,
		Router:      tx.ToAddress,
		Factory:     uniswapV3FactoryAddr,
		Protocol:    "uniswap_v3",
		Pool:        poolAddr,
		Hash:        tx.TxHash,
		From:        tx.FromAddress,
		Side:        side,
		Amount:      amountIn,
		Price:       price,
		Value:       value,
		Time:        uint64(tx.Timestamp.Unix()),
		EventIndex:  logIdx,
		TxIndex:     int64(tx.TxIndex),
		SwapIndex:   swapIdx,
		BlockNumber: dex.GetBlockNumber(tx),
		Extra: &model.TransactionExtra{
			QuotePrice: fmt.Sprintf("%.18f", price),
			Type:       "swap",
		},
	}
}

// parseLiquidity parses V2/V3 Mint/Burn events
func (u *UniswapExtractor) parseLiquidity(log *ethtypes.Log, tx *types.UnifiedTransaction, side string, logIdx int64) *model.Liquidity {
	if len(log.Data) < 64 {
		return nil
	}

	topic0 := log.Topics[0].Hex()

	var amount0, amount1 *big.Int
	switch topic0 {
	case mintV3EventSig:
		// V3 Mint data: [address sender, uint128 amount, uint256 amount0, uint256 amount1]
		if len(log.Data) >= 128 {
			amount0 = new(big.Int).SetBytes(log.Data[64:96])
			amount1 = new(big.Int).SetBytes(log.Data[96:128])
		}
	case burnV3EventSig:
		// V3 Burn data: [uint128 amount, uint256 amount0, uint256 amount1]
		if len(log.Data) >= 96 {
			amount0 = new(big.Int).SetBytes(log.Data[32:64])
			amount1 = new(big.Int).SetBytes(log.Data[64:96])
		}
	default:
		// V2 Mint/Burn data: [uint256 amount0, uint256 amount1]
		amount0 = new(big.Int).SetBytes(log.Data[0:32])
		amount1 = new(big.Int).SetBytes(log.Data[32:64])
	}

	if amount0 == nil {
		amount0 = big.NewInt(0)
	}
	if amount1 == nil {
		amount1 = big.NewInt(0)
	}

	totalAmount := new(big.Int).Add(amount0, amount1)
	val0, _ := new(big.Float).SetInt(amount0).Float64()
	val1, _ := new(big.Float).SetInt(amount1).Float64()

	poolAddr := log.Address.Hex()
	key := fmt.Sprintf("%s_%s_%d", tx.TxHash, side, logIdx)

	return &model.Liquidity{
		Addr:    poolAddr,
		Router:  tx.ToAddress,
		Factory: u.getFactoryAddress(log),
		Pool:    poolAddr,
		Hash:    tx.TxHash,
		From:    tx.FromAddress,
		Side:    side,
		Amount:  totalAmount,
		Value:   val0 + val1,
		Time:    uint64(tx.Timestamp.Unix()),
		Key:     key,
		Extra: &model.LiquidityExtra{
			Key:     key,
			Amounts: amount1,
			Values:  []float64{val0, val1},
			Time:    uint64(tx.Timestamp.Unix()),
		},
	}
}

// parseSync parses V2 Sync(uint112 reserve0, uint112 reserve1) into a pool reserve snapshot.
// Sync is emitted by the pair contract itself, so log.Address is the pool address.
// Note: Uniswap V3 does not emit Sync (concentrated liquidity), so this only covers V2 pools.
func (u *UniswapExtractor) parseSync(ctx context.Context, log *ethtypes.Log, tx *types.UnifiedTransaction) *model.Reserve {
	if len(log.Data) < 64 {
		return nil
	}

	// Both reserves are non-indexed uint112, right-aligned in 32-byte words.
	reserve0 := new(big.Int).SetBytes(log.Data[0:32])
	reserve1 := new(big.Int).SetBytes(log.Data[32:64])

	poolAddr := log.Address.Hex()
	return &model.Reserve{
		Addr:     poolAddr,
		Protocol: "uniswap_v2",
		Amounts: map[int]*big.Int{
			0: reserve0,
			1: reserve1,
		},
		Time: uint64(tx.Timestamp.Unix()),
	}
}

// parseV3Reserve derives virtual reserves at the current price from a V3 Swap event.
// V3 has no Sync event and no global reserve pair; around the current tick a V3 pool
// behaves like a V2 pool with:
//
//	x (token0) = L * 2^96 / sqrtPriceX96
//	y (token1) = L * sqrtPriceX96 / 2^96
//
// where L is the active in-range liquidity and sqrtPriceX96 = sqrt(price) * 2^96.
// These are the virtual reserves backing the current price (depth at the current tick),
// NOT the pool's total token balances. No extra RPC is needed: both fields are already
// in the Swap log (data[64:96] = sqrtPriceX96, data[96:128] = liquidity).
func (u *UniswapExtractor) parseV3Reserve(ctx context.Context, log *ethtypes.Log, tx *types.UnifiedTransaction) *model.Reserve {
	if len(log.Data) < 160 {
		return nil
	}

	sqrtPriceX96 := new(big.Int).SetBytes(log.Data[64:96])
	liquidity := new(big.Int).SetBytes(log.Data[96:128])
	if sqrtPriceX96.Sign() == 0 || liquidity.Sign() == 0 {
		return nil
	}

	q96 := new(big.Int).Lsh(big.NewInt(1), 96) // 2^96

	// x (token0) = L * 2^96 / sqrtPriceX96
	amount0 := new(big.Int).Mul(liquidity, q96)
	amount0.Quo(amount0, sqrtPriceX96)

	// y (token1) = L * sqrtPriceX96 / 2^96
	amount1 := new(big.Int).Mul(liquidity, sqrtPriceX96)
	amount1.Rsh(amount1, 96)

	poolAddr := log.Address.Hex()
	return &model.Reserve{
		Addr:     poolAddr,
		Protocol: "uniswap_v3",
		Amounts: map[int]*big.Int{
			0: amount0,
			1: amount1,
		},
		Time: uint64(tx.Timestamp.Unix()),
	}
}

// parseV2PairCreated parses PairCreated(address indexed token0, address indexed token1, address pair, uint256)
// FIX #5: Pool address from data[0:32], not log.Address
func (u *UniswapExtractor) parseV2PairCreated(log *ethtypes.Log, tx *types.UnifiedTransaction) *model.Pool {
	if len(log.Topics) < 3 || len(log.Data) < 64 {
		return nil
	}

	token0 := common.BytesToAddress(log.Topics[1].Bytes()).Hex()
	token1 := common.BytesToAddress(log.Topics[2].Bytes()).Hex()
	pairAddr := common.BytesToAddress(log.Data[0:32]).Hex()

	// 建池事件是权威来源,覆盖 lazyPool 的占位记录,存入 token 地址供后续 swap 查 decimals
	u.seenPools.Store(pairAddr, &poolMeta{token0: token0, token1: token1})

	return &model.Pool{
		Addr:     pairAddr,
		Factory:  uniswapV2FactoryAddr,
		Protocol: "uniswap_v2",
		Tokens:   map[int]string{0: token0, 1: token1},
		Fee:      3000,
		Extra: &model.PoolExtra{
			Hash: tx.TxHash,
			From: tx.FromAddress,
			Time: uint64(tx.Timestamp.Unix()),
		},
	}
}

// parseV3PoolCreated parses PoolCreated(address indexed token0, address indexed token1, uint24 indexed fee, int24 tickSpacing, address pool)
// FIX #5: Pool address from data[32:64], not log.Address
func (u *UniswapExtractor) parseV3PoolCreated(log *ethtypes.Log, tx *types.UnifiedTransaction) *model.Pool {
	if len(log.Topics) < 4 || len(log.Data) < 64 {
		return nil
	}

	token0 := common.BytesToAddress(log.Topics[1].Bytes()).Hex()
	token1 := common.BytesToAddress(log.Topics[2].Bytes()).Hex()
	fee := new(big.Int).SetBytes(log.Topics[3].Bytes())
	poolAddr := common.BytesToAddress(log.Data[32:64]).Hex()

	// 建池事件是权威来源,覆盖 lazyPool 的占位记录。
	u.seenPools.Store(poolAddr, &poolMeta{token0: token0, token1: token1})

	return &model.Pool{
		Addr:     poolAddr,
		Factory:  uniswapV3FactoryAddr,
		Protocol: "uniswap_v3",
		Tokens:   map[int]string{0: token0, 1: token1},
		Fee:      int(fee.Int64()),
		Extra: &model.PoolExtra{
			Hash: tx.TxHash,
			From: tx.FromAddress,
			Time: uint64(tx.Timestamp.Unix()),
		},
	}
}

func (u *UniswapExtractor) getFactoryAddress(log *ethtypes.Log) string {
	if len(log.Topics) == 0 {
		return ""
	}
	topic0 := log.Topics[0].Hex()
	switch topic0 {
	case swapV2EventSig, mintV2EventSig, burnV2EventSig, pairCreatedEventSig:
		return uniswapV2FactoryAddr
	case swapV3EventSig, mintV3EventSig, burnV3EventSig, poolCreatedEventSig:
		return uniswapV3FactoryAddr
	default:
		return ""
	}
}
