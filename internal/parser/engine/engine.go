package engine

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"unified-tx-parser/internal/model"
	"unified-tx-parser/internal/types"
)

var log = logrus.WithFields(logrus.Fields{"service": "parser", "module": "engine"})

// SuiProcessorInjectable is implemented by DEX extractors that need a reference
// to the Sui chain processor for on-chain queries during extraction.
type SuiProcessorInjectable interface {
	SetSuiProcessor(processor interface{})
}

// Engine orchestrates block fetching, DEX data extraction, and storage across
// multiple blockchain networks.
type Engine struct {
	chainProcessors map[types.ChainType]types.ChainProcessor
	dexExtractors   []types.DexExtractors
	storage         types.StorageEngine
	progressTracker types.ProgressTracker
	config          *EngineConfig

	running bool
	mu      sync.RWMutex
	ctx     context.Context
	cancel  context.CancelFunc
}

// EngineConfig holds tuning parameters for the processing engine.
type EngineConfig struct {
	BatchSize        int                              `json:"batch_size"`
	ProcessInterval  time.Duration                    `json:"process_interval"`
	MaxRetries       int                              `json:"max_retries"`
	ConcurrentChains int                              `json:"concurrent_chains"`
	RealTimeMode     bool                             `json:"real_time_mode"`
	ChainConfigs     map[types.ChainType]*ChainConfig `json:"chain_configs"`
}

// ChainConfig holds per-chain overrides for block processing.
type ChainConfig struct {
	Enabled     bool   `json:"enabled"`
	StartBlock  int64  `json:"start_block"`
	BatchSize   int    `json:"batch_size"`
	RpcEndpoint string `json:"rpc_endpoint"`
}

// NewEngine creates a new processing engine with the given config.
// If config is nil, sensible defaults are used.
func NewEngine(config *EngineConfig) *Engine {
	ctx, cancel := context.WithCancel(context.Background())

	if config == nil {
		config = &EngineConfig{
			BatchSize:        100,
			ProcessInterval:  time.Second * 10,
			MaxRetries:       3,
			ConcurrentChains: 4,
			RealTimeMode:     true,
		}
	}

	return &Engine{
		chainProcessors: make(map[types.ChainType]types.ChainProcessor),
		dexExtractors:   make([]types.DexExtractors, 0),
		config:          config,
		ctx:             ctx,
		cancel:          cancel,
	}
}

// RegisterChainProcessor adds a chain processor and injects it into any
// already-registered DEX extractors that support the same chain.
func (e *Engine) RegisterChainProcessor(processor types.ChainProcessor) {
	e.mu.Lock()
	defer e.mu.Unlock()

	chainType := processor.GetChainType()
	e.chainProcessors[chainType] = processor

	// 为已注册的DEX提取器注入链处理器
	e.injectChainProcessorToExtractors(chainType, processor)

	log.Infof("registered chain processor: %s", chainType)
}

// RegisterDexExtractor adds a DEX data extractor and injects any
// already-registered chain processors it needs.
func (e *Engine) RegisterDexExtractor(extractor types.DexExtractors) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// 检查是否需要注入链处理器
	e.injectChainProcessors(extractor)

	e.dexExtractors = append(e.dexExtractors, extractor)

	protocols := extractor.GetSupportedProtocols()
	chains := extractor.GetSupportedChains()
	log.Infof("registered dex extractor: protocols=%v, chains=%v", protocols, chains)
}

// SetStorageEngine configures the storage backend for persisting parsed data.
func (e *Engine) SetStorageEngine(storage types.StorageEngine) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.storage = storage
}

// SetProgressTracker configures the progress tracker for checkpoint management.
func (e *Engine) SetProgressTracker(tracker types.ProgressTracker) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.progressTracker = tracker
	log.Infof("progress tracker configured")
}

// Start launches a goroutine for each registered chain processor.
func (e *Engine) Start() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return fmt.Errorf("引擎已经在运行")
	}

	if e.storage == nil {
		return fmt.Errorf("未设置存储引擎")
	}

	if len(e.chainProcessors) == 0 {
		return fmt.Errorf("未注册任何链处理器")
	}

	e.running = true

	// 启动每个链的处理协程
	for chainType, processor := range e.chainProcessors {
		if config, exists := e.config.ChainConfigs[chainType]; exists && !config.Enabled {
			log.Warnf("chain %s disabled", chainType)
			continue
		}

		go e.processChain(chainType, processor)
		log.Infof("started chain processor: %s", chainType)
	}

	log.Infof("unified transaction processing engine started")
	return nil
}

// Stop gracefully shuts down all chain processors.
func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return
	}

	e.running = false
	e.cancel()

	log.Infof("unified transaction processing engine stopped")
}

// IsRunning reports whether the engine is currently processing blocks.
func (e *Engine) IsRunning() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.running
}

// processChain 处理单个链的数据
func (e *Engine) processChain(chainType types.ChainType, processor types.ChainProcessor) {
	ticker := time.NewTicker(e.config.ProcessInterval)
	defer ticker.Stop()

	for {
		select {
		case <-e.ctx.Done():
			log.Infof("chain %s processor stopped", chainType)
			return
		case <-ticker.C:
			if err := e.processChainBatch(chainType, processor); err != nil {
				log.Errorf("chain %s processing error: %v", chainType, err)
			}
		}
	}
}

// processChainBatch 处理链的一批数据
func (e *Engine) processChainBatch(chainType types.ChainType, processor types.ChainProcessor) error {
	// 获取当前进度
	progress, err := e.getOrCreateProgress(chainType)
	if err != nil {
		return fmt.Errorf("获取进度失败: %w", err)
	}

	// 获取最新区块号
	latestBlock, err := processor.GetLatestBlockNumber(e.ctx)
	if err != nil {
		return fmt.Errorf("获取最新区块号失败: %w", err)
	}

	// 计算处理范围
	startBlock := progress.LastProcessedBlock

	if startBlock.Cmp(big.NewInt(0)) == 0 {
		// 如果没有处理过，从配置的起始块开始
		if config, exists := e.config.ChainConfigs[chainType]; exists && config.StartBlock > 0 {
			startBlock = big.NewInt(config.StartBlock)
		} else {
			startBlock = big.NewInt(0).Sub(latestBlock, big.NewInt(100)) // 默认从最新块往前100块开始
		}
	} else {
		// 从下一个区块开始处理（避免重复处理）
		startBlock = big.NewInt(0).Add(startBlock, big.NewInt(1))
	}

	// 确定批处理大小
	batchSize := e.config.BatchSize
	if config, exists := e.config.ChainConfigs[chainType]; exists && config.BatchSize > 0 {
		batchSize = config.BatchSize
	}

	endBlock := big.NewInt(0).Add(startBlock, big.NewInt(int64(batchSize)))
	if endBlock.Cmp(latestBlock) > 0 {
		endBlock = latestBlock
	}

	// 简化的处理范围日志
	log.Infof("[%s] blocks %s-%s", strings.ToUpper(string(chainType)), startBlock.String(), endBlock.String())

	// 如果没有新块需要处理
	if startBlock.Cmp(endBlock) >= 0 {
		return nil
	}

	log.Infof("processing chain %s: blocks %s to %s", chainType, startBlock.String(), endBlock.String())

	// 获取交易数据 - 添加超时控制
	txCtx, txCancel := context.WithTimeout(e.ctx, time.Minute*5) // 5分钟超时
	defer txCancel()

	log.Infof("fetching block data...")
	startTime := time.Now()

	blocks, err := processor.GetBlocksByRange(txCtx, startBlock, endBlock)
	if err != nil {
		return fmt.Errorf("获取区块数据失败: %w", err)
	}

	duration := time.Since(startTime)
	// 简化日志：只在有区块时输出
	if len(blocks) > 0 {
		totalTxs := 0
		for _, block := range blocks {
			totalTxs += len(block.Transactions)
		}
		log.Infof("[%s] %d blocks, %d transactions (%.1fs)", strings.ToUpper(string(chainType)), len(blocks), totalTxs, duration.Seconds())
	}
	// 如果没有区块数据，直接返回
	if len(blocks) == 0 {
		return nil
	}

	// 提取DEX数据
	log.Infof("extracting dex data...")
	extractStartTime := time.Now()

	dexData, err := e.extractDexData(blocks)
	if err != nil {
		return fmt.Errorf("提取DEX数据失败: %w", err)
	}

	extractDuration := time.Since(extractStartTime)
	totalDexRecords := len(dexData.Pools) + len(dexData.Transactions) + len(dexData.Liquidities) + len(dexData.Reserves) + len(dexData.Tokens)

	if totalDexRecords > 0 {
		log.Infof("[%s] dex data extracted: %d records (%.1fs)",
			strings.ToUpper(string(chainType)), totalDexRecords, extractDuration.Seconds())
		log.Infof("  pools: %d, transactions: %d, liquidities: %d, reserves: %d, tokens: %d",
			len(dexData.Pools), len(dexData.Transactions), len(dexData.Liquidities),
			len(dexData.Reserves), len(dexData.Tokens))
	}

	// 存储数据
	log.Infof("storing data...")
	storeStartTime := time.Now()

	// 存储区块元数据(链无关:chain_type/chain_id 已在 UnifiedBlock 内)
	if err := e.storage.StoreBlocks(e.ctx, blocks); err != nil {
		return fmt.Errorf("存储区块数据失败: %w", err)
	}

	// 存储原始交易(从各区块展开;dex_transactions.hash 可据此跨表关联)
	allTxs := make([]types.UnifiedTransaction, 0)
	for _, block := range blocks {
		allTxs = append(allTxs, block.Transactions...)
	}
	if len(allTxs) > 0 {
		if err := e.storage.StoreTransactions(e.ctx, allTxs); err != nil {
			return fmt.Errorf("存储交易数据失败: %w", err)
		}
	}

	// 存储DEX数据
	if totalDexRecords > 0 {
		if err := e.storage.StoreDexData(e.ctx, dexData); err != nil {
			return fmt.Errorf("存储DEX数据失败: %w", err)
		}
	}

	storeDuration := time.Since(storeStartTime)
	if totalDexRecords > 0 {
		log.Infof("[%s] stored: %d dex records (%.1fs)",
			strings.ToUpper(string(chainType)), totalDexRecords, storeDuration.Seconds())
	}

	// 更新进度
	progress.LastProcessedBlock = endBlock
	progress.LastUpdateTime = time.Now()

	// 计算总交易数
	totalTxs := 0
	for _, block := range blocks {
		totalTxs += len(block.Transactions)
	}

	progress.TotalTransactions += int64(totalTxs)
	progress.TotalEvents += int64(totalDexRecords)

	if e.progressTracker != nil {
		if err := e.progressTracker.UpdateProgress(chainType, progress); err != nil {
			log.Warnf("%s: failed to save progress - %v", chainType, err)
		}
	}

	// 完成日志
	if len(blocks) > 0 || totalDexRecords > 0 {
		log.Infof("[%s] batch complete: %d blocks, %d transactions, %d dex records (block: %s)",
			strings.ToUpper(string(chainType)), len(blocks), totalTxs, totalDexRecords, endBlock.String())
	}

	return nil
}

// extractDexData 从区块中提取DEX数据
func (e *Engine) extractDexData(blocks []types.UnifiedBlock) (*types.DexData, error) {
	// 创建一个空的DexData结构
	result := &types.DexData{
		Pools:        make([]model.Pool, 0),
		Transactions: make([]model.Transaction, 0),
		Liquidities:  make([]model.Liquidity, 0),
		Reserves:     make([]model.Reserve, 0),
		Tokens:       make([]model.Token, 0),
	}

	// 遍历所有DEX提取器
	for _, extractor := range e.dexExtractors {
		// 检查是否有支持的区块
		supportedBlocks := make([]types.UnifiedBlock, 0)
		for _, block := range blocks {
			if extractor.SupportsBlock(&block) {
				supportedBlocks = append(supportedBlocks, block)
			}
		}

		if len(supportedBlocks) == 0 {
			continue
		}

		// 提取DEX数据
		dexData, err := extractor.ExtractDexData(e.ctx, supportedBlocks)
		if err != nil {
			return nil, fmt.Errorf("DEX提取器提取数据失败: %w", err)
		}

		// 合并数据
		result.Pools = append(result.Pools, dexData.Pools...)
		result.Transactions = append(result.Transactions, dexData.Transactions...)
		result.Liquidities = append(result.Liquidities, dexData.Liquidities...)
		result.Reserves = append(result.Reserves, dexData.Reserves...)
		result.Tokens = append(result.Tokens, dexData.Tokens...)
	}

	return result, nil
}

// getOrCreateProgress 获取或创建处理进度
func (e *Engine) getOrCreateProgress(chainType types.ChainType) (*types.ProcessProgress, error) {
	if e.progressTracker == nil {
		log.Warnf("chain %s has no progress tracker, using default", chainType)
		return &types.ProcessProgress{
			ChainType:          chainType,
			LastProcessedBlock: big.NewInt(0),
			LastUpdateTime:     time.Now(),
		}, nil
	}

	progress, err := e.progressTracker.GetProgress(chainType)
	if err != nil {
		log.Warnf("chain %s failed to get progress: %v, creating new", chainType, err)
		// 如果没有找到进度记录，创建新的
		progress = &types.ProcessProgress{
			ChainType:          chainType,
			LastProcessedBlock: big.NewInt(0),
			LastUpdateTime:     time.Now(),
		}
	} else {
		log.Infof("chain %s progress loaded: last_block=%s", chainType, progress.LastProcessedBlock.String())
	}

	return progress, nil
}

// GetStats returns a snapshot of engine state including storage stats and chain progress.
func (e *Engine) GetStats(ctx context.Context) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	e.mu.RLock()
	stats["running"] = e.running
	stats["registered_chains"] = len(e.chainProcessors)
	stats["registered_extractors"] = len(e.dexExtractors)
	e.mu.RUnlock()

	// 获取存储统计
	if e.storage != nil {
		storageStats, err := e.storage.GetStorageStats(ctx)
		if err == nil {
			stats["storage"] = storageStats
		}
	}

	// 获取链进度
	if e.progressTracker != nil {
		chainProgress := make(map[string]*types.ProcessProgress)
		for chainType := range e.chainProcessors {
			if progress, err := e.progressTracker.GetProgress(chainType); err == nil {
				chainProgress[string(chainType)] = progress
			}
		}
		stats["chain_progress"] = chainProgress
	}

	return stats, nil
}

// injectChainProcessors 为DEX提取器注入对应的链处理器
func (e *Engine) injectChainProcessors(extractor types.DexExtractors) {
	// 检查提取器支持的链类型
	supportedChains := extractor.GetSupportedChains()

	for _, chainType := range supportedChains {
		// 获取对应的链处理器
		if processor, exists := e.chainProcessors[chainType]; exists {
			// 检查是否是可注入Sui处理器的提取器
			if injectable, ok := extractor.(SuiProcessorInjectable); ok && chainType == types.ChainTypeSui {
				injectable.SetSuiProcessor(processor)
				log.Infof("injected %s chain processor into dex extractor", chainType)
			}
		}
	}
}

// injectChainProcessorToExtractors 为已注册的DEX提取器注入新注册的链处理器
func (e *Engine) injectChainProcessorToExtractors(chainType types.ChainType, processor types.ChainProcessor) {
	for _, extractor := range e.dexExtractors {
		// 检查提取器是否支持该链类型
		supportedChains := extractor.GetSupportedChains()
		for _, supportedChain := range supportedChains {
			if supportedChain == chainType {
				// 检查是否是可注入Sui处理器的提取器
				if injectable, ok := extractor.(SuiProcessorInjectable); ok && chainType == types.ChainTypeSui {
					injectable.SetSuiProcessor(processor)
					log.Infof("injected %s chain processor into registered dex extractor", chainType)
				}
				break
			}
		}
	}
}
