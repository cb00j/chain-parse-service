package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	appinit "unified-tx-parser/internal/app"
	"unified-tx-parser/internal/config"
	"unified-tx-parser/internal/logger"
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
	chainType := parseArgs()
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

func loadConfig(chainType string) *config.Config {
	if chainType == "" {
		chainType = os.Getenv("CHAIN_TYPE")
	}
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

	if err := registerDexExtractors(eng, cfg); err != nil {
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

func registerDexExtractors(eng *engine.Engine, cfg *config.Config) error {
	protocolsCfg := make(map[string]interface{})
	for name, proto := range cfg.Protocols {
		if proto.Enabled {
			protocolsCfg[name] = true
		}
	}

	quoteAssets := buildQuoteAssetsMap(cfg)

	factory := dexfactory.CreateFactoryWithConfig(protocolsCfg)
	for _, extractor := range factory.GetAllExtractors() {
		if setter, ok := extractor.(dex.QuoteAssetSetter); ok && len(quoteAssets) > 0 {
			setter.SetQuoteAssets(quoteAssets)
		}
		eng.RegisterDexExtractor(extractor)
	}
	return nil
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
