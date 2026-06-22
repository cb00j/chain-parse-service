package sui

import (
	"context"
	"fmt"
	"math/big"
	"strconv"
	"time"

	"unified-tx-parser/internal/parser/chains/base"
	"unified-tx-parser/internal/types"

	"github.com/block-vision/sui-go-sdk/models"
	"github.com/block-vision/sui-go-sdk/sui"
	"github.com/sirupsen/logrus"
)

var log = logrus.WithFields(logrus.Fields{"service": "parser", "module": "chain-sui"})

// SuiProcessor Sui链处理器
type SuiProcessor struct {
	base.Processor
	client  sui.ISuiAPI
	chainID string
	config  *SuiConfig
}

// SuiConfig Sui配置
type SuiConfig struct {
	RPCEndpoint string `json:"rpc_endpoint"`
	ChainID     string `json:"chain_id"`
	BatchSize   int    `json:"batch_size"`
}

// NewSuiProcessor 创建新的Sui处理器
func NewSuiProcessor(config *SuiConfig) (*SuiProcessor, error) {
	if config.BatchSize <= 0 {
		config.BatchSize = 50
	}

	// 创建Sui客户端
	client := sui.NewSuiClient(config.RPCEndpoint)

	processor := &SuiProcessor{
		Processor: base.NewProcessor(types.ChainTypeSui, config.RPCEndpoint, config.BatchSize),
		client:    client,
		chainID:   config.ChainID,
		config:    config,
	}

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	if err := processor.HealthCheck(ctx); err != nil {
		return nil, fmt.Errorf("Sui连接测试失败: %w", err)
	}

	processor.Log.Infof("sui processor initialized: %s", config.RPCEndpoint)
	return processor, nil
}

// GetChainID 获取链ID
func (s *SuiProcessor) GetChainID() string {
	return s.chainID
}

// GetLatestBlockNumber 获取最新区块号 (Sui中是检查点序号)
func (s *SuiProcessor) GetLatestBlockNumber(ctx context.Context) (*big.Int, error) {
	// 在Sui中，我们使用检查点序号作为"区块号"
	sequence, err := s.client.SuiGetLatestCheckpointSequenceNumber(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取最新检查点序号失败: %w", err)
	}

	return big.NewInt(int64(sequence)), nil
}

// GetTransactionsByBlockRange 批量获取交易 (通过检查点范围)
func (s *SuiProcessor) GetTransactionsByBlockRange(ctx context.Context, startBlock, endBlock *big.Int) ([]types.UnifiedTransaction, error) {
	var allTransactions []types.UnifiedTransaction

	// 遍历检查点范围
	for checkpoint := new(big.Int).Set(startBlock); checkpoint.Cmp(endBlock) <= 0; checkpoint.Add(checkpoint, big.NewInt(1)) {
		checkpointSeq := checkpoint.String()

		// 获取检查点数据
		checkpointResp, err := s.client.SuiGetCheckpoint(ctx, models.SuiGetCheckpointRequest{
			CheckpointID: checkpointSeq,
		})
		if err != nil {
			s.Log.Warnf("failed to get checkpoint %s: %v", checkpointSeq, err)
			continue
		}

		if len(checkpointResp.Transactions) == 0 {
			continue
		}

		// 批量获取交易详情
		transactions, err := s.getTransactionDetails(ctx, checkpointResp.Transactions)
		if err != nil {
			s.Log.Warnf("failed to get transaction details (checkpoint %s): %v", checkpointSeq, err)
			continue
		}

		allTransactions = append(allTransactions, transactions...)
	}

	return allTransactions, nil
}

// GetBlocksByRange 批量获取区块数据 (新接口方法)
func (s *SuiProcessor) GetBlocksByRange(ctx context.Context, startBlock, endBlock *big.Int) ([]types.UnifiedBlock, error) {
	var allBlocks []types.UnifiedBlock

	// 遍历检查点范围
	for checkpoint := new(big.Int).Set(startBlock); checkpoint.Cmp(endBlock) <= 0; checkpoint.Add(checkpoint, big.NewInt(1)) {
		// 获取单个区块
		unifiedBlock, err := s.GetBlock(ctx, checkpoint)
		if err != nil {
			s.Log.Warnf("skipping checkpoint %s: %v", checkpoint.String(), err)
			continue
		}

		allBlocks = append(allBlocks, *unifiedBlock)
	}

	return allBlocks, nil
}

// GetBlock 获取单个区块
func (s *SuiProcessor) GetBlock(ctx context.Context, blockNumber *big.Int) (*types.UnifiedBlock, error) {
	checkpointSeq := blockNumber.String()

	checkpoint, err := s.client.SuiGetCheckpoint(ctx, models.SuiGetCheckpointRequest{
		CheckpointID: checkpointSeq,
	})
	if err != nil {
		return nil, fmt.Errorf("获取Sui检查点失败: %w", err)
	}

	var timestamp time.Time
	if timestampMs, err := strconv.ParseInt(checkpoint.TimestampMs, 10, 64); err == nil {
		timestamp = time.Unix(0, timestampMs*int64(time.Millisecond))
	} else {
		timestamp = time.Now()
	}

	var transactions []types.UnifiedTransaction

	if len(checkpoint.Transactions) > 0 {
		digestIndex := make(map[string]int, len(checkpoint.Transactions))
		for i, digest := range checkpoint.Transactions {
			digestIndex[digest] = i
		}

		txDetails, err := s.getTransactionDetails(ctx, checkpoint.Transactions)
		if err != nil {
			s.Log.Warnf("failed to batch fetch transactions for checkpoint %s: %v", checkpointSeq, err)
		} else {
			for i := range txDetails {
				txDetails[i].BlockNumber = blockNumber
				txDetails[i].Timestamp = timestamp
				if idx, ok := digestIndex[txDetails[i].TxHash]; ok {
					txDetails[i].TxIndex = idx
				}
			}
			transactions = txDetails
		}
	}

	unifiedBlock := &types.UnifiedBlock{
		BlockNumber:  blockNumber,
		BlockHash:    checkpoint.Digest,
		ChainType:    s.ChainType,
		ChainID:      s.chainID,
		ParentHash:   checkpoint.PreviousDigest,
		Timestamp:    timestamp,
		TxCount:      len(checkpoint.Transactions),
		Transactions: transactions,
		Events:       make([]types.UnifiedEvent, 0),
		RawData:      checkpoint,
	}

	return unifiedBlock, nil
}

// GetTransaction 获取单个交易
func (s *SuiProcessor) GetTransaction(ctx context.Context, txHash string) (*types.UnifiedTransaction, error) {
	// 构建查询选项
	options := models.SuiTransactionBlockOptions{
		ShowInput:          true,
		ShowRawInput:       true,
		ShowEffects:        true,
		ShowEvents:         true,
		ShowObjectChanges:  true,
		ShowBalanceChanges: true,
	}

	// 获取交易数据
	resp, err := s.client.SuiGetTransactionBlock(ctx, models.SuiGetTransactionBlockRequest{
		Digest:  txHash,
		Options: options,
	})
	if err != nil {
		return nil, fmt.Errorf("获取交易失败: %w", err)
	}

	// 转换为统一交易格式
	unifiedTx, err := s.convertToUnifiedTransaction(&resp)
	if err != nil {
		return nil, fmt.Errorf("转换交易格式失败: %w", err)
	}

	return unifiedTx, nil
}

// HealthCheck 健康检查
func (s *SuiProcessor) HealthCheck(ctx context.Context) error {
	_, err := s.client.SuiGetLatestCheckpointSequenceNumber(ctx)
	if err != nil {
		return fmt.Errorf("Sui健康检查失败: %w", err)
	}
	return nil
}

// getTransactionDetails 批量获取交易详情
func (s *SuiProcessor) getTransactionDetails(ctx context.Context, txHashes []string) ([]types.UnifiedTransaction, error) {
	if len(txHashes) == 0 {
		return nil, nil
	}

	// 构建查询选项
	options := models.SuiTransactionBlockOptions{
		ShowInput:          true,
		ShowRawInput:       true,
		ShowEffects:        true,
		ShowEvents:         true,
		ShowObjectChanges:  true,
		ShowBalanceChanges: true,
	}

	// 批量查询
	responses, err := s.client.SuiMultiGetTransactionBlocks(ctx, models.SuiMultiGetTransactionBlocksRequest{
		Digests: txHashes,
		Options: options,
	})
	if err != nil {
		// 如果批量失败，尝试单个获取
		s.Log.Warnf("batch fetch failed, falling back to individual fetch: %v", err)
		return s.getTransactionDetailsIndividually(ctx, txHashes)
	}

	var transactions []types.UnifiedTransaction
	for _, resp := range responses {
		if resp.Digest == "" {
			continue
		}

		tx, err := s.convertToUnifiedTransaction(resp)
		if err != nil {
			s.Log.Warnf("failed to convert transaction (hash: %s): %v", resp.Digest, err)
			continue
		}

		transactions = append(transactions, *tx)
	}

	return transactions, nil
}

// getTransactionDetailsIndividually 单个获取交易详情 (回退方案)
func (s *SuiProcessor) getTransactionDetailsIndividually(ctx context.Context, txHashes []string) ([]types.UnifiedTransaction, error) {
	var transactions []types.UnifiedTransaction

	options := models.SuiTransactionBlockOptions{
		ShowInput:          true,
		ShowRawInput:       true,
		ShowEffects:        true,
		ShowEvents:         true,
		ShowObjectChanges:  true,
		ShowBalanceChanges: true,
	}

	for _, txHash := range txHashes {
		resp, err := s.client.SuiGetTransactionBlock(ctx, models.SuiGetTransactionBlockRequest{
			Digest:  txHash,
			Options: options,
		})
		if err != nil {
			s.Log.Warnf("failed to get individual transaction (hash: %s): %v", txHash, err)
			continue
		}

		tx, err := s.convertToUnifiedTransaction(&resp)
		if err != nil {
			s.Log.Warnf("failed to convert transaction (hash: %s): %v", txHash, err)
			continue
		}

		transactions = append(transactions, *tx)
	}

	return transactions, nil
}

// convertToUnifiedTransaction 转换为统一交易格式
func (s *SuiProcessor) convertToUnifiedTransaction(resp *models.SuiTransactionBlockResponse) (*types.UnifiedTransaction, error) {
	if resp == nil {
		return nil, fmt.Errorf("交易响应为空")
	}

	// 解析时间戳
	timestamp := time.Now()
	if resp.TimestampMs != "" {
		if timestampMs, err := strconv.ParseInt(resp.TimestampMs, 10, 64); err == nil {
			timestamp = time.Unix(0, timestampMs*int64(time.Millisecond))
		}
	}

	// 解析区块号
	var blockNumber *big.Int
	if resp.Checkpoint != "" {
		if checkpointNum, err := strconv.ParseInt(resp.Checkpoint, 10, 64); err == nil {
			blockNumber = big.NewInt(checkpointNum)
		}
	}

	// 构建统一交易
	tx := &types.UnifiedTransaction{
		TxHash:      resp.Digest,
		ChainType:   s.ChainType,
		ChainID:     s.chainID,
		BlockNumber: blockNumber,
		Status:      types.TransactionStatusSuccess,
		Timestamp:   timestamp,
		RawData:     resp,
	}

	// 设置交易状态
	if resp.Effects.Status.Status == "success" {
		tx.Status = types.TransactionStatusSuccess
	} else {
		tx.Status = types.TransactionStatusFailed
	}

	// 解析发送者
	if resp.Transaction.Data.Sender != "" {
		tx.FromAddress = resp.Transaction.Data.Sender
	}

	// 解析gas费用
	if resp.Effects.GasUsed.ComputationCost != "" {
		if computationCost, err := strconv.ParseInt(resp.Effects.GasUsed.ComputationCost, 10, 64); err == nil {
			tx.GasUsed = big.NewInt(computationCost)
		}
	}
	if resp.Effects.GasUsed.StorageCost != "" {
		if storageCost, err := strconv.ParseInt(resp.Effects.GasUsed.StorageCost, 10, 64); err == nil {
			if tx.GasUsed == nil {
				tx.GasUsed = big.NewInt(0)
			}
			tx.GasUsed.Add(tx.GasUsed, big.NewInt(storageCost))
		}
	}

	return tx, nil
}

func (s *SuiProcessor) GetObject(ctx context.Context, req models.SuiGetObjectRequest) (models.SuiObjectResponse, error) {
	objectData, err := s.client.SuiGetObject(ctx, req)
	if err != nil {
		s.Log.Errorf("SuiGetObjectData failed, err: %v", err)
		return objectData, err
	}
	return objectData, nil
}

func (s *SuiProcessor) GetToken(ctx context.Context, req models.SuiXGetCoinMetadataRequest) (models.CoinMetadataResponse, error) {
	resp, err := s.client.SuiXGetCoinMetadata(ctx, req)
	if err != nil {
		s.Log.Errorf("SuiXGetCoinMetadata failed, err: %v", err)
		return models.CoinMetadataResponse{}, err
	}
	return resp, nil
}
