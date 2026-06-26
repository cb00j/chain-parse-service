package ethereum

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"unified-tx-parser/internal/parser/chains/base"
	"unified-tx-parser/internal/types"
	"unified-tx-parser/internal/utils"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sirupsen/logrus"
)

var log = logrus.WithFields(logrus.Fields{"service": "parser", "module": "chain-ethereum"})

// EthereumProcessor 以太坊链处理器
type EthereumProcessor struct {
	base.Processor
	rpcClient *rpc.Client       // 用于发送底层的、自定义的 RPC 请求
	client    *ethclient.Client // 用于发送标准封装好的请求
	chainID   *big.Int
	config    *EthereumConfig
}

// EthereumConfig 以太坊链配置
type EthereumConfig struct {
	RPCEndpoint string `json:"rpc_endpoint"`
	ChainID     int64  `json:"chain_id"`
	BatchSize   int    `json:"batch_size"`
	IsTestnet   bool   `json:"is_testnet"`
}

// NewEthereumProcessor 创建以太坊处理器
func NewEthereumProcessor(config *EthereumConfig) (*EthereumProcessor, error) {
	if config == nil {
		config = &EthereumConfig{
			RPCEndpoint: "https://mainnet.infura.io/v3/YOUR_PROJECT_ID",
			ChainID:     1,
			BatchSize:   100,
			IsTestnet:   false,
		}
	}

	// 创建以太坊客户端
	rpcClient, err := rpc.Dial(config.RPCEndpoint)
	//client, err := ethclient.Dial(config.RPCEndpoint)
	if err != nil {
		return nil, fmt.Errorf("连接以太坊节点失败: %w", err)
	}

	processor := &EthereumProcessor{
		Processor: base.NewProcessor(types.ChainTypeEthereum, config.RPCEndpoint, config.BatchSize),
		rpcClient: rpcClient,
		client:    ethclient.NewClient(rpcClient), // 将底层的 rpc.Client 包装成标准的 ethclient.Client
		chainID:   big.NewInt(config.ChainID),
		config:    config,
	}

	// 测试连接 - 使用更长的超时时间
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()

	processor.Log.Infof("testing ethereum connection: %s", config.RPCEndpoint)

	// 重试连接测试
	err = base.Retry(ctx, processor.Log, "ethereum-connection-test", processor.Retry, func() error {
		return processor.HealthCheck(ctx)
	})
	if err != nil {
		return nil, fmt.Errorf("以太坊连接测试失败: %w", err)
	}

	processor.Log.Infof("%s processor initialized: %s (chainID: %d)", processor.ChainType, config.RPCEndpoint, config.ChainID)
	return processor, nil
}

// GetChainID 获取链ID
func (e *EthereumProcessor) GetChainID() string {
	return e.chainID.String()
}

// GetLatestBlockNumber 获取最新区块号
func (e *EthereumProcessor) GetLatestBlockNumber(ctx context.Context) (*big.Int, error) {
	blockNumber, err := e.client.BlockNumber(ctx)
	if err != nil {
		return nil, fmt.Errorf("获取最新区块号失败: %w", err)
	}

	return big.NewInt(int64(blockNumber)), nil
}

// GetTransactionsByBlockRange 批量获取交易 (通过区块范围)
func (e *EthereumProcessor) GetTransactionsByBlockRange(ctx context.Context, startBlock, endBlock *big.Int) ([]types.UnifiedTransaction, error) {
	var allTransactions []types.UnifiedTransaction

	// 遍历区块范围
	for blockNum := new(big.Int).Set(startBlock); blockNum.Cmp(endBlock) <= 0; blockNum.Add(blockNum, big.NewInt(1)) {
		// 获取区块数据
		block, err := e.client.BlockByNumber(ctx, blockNum)
		if err != nil {
			e.Log.Warnf("failed to get block %s: %v", blockNum.String(), err)
			continue
		}

		// 获取区块中的所有交易收据
		transactions, err := e.getTransactionsFromBlock(ctx, block)
		if err != nil {
			e.Log.Warnf("failed to get block %s transactions: %v", blockNum.String(), err)
			continue
		}

		allTransactions = append(allTransactions, transactions...)
	}

	return allTransactions, nil
}

// GetBlocksByRange 批量获取区块数据 (新接口方法)
func (e *EthereumProcessor) GetBlocksByRange(ctx context.Context, startBlock, endBlock *big.Int) ([]types.UnifiedBlock, error) {
	var allBlocks []types.UnifiedBlock

	// 遍历区块范围
	for blockNum := new(big.Int).Set(startBlock); blockNum.Cmp(endBlock) <= 0; blockNum.Add(blockNum, big.NewInt(1)) {
		// 获取单个区块
		unifiedBlock, err := e.GetBlock(ctx, blockNum)
		if err != nil {
			e.Log.Warnf("failed to get block %s: %v", blockNum.String(), err)
			continue
		}

		allBlocks = append(allBlocks, *unifiedBlock)
	}

	return allBlocks, nil
}

// GetBlock 获取单个区块
func (e *EthereumProcessor) GetBlock(ctx context.Context, blockNumber *big.Int) (*types.UnifiedBlock, error) {
	// 获取区块数据
	block, err := e.client.BlockByNumber(ctx, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("获取区块失败: %w", err)
	}

	// 获取区块中的所有交易
	transactions, err := e.getTransactionsFromBlock(ctx, block)
	if err != nil {
		return nil, fmt.Errorf("获取区块交易失败: %w", err)
	}

	// 构建统一区块结构
	unifiedBlock := &types.UnifiedBlock{
		BlockNumber:  block.Number(),
		BlockHash:    block.Hash().Hex(),
		ChainType:    e.ChainType,
		ChainID:      e.chainID.String(),
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

// GetTransaction 获取单个交易
func (e *EthereumProcessor) GetTransaction(ctx context.Context, txHash string) (*types.UnifiedTransaction, error) {
	hash := common.HexToHash(txHash)

	// 获取交易信息
	tx, isPending, err := e.client.TransactionByHash(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("获取交易失败: %w", err)
	}

	if isPending {
		return e.convertPendingTransaction(tx), nil
	}

	// 获取交易收据
	receipt, err := e.client.TransactionReceipt(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("获取交易收据失败: %w", err)
	}

	// 获取区块信息
	block, err := e.client.BlockByHash(ctx, receipt.BlockHash)
	if err != nil {
		return nil, fmt.Errorf("获取区块信息失败: %w", err)
	}

	// 转换为统一交易格式
	unifiedTx, err := e.convertToUnifiedTransaction(tx, receipt, block)
	if err != nil {
		return nil, fmt.Errorf("转换交易格式失败: %w", err)
	}

	return unifiedTx, nil
}

// HealthCheck 健康检查
func (e *EthereumProcessor) HealthCheck(ctx context.Context) error {
	_, err := e.client.BlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("%s健康检查失败: %w", e.ChainType, err)
	}
	return nil
}

// getTransactionsFromBlock 从区块中获取所有交易
func (e *EthereumProcessor) getTransactionsFromBlock(ctx context.Context, block *ethtypes.Block) ([]types.UnifiedTransaction, error) {
	var transactions []types.UnifiedTransaction

	blockTxCount := len(block.Transactions())
	e.Log.Infof("processing block %s, %d transactions", block.Number().String(), blockTxCount)

	// 如果交易数量太多，限制处理数量以避免超时
	maxTxPerBlock := 100
	if blockTxCount > maxTxPerBlock {
		e.Log.Warnf("too many transactions in block (%d), limiting to %d", blockTxCount, maxTxPerBlock)
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
	semaphore := make(chan struct{}, 10) // 限制并发数为10

	// 启动goroutines获取收据
	for i := 0; i < blockTxCount; i++ {
		go func(index int, tx *ethtypes.Transaction) {
			semaphore <- struct{}{}        // 获取信号量
			defer func() { <-semaphore }() // 释放信号量

			// 为每个请求设置超时
			receiptCtx, cancel := context.WithTimeout(ctx, time.Second*30)
			defer cancel()

			receipt, err := e.client.TransactionReceipt(receiptCtx, tx.Hash())
			//var receipts []*ethtypes.Receipt
			//err := e.rpcClient.CallContext(receiptCtx, &receipts, "eth_getBlockReceipts", block.Hash().Hex())
			//if err != nil {
			//	log.Printf("获取区块 %s 的 Receipts 失败: %v", block.Hash().Hex(), err)
			//}
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
			e.Log.Warnf("failed to get transaction receipt (index: %d): %v", result.index, result.err)
			continue
		}
		receipts[result.index] = result.receipt
	}

	// 转换交易 - 只处理我们实际获取的交易数量
	for i := 0; i < blockTxCount; i++ {
		if receipts[i] == nil {
			continue
		}

		tx := block.Transactions()[i]
		unifiedTx, err := e.convertToUnifiedTransaction(tx, receipts[i], block)
		if err != nil {
			e.Log.Warnf("failed to convert transaction (hash: %s): %v", tx.Hash().Hex(), err)
			continue
		}

		transactions = append(transactions, *unifiedTx)
	}

	e.Log.Infof("block %s processed, converted %d transactions", block.Number().String(), len(transactions))

	return transactions, nil
}

// convertToUnifiedTransaction 转换为统一交易格式
func (e *EthereumProcessor) convertToUnifiedTransaction(tx *ethtypes.Transaction, receipt *ethtypes.Receipt, block *ethtypes.Block) (*types.UnifiedTransaction, error) {
	// 基础信息
	unifiedTx := &types.UnifiedTransaction{
		TxHash:      tx.Hash().Hex(),
		ChainType:   e.ChainType,
		ChainID:     e.GetChainID(),
		BlockNumber: receipt.BlockNumber,
		BlockHash:   receipt.BlockHash.Hex(),
		TxIndex:     int(receipt.TransactionIndex),
		GasLimit:    big.NewInt(int64(tx.Gas())),
		GasUsed:     big.NewInt(int64(receipt.GasUsed)),
		GasPrice:    tx.GasPrice(),
		Value:       tx.Value(),
		Timestamp:   time.Unix(int64(block.Time()), 0),
		RawData: map[string]interface{}{
			"transaction": tx,
			"receipt":     receipt,
			"block":       block,
		},
	}

	// 发送者地址 - 支持多种交易类型
	var from common.Address
	var err error
	signers := []ethtypes.Signer{
		ethtypes.LatestSignerForChainID(e.chainID),
		ethtypes.NewEIP155Signer(e.chainID),
		ethtypes.NewLondonSigner(e.chainID),
		ethtypes.HomesteadSigner{},
	}

	for _, signer := range signers {
		from, err = ethtypes.Sender(signer, tx)
		if err == nil {
			break
		}
	}

	if err != nil {
		e.Log.Warnf("failed to get sender address (hash: %s): %v", tx.Hash().Hex(), err)
		from = common.Address{}
	}
	unifiedTx.FromAddress = utils.NormalizeAddress(from.Hex())

	// 接收者地址
	if tx.To() != nil {
		unifiedTx.ToAddress = utils.NormalizeAddress(tx.To().Hex())
	}

	// 交易状态
	if receipt.Status == ethtypes.ReceiptStatusSuccessful {
		unifiedTx.Status = types.TransactionStatusSuccess
	} else {
		unifiedTx.Status = types.TransactionStatusFailed
	}

	return unifiedTx, nil
}

// convertPendingTransaction 转换待处理交易
func (e *EthereumProcessor) convertPendingTransaction(tx *ethtypes.Transaction) *types.UnifiedTransaction {
	unifiedTx := &types.UnifiedTransaction{
		TxHash:    tx.Hash().Hex(),
		ChainType: e.ChainType,
		ChainID:   e.GetChainID(),
		GasLimit:  big.NewInt(int64(tx.Gas())),
		GasPrice:  tx.GasPrice(),
		Value:     tx.Value(),
		Status:    types.TransactionStatusPending,
		Timestamp: time.Now(),
		RawData:   tx,
	}

	// 尝试获取发送者地址
	if from, err := ethtypes.Sender(ethtypes.LatestSignerForChainID(e.chainID), tx); err == nil {
		unifiedTx.FromAddress = utils.NormalizeAddress(from.Hex())
	}

	// 接收者地址
	if tx.To() != nil {
		unifiedTx.ToAddress = utils.NormalizeAddress(tx.To().Hex())
	}

	return unifiedTx
}
