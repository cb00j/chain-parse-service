package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	appinit "unified-tx-parser/internal/app"
	"unified-tx-parser/internal/config"
	"unified-tx-parser/internal/dexcache"
	"unified-tx-parser/internal/logger"
	"unified-tx-parser/internal/model"
	"unified-tx-parser/internal/parser/chains/bsc"
	"unified-tx-parser/internal/parser/chains/ethereum"
	"unified-tx-parser/internal/parser/chains/solana"
	"unified-tx-parser/internal/parser/chains/sui"
	dex "unified-tx-parser/internal/parser/dexs"
	dexfactory "unified-tx-parser/internal/parser/dexs/factory"
	"unified-tx-parser/internal/parser/engine"
	"unified-tx-parser/internal/types"

	"github.com/redis/go-redis/v9"
)

var (
	version = "dev"
	commit  = "unknown"
)

var log = logger.New("parser", "main")

func main() {
	chainType := resolveChainType(parseArgs())
	cfg := loadConfig(chainType)

	logger.SetLevel(cfg.Logging.Level)

	log.Infof("parser service starting (version=%s, commit=%s)", version, commit)

	app, err := initApp(cfg)
	if err != nil {
		log.Fatalf("init failed: %v", err)
	}
	defer app.shutdown()

	if err := app.engine.Start(); err != nil {
		log.Fatalf("engine start failed: %v", err)
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("shutdown signal received, stopping")
}

type application struct {
	engine      *engine.Engine
	redisClient *redis.Client
}

func (a *application) shutdown() {
	log.Info("shutting down")
	if a.engine != nil {
		a.engine.Stop()
	}
	if a.redisClient != nil {
		a.redisClient.Close()
	}
	log.Info("parser service stopped")
}

func parseArgs() string {
	var chain string
	flag.StringVar(&chain, "chain", "", "chain type to run (sui, ethereum, bsc, solana)")
	flag.Parse()
	return chain
}

// resolveChainType applies the same "-chain flag, then CHAIN_TYPE env var"
// precedence loadConfig used to apply internally — pulled out so main()
// has the final resolved value to pass to initApp (previously it only had
// the pre-fallback flag value, which was wrong whenever the chain was
// selected via env var rather than -chain).
func resolveChainType(fromFlag string) string {
	chain := fromFlag
	if chain == "" {
		chain = os.Getenv("CHAIN_TYPE")
	}
	return strings.ToLower(chain)
}

func loadConfig(chainType string) *config.Config {
	if chainType == "" {
		log.Fatal("chain type required: use -chain flag or set CHAIN_TYPE env var")
	}

	log.Infof("loading chain config: %s", chainType)
	cfg, err := config.LoadChainConfig(chainType)
	if err != nil {
		log.Fatalf("config load failed: %v", err)
	}
	return cfg
}

func initApp(cfg *config.Config) (*application, error) {
	engineCfg := buildEngineConfig(cfg)
	eng := engine.NewEngine(engineCfg)

	storage, err := appinit.CreateStorageEngine(cfg)
	if err != nil {
		return nil, fmt.Errorf("storage init failed: %w", err)
	}
	eng.SetStorageEngine(storage)
	log.Infof("storage engine: %s", cfg.Storage.Type)

	tracker, redisClient, err := appinit.CreateProgressTracker(cfg)
	if err != nil {
		return nil, fmt.Errorf("tracker init failed: %w", err)
	}
	eng.SetProgressTracker(tracker)
	log.Info("progress tracker: redis")

	if err := registerChains(eng, cfg); err != nil {
		return nil, fmt.Errorf("chain registration failed: %w", err)
	}

	if err := registerDexExtractors(eng, cfg, storage, redisClient); err != nil {
		return nil, fmt.Errorf("dex registration failed: %w", err)
	}

	return &application{engine: eng, redisClient: redisClient}, nil
}

func buildEngineConfig(cfg *config.Config) *engine.EngineConfig {
	ec := &engine.EngineConfig{
		BatchSize:        cfg.Processor.BatchSize,
		ProcessInterval:  time.Duration(cfg.Processor.RetryDelay) * time.Second,
		MaxRetries:       cfg.Processor.MaxRetries,
		ConcurrentChains: cfg.Processor.MaxConcurrent,
		RealTimeMode:     true,
		ChainConfigs:     make(map[types.ChainType]*engine.ChainConfig),
	}

	for name, cc := range cfg.Chains {
		var ct types.ChainType
		switch name {
		case "sui":
			ct = types.ChainTypeSui
		case "ethereum":
			ct = types.ChainTypeEthereum
		case "solana":
			ct = types.ChainTypeSolana
		case "bsc":
			ct = types.ChainTypeBSC
		default:
			continue
		}
		ec.ChainConfigs[ct] = &engine.ChainConfig{
			Enabled:     cc.Enabled,
			BatchSize:   cc.BatchSize,
			RpcEndpoint: cc.RPCEndpoint,
		}
	}
	return ec
}

func registerChains(eng *engine.Engine, cfg *config.Config) error {
	if sc, ok := cfg.Chains["sui"]; ok && sc.Enabled {
		p, err := sui.NewSuiProcessor(&sui.SuiConfig{
			RPCEndpoint: sc.RPCEndpoint,
			ChainID:     sc.ChainID,
			BatchSize:   sc.BatchSize,
		})
		if err != nil {
			return fmt.Errorf("sui processor failed: %w", err)
		}
		eng.RegisterChainProcessor(p)
	}

	if ec, ok := cfg.Chains["ethereum"]; ok && ec.Enabled {
		p, err := ethereum.NewEthereumProcessor(&ethereum.EthereumConfig{
			RPCEndpoint: ec.RPCEndpoint,
			ChainID:     1,
			BatchSize:   ec.BatchSize,
			IsTestnet:   false,
		})
		if err != nil {
			return fmt.Errorf("ethereum processor failed: %w", err)
		}
		eng.RegisterChainProcessor(p)
	}

	if bc, ok := cfg.Chains["bsc"]; ok && bc.Enabled {
		p, err := bsc.NewBSCProcessor(&bsc.BSCConfig{
			RPCEndpoint: bc.RPCEndpoint,
			ChainID:     56,
			BatchSize:   bc.BatchSize,
		})
		if err != nil {
			return fmt.Errorf("bsc processor failed: %w", err)
		}
		eng.RegisterChainProcessor(p)
	}

	if sc, ok := cfg.Chains["solana"]; ok && sc.Enabled {
		p, err := solana.NewSolanaProcessor(&solana.SolanaConfig{
			RPCEndpoint: sc.RPCEndpoint,
			ChainID:     sc.ChainID,
			BatchSize:   sc.BatchSize,
			IsTestnet:   false,
		})
		if err != nil {
			return fmt.Errorf("solana processor failed: %w", err)
		}
		eng.RegisterChainProcessor(p)
	}

	return nil
}

func registerDexExtractors(eng *engine.Engine, cfg *config.Config, storage types.StorageEngine, redisClient *redis.Client) error {
	protocolsCfg := make(map[string]interface{})
	for name, proto := range cfg.Protocols {
		if proto.Enabled {
			protocolsCfg[name] = true
		}
	}

	quoteAssets := buildQuoteAssetsMap(cfg)

	// Pre-fetch known pool token addresses once, shared across all extractors
	// that support warmup. This avoids re-running eth_call for every pool on
	// every process restart — see UniswapExtractor.WarmupPoolTokens.
	var poolTokens map[string][2]string
	if storage != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		tokens, err := storage.GetAllPoolTokens(ctx)
		cancel()
		if err != nil {
			log.Warnf("pool token warmup query failed (non-fatal, will fall back to eth_call as needed): %v", err)
		} else {
			poolTokens = tokens
			log.Infof("loaded %d known pool token mappings for warmup", len(poolTokens))
		}
	}

	// Pre-fetch known token metadata (decimals, symbol, name) from
	// dex_tokens — the authoritative, persistent store for token metadata.
	// Without this, a process restart would re-derive all three fields (via
	// eth_call, or silently default to 18 decimals / empty strings) for
	// every token already resolved in a prior run. See
	// UniswapExtractor.WarmupTokenMeta.
	var tokenMeta map[string]model.Token
	if storage != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		meta, err := storage.GetAllTokenMeta(ctx)
		cancel()
		if err != nil {
			log.Warnf("token metadata warmup query failed (non-fatal, will re-derive as needed): %v", err)
		} else {
			tokenMeta = meta
			log.Infof("loaded %d known token metadata entries for warmup", len(tokenMeta))
		}
	}

	// Redis cache warmup runs in the background, not on the startup path.
	// It exists purely to help *other* consumers (a future restart of this
	// same process, or any other process that reads dexcache without its
	// own DB-backed warmup) — it has zero effect on this process's own hot
	// path, since fetchTokenMeta always checks the in-memory cache (just
	// warmed synchronously above) before ever touching Redis. Blocking
	// engine startup on filling Redis with 500k+ entries was purely wasted
	// wall-clock time from this process's own perspective — see
	// warmRedisCacheAsync for what actually runs and cmd/thegraph-sync's
	// own dexcache writes for the other place this cache gets filled.
	//
	// GetAllPools (unlike GetAllPoolTokens/GetAllTokenMeta above) has no
	// synchronous consumer at all — nothing needs it before the engine can
	// start — so its DB read moves into the background too, not just the
	// Redis write.
	if redisClient != nil {
		if dexcache.IsWarmed(context.Background(), redisClient) {
			log.Info("redis dexcache already warmed recently — skipping bulk warmup (per-item TTLs still fresh)")
		} else {
			go warmRedisCacheAsync(storage, redisClient, tokenMeta)
		}
	}

	factory := dexfactory.CreateFactoryWithConfig(protocolsCfg)
	for _, extractor := range factory.GetAllExtractors() {
		if setter, ok := extractor.(dex.QuoteAssetSetter); ok && len(quoteAssets) > 0 {
			setter.SetQuoteAssets(quoteAssets)
		}
		if warmer, ok := extractor.(interface {
			WarmupPoolTokens(map[string][2]string) int
		}); ok && len(poolTokens) > 0 {
			warmer.WarmupPoolTokens(poolTokens)
		}
		if warmer, ok := extractor.(interface {
			WarmupTokenMeta(map[string]model.Token) int
		}); ok && len(tokenMeta) > 0 {
			warmer.WarmupTokenMeta(tokenMeta)
		}
		if setter, ok := extractor.(interface {
			SetTokenCacheRedis(*redis.Client)
		}); ok && redisClient != nil {
			setter.SetTokenCacheRedis(redisClient)
		}
		eng.RegisterDexExtractor(extractor)
	}
	return nil
}

// warmRedisCacheAsync fills dexcache (Redis) with every known token and
// pool from storage. Called as its own goroutine from
// registerDexExtractors — see the call site for why this must never be on
// the startup path. Uses its own long-lived background context rather than
// one tied to registerDexExtractors' stack frame, since by the time this
// runs, that function has already returned and the engine may already be
// processing blocks.
//
// tokenMeta is passed in (already fetched synchronously for the in-memory
// warmup) to avoid a second identical DB query; pools has no such
// synchronous counterpart, so it's queried fresh here.
func warmRedisCacheAsync(storage types.StorageEngine, redisClient *redis.Client, tokenMeta map[string]model.Token) {
	start := time.Now()
	// defer, not a call at the end of the happy path: every early return
	// below (storage nil, query failure, no pools) is still "a warmup ran"
	// from the marker's point of view — a failed/partial attempt
	// shouldn't force every subsequent restart to redundantly retry the
	// same thing. Individual cache misses still fall through to their
	// normal resolution path regardless of whether the marker is set.
	defer dexcache.MarkWarmed(context.Background(), redisClient)

	if len(tokenMeta) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		failed := dexcache.WarmTokens(ctx, redisClient, tokenMeta)
		cancel()
		if failed > 0 {
			log.Warnf("[warmup] redis token cache: %d/%d entries cached, %d failed", len(tokenMeta)-failed, len(tokenMeta), failed)
		} else {
			log.Infof("[warmup] redis token cache warmed with %d entries", len(tokenMeta))
		}
	}

	if storage == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	pools, err := storage.GetAllPools(ctx)
	cancel()
	if err != nil {
		log.Warnf("[warmup] pool cache query failed (non-fatal, will fill in as pools are synced/discovered): %v", err)
		return
	}
	if len(pools) == 0 {
		return
	}

	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Minute)
	failed := dexcache.WarmPools(ctx, redisClient, pools)
	cancel()
	if failed > 0 {
		log.Warnf("[warmup] redis pool cache: %d/%d entries cached, %d failed", len(pools)-failed, len(pools), failed)
	} else {
		log.Infof("[warmup] redis pool cache warmed with %d entries", len(pools))
	}

	log.Infof("[warmup] redis cache warmup finished in %s", time.Since(start).Round(time.Second))
}

func buildQuoteAssetsMap(cfg *config.Config) map[string]int {
	if len(cfg.QuoteAssets) == 0 {
		return nil
	}
	m := make(map[string]int, len(cfg.QuoteAssets))
	for _, qa := range cfg.QuoteAssets {
		m[qa.Addr] = qa.Rank
	}
	return m
}
