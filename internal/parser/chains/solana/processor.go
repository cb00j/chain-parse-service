package solana

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"unified-tx-parser/internal/parser/chains/base"
	"unified-tx-parser/internal/types"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/sirupsen/logrus"
)

// maxSupportedTxVersion is set to 0 to support both legacy and V0 versioned transactions.
// Without this, the RPC will reject blocks containing V0 transactions.
var maxSupportedTxVersion uint64 = 0

// SolanaProcessor Solana链处理器
type SolanaProcessor struct {
	base.Processor
	client  *rpc.Client
	chainID string
	config  *SolanaConfig
}

// SolanaConfig Solana配置
type SolanaConfig struct {
	RPCEndpoint string `json:"rpc_endpoint"`
	ChainID     string `json:"chain_id"`
	BatchSize   int    `json:"batch_size"`
	IsTestnet   bool   `json:"is_testnet"`
}

// NewSolanaProcessor 创建新的Solana处理器
func NewSolanaProcessor(config *SolanaConfig) (*SolanaProcessor, error) {
	if config.BatchSize <= 0 {
		config.BatchSize = 100
	}

	client := rpc.New(config.RPCEndpoint)

	processor := &SolanaProcessor{
		Processor: base.NewProcessor(types.ChainTypeSolana, config.RPCEndpoint, config.BatchSize),
		client:    client,
		chainID:   config.ChainID,
		config:    config,
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	if err := processor.HealthCheck(ctx); err != nil {
		return nil, fmt.Errorf("solana connection test failed: %w", err)
	}

	processor.Log.Infof("solana processor initialized: %s", config.RPCEndpoint)
	return processor, nil
}

// GetChainID 获取链ID
func (s *SolanaProcessor) GetChainID() string {
	return s.chainID
}

// GetLatestBlockNumber 获取最新slot
func (s *SolanaProcessor) GetLatestBlockNumber(ctx context.Context) (*big.Int, error) {
	var slot uint64
	err := base.Retry(ctx, s.Log, "get-latest-slot", s.Retry, func() error {
		var rpcErr error
		slot, rpcErr = s.client.GetSlot(ctx, rpc.CommitmentFinalized)
		return rpcErr
	})
	if err != nil {
		return nil, fmt.Errorf("get latest slot failed: %w", err)
	}
	return big.NewInt(int64(slot)), nil
}

// GetTransactionsByBlockRange 批量获取交易
func (s *SolanaProcessor) GetTransactionsByBlockRange(ctx context.Context, startBlock, endBlock *big.Int) ([]types.UnifiedTransaction, error) {
	var allTransactions []types.UnifiedTransaction

	for slot := startBlock.Uint64(); slot <= endBlock.Uint64(); slot++ {
		block, err := s.getBlockWithRetry(ctx, slot)
		if err != nil {
			s.Log.WithFields(logrus.Fields{"slot": slot}).Warnf("failed to get block: %v", err)
			continue
		}
		if block == nil || len(block.Transactions) == 0 {
			continue
		}

		timestamp := blockTimestamp(block)
		blockHash := block.Blockhash.String()

		txs := s.convertBlockTransactions(block, slot, timestamp, blockHash)
		allTransactions = append(allTransactions, txs...)
	}

	return allTransactions, nil
}

// GetBlocksByRange 批量获取区块数据
func (s *SolanaProcessor) GetBlocksByRange(ctx context.Context, startBlock, endBlock *big.Int) ([]types.UnifiedBlock, error) {
	var allBlocks []types.UnifiedBlock

	for slot := startBlock.Uint64(); slot <= endBlock.Uint64(); slot++ {
		unifiedBlock, err := s.GetBlock(ctx, big.NewInt(int64(slot)))
		if err != nil {
			s.Log.WithFields(logrus.Fields{"slot": slot}).Warnf("skipping slot: %v", err)
			continue
		}
		allBlocks = append(allBlocks, *unifiedBlock)
	}

	return allBlocks, nil
}

// GetBlock 获取单个区块
func (s *SolanaProcessor) GetBlock(ctx context.Context, blockNumber *big.Int) (*types.UnifiedBlock, error) {
	slot := blockNumber.Uint64()

	block, err := s.getBlockWithRetry(ctx, slot)
	if err != nil {
		return nil, fmt.Errorf("get solana block %d failed: %w", slot, err)
	}

	if block == nil {
		return nil, fmt.Errorf("solana block %d returned nil", slot)
	}

	timestamp := blockTimestamp(block)
	blockHash := block.Blockhash.String()

	transactions := s.convertBlockTransactions(block, slot, timestamp, blockHash)

	unifiedBlock := &types.UnifiedBlock{
		BlockNumber:  blockNumber,
		BlockHash:    blockHash,
		ChainType:    s.ChainType,
		ChainID:      s.chainID,
		ParentHash:   block.PreviousBlockhash.String(),
		Timestamp:    timestamp,
		TxCount:      len(block.Transactions),
		Transactions: transactions,
		Events:       make([]types.UnifiedEvent, 0),
		RawData:      block,
	}

	s.Log.WithFields(logrus.Fields{
		"slot":     slot,
		"tx_total": len(block.Transactions),
		"tx_valid": len(transactions),
	}).Debug("solana block processed")

	return unifiedBlock, nil
}

// GetTransaction 获取单个交易
func (s *SolanaProcessor) GetTransaction(ctx context.Context, txHash string) (*types.UnifiedTransaction, error) {
	sig, err := solana.SignatureFromBase58(txHash)
	if err != nil {
		return nil, fmt.Errorf("invalid solana transaction signature %q: %w", txHash, err)
	}

	var txResult *rpc.GetTransactionResult
	err = base.Retry(ctx, s.Log, "get-tx-"+txHash[:8], s.Retry, func() error {
		txCtx, cancel := context.WithTimeout(ctx, time.Second*30)
		defer cancel()

		var rpcErr error
		txResult, rpcErr = s.client.GetTransaction(txCtx, sig, &rpc.GetTransactionOpts{
			MaxSupportedTransactionVersion: &maxSupportedTxVersion,
		})
		return rpcErr
	})
	if err != nil {
		return nil, fmt.Errorf("get solana transaction failed: %w", err)
	}

	if txResult == nil {
		return nil, fmt.Errorf("solana transaction %s not found", txHash)
	}

	// Determine status
	status := types.TransactionStatusSuccess
	if txResult.Meta != nil && txResult.Meta.Err != nil {
		status = types.TransactionStatusFailed
	}

	// Extract fee
	var gasUsed *big.Int
	if txResult.Meta != nil {
		gasUsed = big.NewInt(int64(txResult.Meta.Fee))
	}

	// Parse transaction once for fromAddress and accountKeys
	fromAddress := ""
	parsedTx, parseErr := txResult.Transaction.GetTransaction()
	if parseErr == nil && len(parsedTx.Message.AccountKeys) > 0 {
		fromAddress = parsedTx.Message.AccountKeys[0].String()
	}

	rawData := buildRawDataFromResult(txResult, parsedTx)

	// Determine block time
	var txTime time.Time
	if txResult.BlockTime != nil {
		txTime = time.Unix(int64(*txResult.BlockTime), 0)
	} else {
		txTime = time.Now()
	}

	unifiedTx := &types.UnifiedTransaction{
		TxHash:      txHash,
		ChainType:   s.ChainType,
		ChainID:     s.chainID,
		BlockNumber: big.NewInt(int64(txResult.Slot)),
		FromAddress: fromAddress,
		Status:      status,
		Timestamp:   txTime,
		GasUsed:     gasUsed,
		RawData:     rawData,
	}
	return unifiedTx, nil
}

// HealthCheck 健康检查
func (s *SolanaProcessor) HealthCheck(ctx context.Context) error {
	_, err := s.client.GetSlot(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return fmt.Errorf("solana health check failed: %w", err)
	}
	return nil
}

// getBlockWithRetry fetches a Solana block with retry and versioned transaction support.
func (s *SolanaProcessor) getBlockWithRetry(ctx context.Context, slot uint64) (*rpc.GetBlockResult, error) {
	var result *rpc.GetBlockResult

	err := base.Retry(ctx, s.Log, fmt.Sprintf("get-solana-block-%d", slot), s.Retry, func() error {
		blockCtx, cancel := context.WithTimeout(ctx, time.Second*30)
		defer cancel()

		block, rpcErr := s.client.GetBlockWithOpts(blockCtx, slot, &rpc.GetBlockOpts{
			MaxSupportedTransactionVersion: &maxSupportedTxVersion,
			TransactionDetails:             rpc.TransactionDetailsFull,
			Commitment:                     rpc.CommitmentFinalized,
		})
		if rpcErr != nil {
			return rpcErr
		}

		result = block
		return nil
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}

// convertBlockTransactions 将区块中的所有交易转换为统一格式。
// 跳过失败的交易，因为失败交易不会产生有效的 DEX 事件。
func (s *SolanaProcessor) convertBlockTransactions(block *rpc.GetBlockResult, slot uint64, timestamp time.Time, blockHash string) []types.UnifiedTransaction {
	if block == nil || len(block.Transactions) == 0 {
		return nil
	}

	transactions := make([]types.UnifiedTransaction, 0, len(block.Transactions))
	skippedFailed := 0

	for i, txWithMeta := range block.Transactions {
		// Skip failed transactions: they don't produce valid DEX events
		if txWithMeta.Meta != nil && txWithMeta.Meta.Err != nil {
			skippedFailed++
			continue
		}

		// Parse transaction once, reuse for signature, fromAddress, and accountKeys
		parsedTx, parseErr := txWithMeta.GetTransaction()

		txHash := fmt.Sprintf("tx_%d_%d", slot, i)
		fromAddress := ""
		if parseErr == nil {
			if len(parsedTx.Signatures) > 0 {
				txHash = parsedTx.Signatures[0].String()
			}
			if len(parsedTx.Message.AccountKeys) > 0 {
				fromAddress = parsedTx.Message.AccountKeys[0].String()
			}
		}

		// 构建 RawData，包含 logMessages、innerInstructions 和 accountKeys
		rawData := buildRawData(&txWithMeta, parsedTx)

		// Gas 费用
		var gasUsed *big.Int
		if txWithMeta.Meta != nil {
			gasUsed = big.NewInt(int64(txWithMeta.Meta.Fee))
		}

		tx := types.UnifiedTransaction{
			TxHash:      txHash,
			ChainType:   s.ChainType,
			ChainID:     s.chainID,
			BlockNumber: big.NewInt(int64(slot)),
			BlockHash:   blockHash,
			TxIndex:     i,
			FromAddress: fromAddress,
			Status:      types.TransactionStatusSuccess,
			Timestamp:   timestamp,
			GasUsed:     gasUsed,
			RawData:     rawData,
		}

		transactions = append(transactions, tx)
	}

	if skippedFailed > 0 {
		s.Log.WithFields(logrus.Fields{
			"slot":           slot,
			"skipped_failed": skippedFailed,
			"kept":           len(transactions),
		}).Debug("skipped failed transactions")
	}

	return transactions
}

// buildRawData 构建 DEX extractor 可解析的 RawData。
// parsedTx 可以为 nil（解析失败时），此时仅从 meta 提取数据。
func buildRawData(txWithMeta *rpc.TransactionWithMeta, parsedTx *solana.Transaction) map[string]any {
	rawData := make(map[string]any)

	if txWithMeta.Meta != nil {
		meta := txWithMeta.Meta
		rawData["logMessages"] = meta.LogMessages

		if len(meta.InnerInstructions) > 0 {
			innerInstructions := make([]map[string]any, 0, len(meta.InnerInstructions))
			for _, inner := range meta.InnerInstructions {
				innerInstructions = append(innerInstructions, map[string]any{
					"index":        inner.Index,
					"instructions": inner.Instructions,
				})
			}
			rawData["innerInstructions"] = innerInstructions
		}

		if len(meta.PreTokenBalances) > 0 {
			rawData["preTokenBalances"] = meta.PreTokenBalances
		}
		if len(meta.PostTokenBalances) > 0 {
			rawData["postTokenBalances"] = meta.PostTokenBalances
		}

		rawData["meta"] = map[string]any{
			"fee": meta.Fee,
			"err": meta.Err,
		}
	}

	// Account keys from parsed transaction + Address Lookup Tables
	if parsedTx != nil {
		keys := make([]string, 0, len(parsedTx.Message.AccountKeys))
		for _, key := range parsedTx.Message.AccountKeys {
			keys = append(keys, key.String())
		}
		if txWithMeta.Meta != nil {
			for _, addr := range txWithMeta.Meta.LoadedAddresses.Writable {
				keys = append(keys, addr.String())
			}
			for _, addr := range txWithMeta.Meta.LoadedAddresses.ReadOnly {
				keys = append(keys, addr.String())
			}
		}
		rawData["accountKeys"] = keys
	}

	return rawData
}

// buildRawDataFromResult builds RawData from a GetTransactionResult (single tx lookup).
// parsedTx 可以为 nil（解析失败时），此时仅从 meta 提取数据。
func buildRawDataFromResult(txResult *rpc.GetTransactionResult, parsedTx *solana.Transaction) map[string]any {
	rawData := make(map[string]any)

	if txResult.Meta != nil {
		meta := txResult.Meta
		rawData["logMessages"] = meta.LogMessages

		if len(meta.InnerInstructions) > 0 {
			innerInstructions := make([]map[string]any, 0, len(meta.InnerInstructions))
			for _, inner := range meta.InnerInstructions {
				innerInstructions = append(innerInstructions, map[string]any{
					"index":        inner.Index,
					"instructions": inner.Instructions,
				})
			}
			rawData["innerInstructions"] = innerInstructions
		}

		if len(meta.PreTokenBalances) > 0 {
			rawData["preTokenBalances"] = meta.PreTokenBalances
		}
		if len(meta.PostTokenBalances) > 0 {
			rawData["postTokenBalances"] = meta.PostTokenBalances
		}

		rawData["meta"] = map[string]any{
			"fee": meta.Fee,
			"err": meta.Err,
		}
	}

	// Account keys from parsed transaction + Address Lookup Tables
	if parsedTx != nil {
		keys := make([]string, 0, len(parsedTx.Message.AccountKeys))
		for _, key := range parsedTx.Message.AccountKeys {
			keys = append(keys, key.String())
		}
		if txResult.Meta != nil {
			for _, addr := range txResult.Meta.LoadedAddresses.Writable {
				keys = append(keys, addr.String())
			}
			for _, addr := range txResult.Meta.LoadedAddresses.ReadOnly {
				keys = append(keys, addr.String())
			}
		}
		rawData["accountKeys"] = keys
	}

	return rawData
}

// blockTimestamp 从区块中提取时间戳
func blockTimestamp(block *rpc.GetBlockResult) time.Time {
	if block.BlockTime != nil {
		return time.Unix(int64(*block.BlockTime), 0)
	}
	return time.Now()
}
