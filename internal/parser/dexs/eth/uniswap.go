package eth

import (
	"context"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"

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

// poolABI is the minimal ABI needed to call token0()/token1() on a V2/V3 pool contract.
// Both methods exist on every Uniswap V2 pair and V3 pool from the moment the contract
// is deployed — unlike PairCreated/PoolCreated, they don't require having scanned the
// block where the pool was created. This lets fallback paths resolve token addresses
// for a pool encountered out of event order (e.g. its first swap was scanned before
// its creation event), instead of falling back to raw, unnormalized price/value.
var poolABI, _ = abi.JSON(strings.NewReader(`[
	{"constant":true,"inputs":[],"name":"token0","outputs":[{"name":"","type":"address"}],"type":"function"},
	{"constant":true,"inputs":[],"name":"token1","outputs":[{"name":"","type":"address"}],"type":"function"}
]`))

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

	// decimals()
	if data, err := erc20ABI.Pack("decimals"); err == nil {
		var result []byte
		msg := ethereum.CallMsg{To: &addr, Data: data}
		if out, err := u.ethClient.CallContract(callCtx, msg, nil); err == nil {
			result = out
		}

		if len(result) >= 32 {
			token.Decimals = int(new(big.Int).SetBytes(result[len(result)-32:]).Uint64())
		}
	}

	// symbol()
	if data, err := erc20ABI.Pack("symbol"); err == nil {
		msg := ethereum.CallMsg{To: &addr, Data: data}
		if out, err := u.ethClient.CallContract(callCtx, msg, nil); err == nil {
			if vals, err := erc20ABI.Unpack("symbol", out); err == nil && len(vals) > 0 {
				if s, ok := vals[0].(string); ok {
					token.Symbol = s
				}
			}
		}
	}

	// name()
	if data, err := erc20ABI.Pack("name"); err == nil {
		msg := ethereum.CallMsg{To: &addr, Data: data}
		if out, err := u.ethClient.CallContract(callCtx, msg, nil); err == nil {
			if vals, err := erc20ABI.Unpack("name", out); err == nil && len(vals) > 0 {
				if s, ok := vals[0].(string); ok {
					token.Name = s
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

	// Active eth_call resolution disabled (2026-06-30): under load, this was
	// observed to cost tens of seconds per unresolved pool — RPC latency
	// against the public node was high enough to stall the whole batch
	// (one block batch took 81.4s, almost entirely waiting on a single
	// pool's token0()/token1() calls). Until this is reworked to run off
	// the synchronous extraction path (see TODO below), pools not already
	// known via a scanned PairCreated/PoolCreated event or the startup
	// warmup (WarmupPoolTokens) fall through to the raw-value fallback in
	// parseV2Swap/parseV3Swap — same behavior as before resolvePoolTokens
	// was introduced.
	//
	// TODO: move this resolution off the hot path — e.g. queue unresolved
	// pool addresses and resolve them concurrently in a background worker,
	// so a slow/unavailable RPC node degrades data quality for one batch
	// instead of stalling block processing. See registerDexExtractors'
	// WarmupPoolTokens call for the cache this would feed into.
	return nil
}

func (u *UniswapExtractor) resolvePoolTokensViaEthCall(ctx context.Context, poolAddr string) *poolMeta {
	if u.ethClient == nil {
		return nil
	}

	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	addr := common.HexToAddress(poolAddr)

	var token0, token1 string

	if data, err := poolABI.Pack("token0"); err == nil {
		msg := ethereum.CallMsg{To: &addr, Data: data}
		if out, err := u.ethClient.CallContract(callCtx, msg, nil); err == nil {
			if vals, err := poolABI.Unpack("token0", out); err == nil && len(vals) > 0 {
				if a, ok := vals[0].(common.Address); ok {
					token0 = a.Hex()
				}
			}
		}
	}
	if data, err := poolABI.Pack("token1"); err == nil {
		msg := ethereum.CallMsg{To: &addr, Data: data}
		if out, err := u.ethClient.CallContract(callCtx, msg, nil); err == nil {
			if vals, err := poolABI.Unpack("token1", out); err == nil && len(vals) > 0 {
				if a, ok := vals[0].(common.Address); ok {
					token1 = a.Hex()
				}
			}
		}
	}

	if token0 == "" || token1 == "" {
		// eth_call failed (e.g. not a standard V2/V3 pool, or RPC error).
		// Leave seenPools untouched so a later call can retry.
		return nil
	}

	pm := &poolMeta{token0: token0, token1: token1}
	u.seenPools.Store(poolAddr, pm)
	return pm
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

	var price, value float64
	side := "swap" // fallback when pool token addresses are unknown
	if pm := u.resolvePoolTokens(ctx, poolAddr); pm != nil {
		decimals0 := u.fetchTokenMeta(ctx, pm.token0).Decimals
		decimals1 := u.fetchTokenMeta(ctx, pm.token1).Decimals

		// price is always token1/token0, independent of buy/sell direction.
		// Deriving it from amountIn/amountOut would flip depending on trade
		// direction, producing reciprocal values for buys vs sells — the same
		// class of bug fixed for V3 via CalcV3PriceNormalized. V2 has no
		// sqrtPriceX96, so instead we use whichever side of this swap is
		// non-zero on both amount0/amount1 to derive a fixed-direction ratio:
		// amount0 and amount1 here represent the *same* swap's two legs
		// (one in, one out), so token1Amount/token0Amount is well-defined
		// regardless of which leg was "in" vs "out".
		var amount0, amount1 *big.Int
		if token0IsIn {
			amount0, amount1 = amountIn, amountOut
		} else {
			amount0, amount1 = amountOut, amountIn
		}
		price = dex.CalcPriceNormalized(amount0, decimals0, amount1, decimals1)

		var tokenIn, tokenOut string
		if token0IsIn {
			tokenIn, tokenOut = pm.token0, pm.token1
		} else {
			tokenIn, tokenOut = pm.token1, pm.token0
		}
		decimalsIn := decimals0
		if !token0IsIn {
			decimalsIn = decimals1
		}
		value = dex.CalcValueNormalized(amountIn, decimalsIn, price)
		side = u.determineSide(tokenIn, tokenOut)
	} else {
		price = dex.CalcPrice(amountIn, amountOut)
		value = dex.CalcValue(amountIn, price)
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
	var price, value float64
	side := "swap"
	if pm := u.resolvePoolTokens(ctx, poolAddr); pm != nil {
		decimals0 := u.fetchTokenMeta(ctx, pm.token0).Decimals
		decimals1 := u.fetchTokenMeta(ctx, pm.token1).Decimals

		// price is always token1/token0, independent of buy/sell direction.
		// Deriving price from amountIn/amountOut instead would flip depending
		// on trade direction, producing reciprocal values for buys vs sells
		// (e.g. 1.57e-9 vs 6.34e8 for the same pool) — that bug is why this
		// uses sqrtPriceX96 directly rather than the swap's relative amounts.
		price = dex.CalcV3PriceNormalized(sqrtPriceX96, decimals0, decimals1)

		// value is expressed in token0 terms for every swap, regardless of
		// which side the user paid in, so volumes aggregate consistently:
		//   token0 paid  -> value = amount0 (already in token0)
		//   token1 paid  -> value = amount1 / price (convert token1 -> token0)
		amount0Norm := dex.NormalizeAmount(new(big.Int).Abs(amount0), decimals0)
		amount1Norm := dex.NormalizeAmount(new(big.Int).Abs(amount1), decimals1)
		if token0IsIn {
			value = amount0Norm
		} else if price > 0 {
			value = amount1Norm / price
		}

		var tokenIn, tokenOut string
		if token0IsIn {
			tokenIn, tokenOut = pm.token0, pm.token1
		} else {
			tokenIn, tokenOut = pm.token1, pm.token0
		}
		side = u.determineSide(tokenIn, tokenOut)
	} else {
		price = dex.CalcV3Price(sqrtPriceX96)
		value = dex.CalcValue(amountIn, price)
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
	reserve := &model.Reserve{
		Addr:     poolAddr,
		Protocol: "uniswap_v2",
		Amounts: map[int]*big.Int{
			0: reserve0,
			1: reserve1,
		},
		Time: uint64(tx.Timestamp.Unix()),
	}

	// Normalize into human-readable units when token decimals are already
	// known (from a scanned PairCreated event or the startup warmup cache).
	// resolvePoolTokens only checks the cache here — it does not issue
	// eth_call — so this never blocks the hot path; pools not yet known
	// simply get Value left unset, same as before this normalization existed.
	if pm := u.resolvePoolTokens(ctx, poolAddr); pm != nil {
		decimals0 := u.fetchTokenMeta(ctx, pm.token0).Decimals
		decimals1 := u.fetchTokenMeta(ctx, pm.token1).Decimals
		reserve.Value = map[int]float64{
			0: dex.NormalizeAmount(reserve0, decimals0),
			1: dex.NormalizeAmount(reserve1, decimals1),
		}
	}

	return reserve
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
	reserve := &model.Reserve{
		Addr:     poolAddr,
		Protocol: "uniswap_v3",
		Amounts: map[int]*big.Int{
			0: amount0,
			1: amount1,
		},
		Time: uint64(tx.Timestamp.Unix()),
	}

	// Normalize when token decimals are already cached (see parseSync for
	// why this is cache-only and never blocks on eth_call).
	if pm := u.resolvePoolTokens(ctx, poolAddr); pm != nil {
		decimals0 := u.fetchTokenMeta(ctx, pm.token0).Decimals
		decimals1 := u.fetchTokenMeta(ctx, pm.token1).Decimals
		reserve.Value = map[int]float64{
			0: dex.NormalizeAmount(amount0, decimals0),
			1: dex.NormalizeAmount(amount1, decimals1),
		}
	}

	return reserve
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
