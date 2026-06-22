package bsc

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"unified-tx-parser/internal/parser/chains/base"
	"unified-tx-parser/internal/types"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/sirupsen/logrus"
)

var log = logrus.WithFields(logrus.Fields{"service": "parser", "module": "chain-bsc"})

// BSCProcessor BSC链处理器
type BSCProcessor struct {
	base.Processor
	client  *ethclient.Client
	config  *BSCConfig
	chainID *big.Int
}

// BSCConfig BSC配置
type BSCConfig struct {
	RPCEndpoint string
	ChainID     int64
	BatchSize   int
}

// NewBSCProcessor 创建BSC处理器
func NewBSCProcessor(config *BSCConfig) (*BSCProcessor, error) {
	// 连接到BSC节点
	client, err := ethclient.Dial(config.RPCEndpoint)
	if err != nil {
		return nil, fmt.Errorf("连接BSC节点失败: %w", err)
	}

	processor := &BSCProcessor{
		Processor: base.NewProcessor(types.ChainTypeBSC, config.RPCEndpoint, config.BatchSize),
		client:    client,
		config:    config,
		chainID:   big.NewInt(config.ChainID),
	}

	// 测试连接 - 使用更长的超时时间
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*60)
	defer cancel()

	processor.Log.Infof("testing BSC connection: %s", config.RPCEndpoint)

	// 重试连接测试
	err = base.Retry(ctx, processor.Log, "bsc-connection-test", processor.Retry, func() error {
		return processor.HealthCheck(ctx)
	})
	if err != nil {
		return nil, fmt.Errorf("BSC连接测试失败: %w", err)
	}

	processor.Log.Infof("BSC processor initialized")
	return processor, nil
}

// GetChainID 获取链ID
func (b *BSCProcessor) GetChainID() string {
	return fmt.Sprintf("%d", b.config.ChainID)
}

// HealthCheck 健康检查
func (b *BSCProcessor) HealthCheck(ctx context.Context) error {
	_, err := b.client.BlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("bsc健康检查失败: %w", err)
	}
	return nil
}

// GetLatestBlockNumber 获取最新区块号
func (b *BSCProcessor) GetLatestBlockNumber(ctx context.Context) (*big.Int, error) {
	blockNumber, err := b.client.BlockNumber(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取BSC最新区块号失败: %w", err)
	}
	return big.NewInt(int64(blockNumber)), nil
}

// GetTransaction 获取单个交易
func (b *BSCProcessor) GetTransaction(ctx context.Context, txHash string) (*types.UnifiedTransaction, error) {
	hash := common.HexToHash(txHash)

	// 获取交易
	tx, isPending, err := b.client.TransactionByHash(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("获取BSC交易失败: %w", err)
	}

	if isPending {
		return nil, fmt.Errorf("交易仍在pending状态")
	}

	// 获取交易收据
	receipt, err := b.client.TransactionReceipt(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("获取BSC交易收据失败: %w", err)
	}

	// 获取区块信息
	block, err := b.client.BlockByHash(ctx, receipt.BlockHash)
	if err != nil {
		return nil, fmt.Errorf("获取BSC区块失败: %w", err)
	}

	return b.convertToUnifiedTransaction(tx, receipt, block)
}

// GetTransactionsByBlockRange 获取区块范围内的交易
func (b *BSCProcessor) GetTransactionsByBlockRange(ctx context.Context, startBlock, endBlock *big.Int) ([]types.UnifiedTransaction, error) {
	var allTransactions []types.UnifiedTransaction

	for blockNum := new(big.Int).Set(startBlock); blockNum.Cmp(endBlock) <= 0; blockNum.Add(blockNum, big.NewInt(1)) {
		// 获取区块 - 添加重试机制
		block, err := b.getBlockWithRetry(ctx, blockNum)
		if err != nil {
			b.Log.Warnf("skipping block %s: %v", blockNum.String(), err)
			continue // 跳过有问题的区块，继续处理下一个
		}

		// 获取区块中的所有交易
		transactions, err := b.getTransactionsFromBlock(ctx, block)
		if err != nil {
			b.Log.Warnf("skipping block %s transaction processing: %v", blockNum.String(), err)
			continue // 跳过有问题的区块，继续处理下一个
		}

		allTransactions = append(allTransactions, transactions...)
	}

	return allTransactions, nil
}

// GetBlocksByRange 批量获取区块数据 (新接口方法)
func (b *BSCProcessor) GetBlocksByRange(ctx context.Context, startBlock, endBlock *big.Int) ([]types.UnifiedBlock, error) {
	var allBlocks []types.UnifiedBlock

	for blockNum := new(big.Int).Set(startBlock); blockNum.Cmp(endBlock) <= 0; blockNum.Add(blockNum, big.NewInt(1)) {
		// 获取单个区块
		unifiedBlock, err := b.GetBlock(ctx, blockNum)
		if err != nil {
			b.Log.Warnf("skipping block %s: %v", blockNum.String(), err)
			continue
		}

		allBlocks = append(allBlocks, *unifiedBlock)
	}

	return allBlocks, nil
}

// GetBlock 获取单个区块
func (b *BSCProcessor) GetBlock(ctx context.Context, blockNumber *big.Int) (*types.UnifiedBlock, error) {
	// 获取区块数据 - 使用重试机制
	block, err := b.getBlockWithRetry(ctx, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("获取区块失败: %w", err)
	}

	// 获取区块中的所有交易
	transactions, err := b.getTransactionsFromBlock(ctx, block)
	if err != nil {
		return nil, fmt.Errorf("获取区块交易失败: %w", err)
	}

	// 构建统一区块结构
	unifiedBlock := &types.UnifiedBlock{
		BlockNumber:  block.Number(),
		BlockHash:    block.Hash().Hex(),
		ChainType:    b.ChainType,
		ChainID:      b.GetChainID(),
		ParentHash:   block.ParentHash().Hex(),
		Timestamp:    time.Unix(int64(block.Time()), 0),
		Difficulty:   block.Difficulty(),
		GasLimit:     new(big.Int).SetUint64(block.GasLimit()),
		GasUsed:      new(big.Int).SetUint64(block.GasUsed()),
		Miner:        block.Coinbase().Hex(),
		Nonce:        fmt.Sprintf("0x%x", block.Nonce()),
		Size:         int64(block.Size()),
		TxCount:      len(transactions),
		Transactions: transactions,
		Events:       make([]types.UnifiedEvent, 0),
		RawData:      block,
	}

	return unifiedBlock, nil
}

// getBlockWithRetry 带重试机制的区块获取
func (b *BSCProcessor) getBlockWithRetry(ctx context.Context, blockNum *big.Int) (*ethtypes.Block, error) {
	var result *ethtypes.Block

	err := base.Retry(ctx, b.Log, fmt.Sprintf("get-bsc-block-%s", blockNum.String()), b.Retry, func() error {
		blockCtx, cancel := context.WithTimeout(ctx, time.Second*30)
		defer cancel()

		block, err := b.client.BlockByNumber(blockCtx, blockNum)
		if err == nil {
			result = block
			return nil
		}

		// 如果是交易类型不支持的错误，尝试其他方法
		if strings.Contains(err.Error(), "transaction type not supported") {
			b.Log.Warnf("block %s contains unsupported transaction type, trying fallback", blockNum.String())

			header, headerErr := b.client.HeaderByNumber(blockCtx, blockNum)
			if headerErr == nil {
				b.Log.Infof("got block %s header only, skipping transaction processing", blockNum.String())
				result = ethtypes.NewBlockWithHeader(header)
				return nil
			}
		}

		return err
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}

// getTransactionsFromBlock 从区块中获取所有交易
func (b *BSCProcessor) getTransactionsFromBlock(ctx context.Context, block *ethtypes.Block) ([]types.UnifiedTransaction, error) {
	var transactions []types.UnifiedTransaction

	blockTxCount := len(block.Transactions())

	// 如果区块没有交易，直接返回
	if blockTxCount == 0 {
		b.Log.Infof("BSC block %s has no transactions", block.Number().String())
		return transactions, nil
	}

	b.Log.Infof("processing BSC block %s, %d transactions", block.Number().String(), blockTxCount)

	// 如果交易数量太多，限制处理数量以避免超时
	maxTxPerBlock := 200 // BSC比以太坊快，可以处理更多交易
	if blockTxCount > maxTxPerBlock {
		b.Log.Warnf("too many transactions in block (%d), limiting to %d", blockTxCount, maxTxPerBlock)
		blockTxCount = maxTxPerBlock
	}

	// 批量获取交易收据 - 使用并发处理
	receipts := make([]*ethtypes.Receipt, blockTxCount)

	// 并发获取收据
	type receiptResult struct {
		index   int
		receipt *ethtypes.Receipt
		err     error
	}

	resultChan := make(chan receiptResult, blockTxCount)
	semaphore := make(chan struct{}, 15) // BSC可以使用更多并发

	// 启动goroutines获取收据
	for i := 0; i < blockTxCount; i++ {
		go func(index int, tx *ethtypes.Transaction) {
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			// 为每个请求设置超时
			receiptCtx, cancel := context.WithTimeout(ctx, time.Second*20) // BSC响应更快
			defer cancel()

			receipt, err := b.client.TransactionReceipt(receiptCtx, tx.Hash())
			resultChan <- receiptResult{
				index:   index,
				receipt: receipt,
				err:     err,
			}
		}(i, block.Transactions()[i])
	}

	// 收集结果
	for i := 0; i < blockTxCount; i++ {
		result := <-resultChan
		if result.err != nil {
			b.Log.Warnf("failed to get BSC transaction receipt (index: %d): %v", result.index, result.err)
			continue
		}
		receipts[result.index] = result.receipt
	}

	// 转换交易
	successCount := 0
	for i := 0; i < blockTxCount; i++ {
		if receipts[i] == nil {
			continue
		}

		tx := block.Transactions()[i]
		unifiedTx, err := b.convertToUnifiedTransaction(tx, receipts[i], block)
		if err != nil {
			b.Log.Warnf("failed to convert BSC transaction (hash: %s): %v", tx.Hash().Hex(), err)
			continue
		}

		transactions = append(transactions, *unifiedTx)
		successCount++
	}

	if successCount == blockTxCount {
		b.Log.Infof("BSC block %s processed, converted %d transactions", block.Number().String(), successCount)
	} else {
		b.Log.Warnf("BSC block %s processed, converted %d/%d transactions (%d failed)",
			block.Number().String(), successCount, blockTxCount, blockTxCount-successCount)
	}
	return transactions, nil
}

// convertToUnifiedTransaction 转换为统一交易格式
func (b *BSCProcessor) convertToUnifiedTransaction(tx *ethtypes.Transaction, receipt *ethtypes.Receipt, block *ethtypes.Block) (*types.UnifiedTransaction, error) {
	// 确定发送者地址 - 支持多种交易类型
	var from common.Address
	var err error

	// 尝试不同的签名器类型
	signers := []ethtypes.Signer{
		ethtypes.LatestSignerForChainID(b.chainID), // 支持最新的交易类型
		ethtypes.NewEIP155Signer(b.chainID),        // EIP-155
		ethtypes.NewLondonSigner(b.chainID),        // EIP-1559 (London fork)
		ethtypes.HomesteadSigner{},                 // Homestead
	}

	for _, signer := range signers {
		from, err = ethtypes.Sender(signer, tx)
		if err == nil {
			break
		}
	}

	if err != nil {
		// 如果所有签名器都失败，尝试从收据中获取
		b.Log.Warnf("failed to get sender address via signer, using fallback: %v", err)
		// 作为最后手段，使用零地址
		from = common.Address{}
	}

	// 确定接收者地址
	var toAddress string
	if tx.To() != nil {
		toAddress = tx.To().Hex()
	} else {
		// 合约创建交易
		toAddress = receipt.ContractAddress.Hex()
	}

	// 确定交易状态
	var status types.TransactionStatus
	if receipt.Status == ethtypes.ReceiptStatusSuccessful {
		status = types.TransactionStatusSuccess
	} else {
		status = types.TransactionStatusFailed
	}

	// 处理Gas价格 - 支持不同的交易类型
	var gasPrice *big.Int
	if tx.GasPrice() != nil {
		gasPrice = tx.GasPrice()
	} else {
		// 对于EIP-1559交易，使用有效Gas价格
		if tx.GasFeeCap() != nil && tx.GasTipCap() != nil {
			// 简化计算：使用GasFeeCap作为Gas价格
			gasPrice = tx.GasFeeCap()
		} else {
			gasPrice = big.NewInt(0)
		}
	}

	unifiedTx := &types.UnifiedTransaction{
		ChainType:   b.ChainType,
		ChainID:     b.GetChainID(),
		TxHash:      tx.Hash().Hex(),
		BlockNumber: receipt.BlockNumber,
		BlockHash:   receipt.BlockHash.Hex(),
		TxIndex:     int(receipt.TransactionIndex),
		FromAddress: from.Hex(),
		ToAddress:   toAddress,
		Value:       tx.Value(),
		GasLimit:    big.NewInt(int64(tx.Gas())),
		GasUsed:     big.NewInt(int64(receipt.GasUsed)),
		GasPrice:    gasPrice,
		Status:      status,
		Timestamp:   time.Unix(int64(block.Time()), 0),
		RawData: map[string]interface{}{
			"transaction": tx,
			"receipt":     receipt,
			"block":       block,
		},
	}

	return unifiedTx, nil
}
