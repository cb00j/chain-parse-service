// Command thegraph-sync runs the periodic Uniswap subgraph prefetch job as
// its own process, independent of the parser.
//
// Why a separate process rather than a goroutine inside cmd/parser: the two
// are genuinely independent pieces of business logic. The parser scans
// blocks and extracts DEX events in (near) real time; this job periodically
// bulk-imports pool/token metadata from The Graph as a data-quality
// improvement (dex_pools/dex_tokens ahead of on-chain discovery). They
// don't share request-time state, a crash or slow subgraph response in one
// shouldn't affect the other's uptime/restart policy, and they scale/deploy
// differently — this can run as a single low-resource instance (or even a
// cron-triggered one-shot via -once) while N parser instances run per
// chain. Keeping them as separate binaries means separate restart policies,
// separate logs, and no risk of a subgraph outage ever touching the
// block-scanning hot path.
package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	appinit "unified-tx-parser/internal/app"
	"unified-tx-parser/internal/config"
	"unified-tx-parser/internal/logger"
	"unified-tx-parser/internal/thegraph"
)

// defaultSyncInterval is used when thegraph.enabled but sync_interval is
// left at its zero value — Config.Validate() only rejects negative
// intervals, so 0 is technically valid config and shouldn't mean "sync in
// a tight loop."
const defaultSyncInterval = 5 * time.Minute

var (
	version = "dev"
	commit  = "unknown"
)

var log = logger.New("thegraph-sync", "main")

func main() {
	chainType, once := parseArgs()
	cfg := loadConfig(chainType)

	logger.SetLevel(cfg.Logging.Level)
	log.Infof("thegraph-sync service starting (version=%s, commit=%s, chain=%s)", version, commit, chainType)

	if !cfg.TheGraph.Enabled {
		log.Fatal("thegraph.enabled=false in config — nothing to do (this process only makes sense when it's on)")
	}

	if chainType != "ethereum" {
		// Not a hard error: NewSyncer doesn't care what chainType string
		// you pass it, it's just a tag on the cursor rows. But
		// internal/thegraph today only speaks the Uniswap V2/V3
		// subgraphs, so pointing this at any other chain will just fail
		// every sync (wrong/empty endpoints) until a PancakeSwap/etc.
		// subgraph client is added.
		log.Warnf("chain=%s: internal/thegraph currently only implements Uniswap (ethereum) subgraphs — this will likely fail unless you've configured non-Uniswap endpoints", chainType)
	}

	storage, err := appinit.CreateStorageEngine(cfg)
	if err != nil {
		log.Fatalf("storage init failed: %v", err)
	}
	defer storage.Close()
	log.Infof("storage engine: %s", cfg.Storage.Type)

	cursors, err := appinit.CreateCursorStore(cfg)
	if err != nil {
		log.Fatalf("cursor store init failed: %v", err)
	}

	syncer, err := thegraph.NewSyncer(cfg.TheGraph, chainType, storage, cursors)
	if err != nil {
		log.Fatalf("syncer init failed: %v", err)
	}

	if once {
		runOnce(syncer)
		return
	}

	runLoop(cfg, syncer)
}

// parseArgs reads -chain (falling back to CHAIN_TYPE, same precedence as
// cmd/parser) and -once (run a single sync pass and exit — useful for a
// cron/k8s-CronJob deployment instead of a long-running loop).
func parseArgs() (chainType string, once bool) {
	var chain string
	flag.StringVar(&chain, "chain", "", "chain type to sync (currently only: ethereum)")
	flag.BoolVar(&once, "once", false, "run a single sync pass and exit, instead of looping on sync_interval")
	flag.Parse()

	if chain == "" {
		chain = os.Getenv("CHAIN_TYPE")
	}
	return strings.ToLower(chain), once
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

// runOnce runs a single sync pass with a bounded timeout and exits via the
// caller — used for -once / cron-style invocations.
func runOnce(syncer *thegraph.Syncer) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	if err := syncer.SyncOnce(ctx); err != nil {
		log.Fatalf("sync failed: %v", err)
	}
	log.Info("sync completed")
}

// runLoop runs the periodic sync: once immediately on startup (so a freshly
// deployed instance doesn't sit with stale/empty data for a full interval),
// then on every tick, until SIGINT/SIGTERM.
func runLoop(cfg *config.Config, syncer *thegraph.Syncer) {
	interval := time.Duration(cfg.TheGraph.SyncInterval) * time.Second
	if interval <= 0 {
		interval = defaultSyncInterval
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	log.Infof("starting periodic sync loop (interval=%s)", interval)
	syncOnceWithTimeout(ctx, syncer)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-quit:
			log.Info("shutdown signal received, stopping")
			return
		case <-ticker.C:
			syncOnceWithTimeout(ctx, syncer)
		}
	}
}

// syncOnceWithTimeout bounds a single sync pass independently of the
// interval, so a slow subgraph response can't stall the next tick
// indefinitely.
func syncOnceWithTimeout(parent context.Context, syncer *thegraph.Syncer) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()

	if err := syncer.SyncOnce(ctx); err != nil {
		log.Warnf("sync pass failed (will retry next interval): %v", err)
	}
}
