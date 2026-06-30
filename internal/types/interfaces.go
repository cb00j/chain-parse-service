package types

import (
	"context"
	"math/big"
	"time"

	"unified-tx-parser/internal/model"
)

// ChainType represents a supported blockchain network identifier.
type ChainType string

const (
	ChainTypeEthereum ChainType = "ethereum"
	ChainTypeBSC      ChainType = "bsc"
	ChainTypeSolana   ChainType = "solana"
	ChainTypeSui      ChainType = "sui"
)

// TransactionStatus indicates the outcome of an on-chain transaction.
type TransactionStatus string

const (
	TransactionStatusSuccess TransactionStatus = "success"
	TransactionStatusFailed  TransactionStatus = "failed"
	TransactionStatusPending TransactionStatus = "pending"
)

// UnifiedBlock is a chain-agnostic representation of a blockchain block,
// including its transactions and events.
type UnifiedBlock struct {
	BlockNumber *big.Int  `json:"block_number"`
	BlockHash   string    `json:"block_hash"`
	ChainType   ChainType `json:"chain_type"`
	ChainID     string    `json:"chain_id"`

	ParentHash string    `json:"parent_hash"`
	Timestamp  time.Time `json:"timestamp"`
	Difficulty *big.Int  `json:"difficulty,omitempty"`
	GasLimit   *big.Int  `json:"gas_limit,omitempty"`
	GasUsed    *big.Int  `json:"gas_used,omitempty"`
	Miner      string    `json:"miner,omitempty"`
	Nonce      string    `json:"nonce,omitempty"`
	Size       int64     `json:"size,omitempty"`
	TxCount    int       `json:"tx_count"`

	Transactions []UnifiedTransaction `json:"transactions"`
	Events       []UnifiedEvent       `json:"events"`
	RawData      interface{}          `json:"raw_data"`
}

// UnifiedEvent is a chain-agnostic representation of an on-chain event (log).
type UnifiedEvent struct {
	EventID     string    `json:"event_id"`
	ChainType   ChainType `json:"chain_type"`
	ChainID     string    `json:"chain_id"`
	BlockNumber *big.Int  `json:"block_number"`
	BlockHash   string    `json:"block_hash"`

	TxHash  string `json:"tx_hash"`
	TxIndex int    `json:"tx_index"`

	EventIndex int         `json:"event_index"`
	EventType  string      `json:"event_type"`
	Address    string      `json:"address"`
	Topics     []string    `json:"topics,omitempty"`
	Data       interface{} `json:"data"`

	Timestamp time.Time   `json:"timestamp"`
	RawData   interface{} `json:"raw_data"`
}

// UnifiedTransaction is a chain-agnostic representation of a blockchain transaction.
type UnifiedTransaction struct {
	TxHash    string    `json:"tx_hash"`
	ChainType ChainType `json:"chain_type"`
	ChainID   string    `json:"chain_id"`

	BlockNumber *big.Int          `json:"block_number"`
	BlockHash   string            `json:"block_hash"`
	TxIndex     int               `json:"tx_index"`
	FromAddress string            `json:"from_address"`
	ToAddress   string            `json:"to_address"`
	Value       *big.Int          `json:"value"`
	GasLimit    *big.Int          `json:"gas_limit"`
	GasUsed     *big.Int          `json:"gas_used"`
	GasPrice    *big.Int          `json:"gas_price"`
	Status      TransactionStatus `json:"status"`

	Timestamp time.Time   `json:"timestamp"`
	RawData   interface{} `json:"raw_data"`
}

// ChainProcessor defines the interface that every blockchain adapter must implement
// to fetch blocks, transactions, and perform health checks.
type ChainProcessor interface {
	GetChainType() ChainType
	GetChainID() string
	GetLatestBlockNumber(ctx context.Context) (*big.Int, error)
	GetBlocksByRange(ctx context.Context, startBlock, endBlock *big.Int) ([]UnifiedBlock, error)
	GetBlock(ctx context.Context, blockNumber *big.Int) (*UnifiedBlock, error)
	GetTransaction(ctx context.Context, txHash string) (*UnifiedTransaction, error)
	HealthCheck(ctx context.Context) error
}

// DexData aggregates all data types extracted from DEX protocol events.
type DexData struct {
	Pools        []model.Pool        `json:"pools"`
	Transactions []model.Transaction `json:"transactions"`
	Liquidities  []model.Liquidity   `json:"liquidities"`
	Reserves     []model.Reserve     `json:"reserves"`
	Tokens       []model.Token       `json:"tokens"`
}

// DexExtractors defines the interface for extracting DEX-specific data
// (swaps, liquidity events, pool creations) from unified block data.
type DexExtractors interface {
	GetSupportedProtocols() []string
	GetSupportedChains() []ChainType
	ExtractDexData(ctx context.Context, blocks []UnifiedBlock) (*DexData, error)
	SupportsBlock(block *UnifiedBlock) bool
}

// StorageEngine defines the interface for persisting and querying parsed blockchain data.
type StorageEngine interface {
	StoreBlocks(ctx context.Context, blocks []UnifiedBlock) error
	StoreTransactions(ctx context.Context, txs []UnifiedTransaction) error
	StoreDexData(ctx context.Context, dexData *DexData) error

	GetTransactionsByHash(ctx context.Context, hashes []string) ([]UnifiedTransaction, error)

	// GetAllPoolTokens returns addr -> {token0, token1} for every pool with
	// known token addresses. Used at startup to warm up the in-memory pool
	// cache so a process restart doesn't re-trigger eth_call lookups for
	// pools that were already resolved before the restart.
	GetAllPoolTokens(ctx context.Context) (map[string][2]string, error)

	GetStorageStats(ctx context.Context) (map[string]interface{}, error)
	HealthCheck(ctx context.Context) error
	Close() error
}

// ProgressTracker tracks block-processing progress, errors, and performance
// metrics for each chain.
type ProgressTracker interface {
	GetProgress(chainType ChainType) (*ProcessProgress, error)
	UpdateProgress(chainType ChainType, progress *ProcessProgress) error
	ResetProgress(chainType ChainType) error

	GetAllProgress() (map[ChainType]*ProcessProgress, error)
	UpdateMultipleProgress(progresses map[ChainType]*ProcessProgress) error

	GetProcessingStats(chainType ChainType) (*ProcessingStats, error)
	GetGlobalStats() (*GlobalProcessingStats, error)

	SetProcessingStatus(chainType ChainType, status ProcessingStatus) error
	GetProcessingStatus(chainType ChainType) (ProcessingStatus, error)

	RecordError(chainType ChainType, err error) error
	GetErrorHistory(chainType ChainType, limit int) ([]ProcessingError, error)
	ClearErrorHistory(chainType ChainType) error

	RecordProcessingMetrics(chainType ChainType, metrics *ProcessingMetrics) error
	GetPerformanceMetrics(chainType ChainType, duration time.Duration) (*PerformanceReport, error)

	HealthCheck() error
	Cleanup(olderThan time.Duration) error
}

// ProcessProgress records the current processing state for a single chain.
type ProcessProgress struct {
	ChainType          ChainType        `json:"chain_type"`
	LastProcessedBlock *big.Int         `json:"last_processed_block"`
	LastUpdateTime     time.Time        `json:"last_update_time"`
	TotalTransactions  int64            `json:"total_transactions"`
	TotalEvents        int64            `json:"total_events"`
	ProcessingStatus   ProcessingStatus `json:"processing_status"`
	StartTime          time.Time        `json:"start_time"`
	LastErrorTime      time.Time        `json:"last_error_time"`
	ErrorCount         int64            `json:"error_count"`
	SuccessRate        float64          `json:"success_rate"`
}

// ProcessingStatus represents the lifecycle state of a chain processor.
type ProcessingStatus string

const (
	ProcessingStatusIdle       ProcessingStatus = "idle"
	ProcessingStatusRunning    ProcessingStatus = "running"
	ProcessingStatusPaused     ProcessingStatus = "paused"
	ProcessingStatusError      ProcessingStatus = "error"
	ProcessingStatusStopped    ProcessingStatus = "stopped"
	ProcessingStatusSyncing    ProcessingStatus = "syncing"
	ProcessingStatusCatchingUp ProcessingStatus = "catching_up"
)

// ProcessingStats holds throughput and latency metrics for a chain processor.
type ProcessingStats struct {
	ChainType          ChainType     `json:"chain_type"`
	ProcessingDuration time.Duration `json:"processing_duration"`
	BlocksProcessed    int64         `json:"blocks_processed"`
	TransactionsPerSec float64       `json:"transactions_per_sec"`
	EventsPerSec       float64       `json:"events_per_sec"`
	AverageBlockTime   time.Duration `json:"average_block_time"`
	LastProcessingTime time.Duration `json:"last_processing_time"`
	ErrorRate          float64       `json:"error_rate"`
	RetryCount         int64         `json:"retry_count"`
}

// GlobalProcessingStats aggregates metrics across all active chain processors.
type GlobalProcessingStats struct {
	TotalChains        int                            `json:"total_chains"`
	ActiveChains       int                            `json:"active_chains"`
	TotalTransactions  int64                          `json:"total_transactions"`
	TotalEvents        int64                          `json:"total_events"`
	OverallSuccessRate float64                        `json:"overall_success_rate"`
	ChainStats         map[ChainType]*ProcessingStats `json:"chain_stats"`
	LastUpdateTime     time.Time                      `json:"last_update_time"`
	SystemUptime       time.Duration                  `json:"system_uptime"`
}

// ProcessingError captures a single processing failure for diagnostics and retry tracking.
type ProcessingError struct {
	ChainType   ChainType `json:"chain_type"`
	ErrorTime   time.Time `json:"error_time"`
	ErrorType   string    `json:"error_type"`
	ErrorMsg    string    `json:"error_msg"`
	BlockNumber *big.Int  `json:"block_number"`
	TxHash      string    `json:"tx_hash"`
	RetryCount  int       `json:"retry_count"`
	Resolved    bool      `json:"resolved"`
}

// ProcessingMetrics is a point-in-time snapshot of resource usage during block processing.
type ProcessingMetrics struct {
	ChainType        ChainType     `json:"chain_type"`
	Timestamp        time.Time     `json:"timestamp"`
	BlockNumber      *big.Int      `json:"block_number"`
	ProcessingTime   time.Duration `json:"processing_time"`
	TransactionCount int           `json:"transaction_count"`
	EventCount       int           `json:"event_count"`
	MemoryUsage      int64         `json:"memory_usage"`
	CPUUsage         float64       `json:"cpu_usage"`
}

// PerformanceReport summarizes processing performance over a given time window.
type PerformanceReport struct {
	ChainType          ChainType     `json:"chain_type"`
	ReportPeriod       time.Duration `json:"report_period"`
	AverageProcessTime time.Duration `json:"average_process_time"`
	MaxProcessTime     time.Duration `json:"max_process_time"`
	MinProcessTime     time.Duration `json:"min_process_time"`
	TotalTransactions  int64         `json:"total_transactions"`
	TotalEvents        int64         `json:"total_events"`
	AverageMemoryUsage int64         `json:"average_memory_usage"`
	AverageCPUUsage    float64       `json:"average_cpu_usage"`
	ThroughputTPS      float64       `json:"throughput_tps"`
	ErrorCount         int64         `json:"error_count"`
}
