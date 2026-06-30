package influxdb

import (
	"context"
	"fmt"
	"sync"
	"time"

	"unified-tx-parser/internal/model"
	"unified-tx-parser/internal/types"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/sirupsen/logrus"
)

var log = logrus.WithFields(logrus.Fields{"service": "parser", "module": "storage-influxdb"})

// InfluxDBConfig InfluxDB配置
type InfluxDBConfig struct {
	URL       string `yaml:"url"`        // InfluxDB服务器地址
	Token     string `yaml:"token"`      // 认证令牌
	Org       string `yaml:"org"`        // 组织名称
	Bucket    string `yaml:"bucket"`     // 存储桶名称
	BatchSize int    `yaml:"batch_size"` // 批量写入大小
	FlushTime int    `yaml:"flush_time"` // 刷新时间(秒)
	Precision string `yaml:"precision"`  // 时间精度
}

// DexDataBatch DEX数据批量缓存
type DexDataBatch struct {
	Pools        []model.Pool
	Tokens       []model.Token
	Reserves     []model.Reserve
	Transactions []model.Transaction
	Liquidities  []model.Liquidity
	TotalCount   int
	LastUpdated  time.Time
}

// SimpleInfluxDBStorage 简化的InfluxDB存储引擎
type SimpleInfluxDBStorage struct {
	config     *InfluxDBConfig
	client     influxdb2.Client
	writeAPI   api.WriteAPI
	queryAPI   api.QueryAPI
	ctx        context.Context
	batchCache *DexDataBatch
	batchMutex sync.RWMutex
}

// NewSimpleInfluxDBStorage 创建简化的InfluxDB存储引擎
func NewSimpleInfluxDBStorage(config *InfluxDBConfig) (*SimpleInfluxDBStorage, error) {
	if config == nil {
		return nil, fmt.Errorf("InfluxDB配置不能为空")
	}

	// 设置默认值
	if config.BatchSize == 0 {
		config.BatchSize = 1000
	}
	if config.FlushTime == 0 {
		config.FlushTime = 10
	}
	if config.Precision == "" {
		config.Precision = "ms"
	}

	// 创建InfluxDB客户端
	client := influxdb2.NewClientWithOptions(
		config.URL,
		config.Token,
		influxdb2.DefaultOptions().
			SetBatchSize(uint(config.BatchSize)).
			SetFlushInterval(uint(config.FlushTime*1000)), // 转换为毫秒
	)

	// 测试连接
	ctx := context.Background()
	health, err := client.Health(ctx)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("连接InfluxDB失败: %w", err)
	}

	if health.Status != "pass" {
		client.Close()
		message := ""
		if health.Message != nil {
			message = *health.Message
		}
		return nil, fmt.Errorf("InfluxDB健康检查失败: %s", message)
	}

	// 创建写入和查询API
	writeAPI := client.WriteAPI(config.Org, config.Bucket)
	queryAPI := client.QueryAPI(config.Org)

	storage := &SimpleInfluxDBStorage{
		config:   config,
		client:   client,
		writeAPI: writeAPI,
		queryAPI: queryAPI,
		ctx:      ctx,
		batchCache: &DexDataBatch{
			Pools:        make([]model.Pool, 0),
			Tokens:       make([]model.Token, 0),
			Reserves:     make([]model.Reserve, 0),
			Transactions: make([]model.Transaction, 0),
			Liquidities:  make([]model.Liquidity, 0),
			LastUpdated:  time.Now(),
		},
	}

	// 启动定时刷新goroutine
	go storage.startBatchFlushTimer()

	log.Infof("influxdb storage engine initialized: %s/%s (batchSize=%d, flushTime=%ds)",
		config.URL, config.Bucket, config.BatchSize, config.FlushTime)
	return storage, nil
}

// StoreTransactions 存储交易数据
func (s *SimpleInfluxDBStorage) StoreTransactions(ctx context.Context, transactions []types.UnifiedTransaction) error {
	if len(transactions) == 0 {
		return nil
	}

	// 批量写入交易数据
	for _, tx := range transactions {
		point := influxdb2.NewPoint(
			"transactions",
			map[string]string{
				"chain_type":   string(tx.ChainType),
				"tx_hash":      tx.TxHash,
				"from_address": tx.FromAddress,
				"to_address":   tx.ToAddress,
			},
			map[string]interface{}{
				"block_number": tx.BlockNumber.String(),
				"value":        tx.Value.String(),
				"gas_used":     tx.GasUsed.String(),
				"gas_price":    tx.GasPrice.String(),
				"status":       string(tx.Status),
			},
			tx.Timestamp,
		)
		s.writeAPI.WritePoint(point)
	}

	// 强制刷新
	s.writeAPI.Flush()

	log.Infof("stored %d transactions to influxdb", len(transactions))
	return nil
}

// StoreBlocks 存储区块数据
func (s *SimpleInfluxDBStorage) StoreBlocks(ctx context.Context, blocks []types.UnifiedBlock) error {
	if len(blocks) == 0 {
		return nil
	}

	// 批量写入区块数据
	for _, block := range blocks {
		point := influxdb2.NewPoint(
			"blocks",
			map[string]string{
				"chain_type": string(block.ChainType),
				"block_hash": block.BlockHash,
			},
			map[string]interface{}{
				"block_number": block.BlockNumber.String(),
				"parent_hash":  block.ParentHash,
				"tx_count":     block.TxCount,
				"size":         block.Size,
			},
			block.Timestamp,
		)
		s.writeAPI.WritePoint(point)
	}

	// 强制刷新
	s.writeAPI.Flush()

	log.Infof("stored %d blocks to influxdb", len(blocks))
	return nil
}

// StoreDexData 存储DEX数据 - 实现批量缓存和不同的插入策略
func (s *SimpleInfluxDBStorage) StoreDexData(ctx context.Context, dexData *types.DexData) error {
	if dexData == nil {
		return nil
	}

	// 添加数据到批量缓存
	s.addToBatch(dexData)

	// 检查是否需要刷新批量缓存
	if s.shouldFlushBatch() {
		return s.flushBatch(ctx)
	}

	return nil
}

// addToBatch 将数据添加到批量缓存
func (s *SimpleInfluxDBStorage) addToBatch(dexData *types.DexData) {
	s.batchMutex.Lock()
	defer s.batchMutex.Unlock()

	// 添加各类数据到缓存
	s.batchCache.Pools = append(s.batchCache.Pools, dexData.Pools...)
	s.batchCache.Tokens = append(s.batchCache.Tokens, dexData.Tokens...)
	s.batchCache.Reserves = append(s.batchCache.Reserves, dexData.Reserves...)
	s.batchCache.Transactions = append(s.batchCache.Transactions, dexData.Transactions...)
	s.batchCache.Liquidities = append(s.batchCache.Liquidities, dexData.Liquidities...)

	// 更新总数和时间
	s.batchCache.TotalCount = len(s.batchCache.Pools) + len(s.batchCache.Tokens) +
		len(s.batchCache.Reserves) + len(s.batchCache.Transactions) + len(s.batchCache.Liquidities)
	s.batchCache.LastUpdated = time.Now()

	log.Infof("[batch cache] added data: pools=%d, tokens=%d, reserves=%d, transactions=%d, liquidities=%d, total=%d",
		len(dexData.Pools), len(dexData.Tokens), len(dexData.Reserves),
		len(dexData.Transactions), len(dexData.Liquidities), s.batchCache.TotalCount)
}

// shouldFlushBatch 检查是否应该刷新批量缓存
func (s *SimpleInfluxDBStorage) shouldFlushBatch() bool {
	s.batchMutex.RLock()
	defer s.batchMutex.RUnlock()

	// 条件1: 达到批量大小
	if s.batchCache.TotalCount >= s.config.BatchSize {
		log.Infof("[batch flush] reached batch size threshold: %d >= %d", s.batchCache.TotalCount, s.config.BatchSize)
		return true
	}

	// 条件2: 超过最大等待时间
	maxWaitTime := time.Duration(s.config.FlushTime) * time.Second
	if time.Since(s.batchCache.LastUpdated) > maxWaitTime && s.batchCache.TotalCount > 0 {
		log.Infof("[batch flush] exceeded max wait time: %v, current cache: %d", maxWaitTime, s.batchCache.TotalCount)
		return true
	}

	return false
}

// flushBatch 刷新批量缓存到数据库
func (s *SimpleInfluxDBStorage) flushBatch(ctx context.Context) error {
	s.batchMutex.Lock()
	defer s.batchMutex.Unlock()

	if s.batchCache.TotalCount == 0 {
		return nil
	}

	log.Infof("[batch flush] flushing batch cache: %d records", s.batchCache.TotalCount)
	startTime := time.Now()
	totalRecords := 0
	var errors []error

	// 存储池子数据 - UPSERT策略
	for _, pool := range s.batchCache.Pools {
		if err := s.upsertPool(ctx, pool); err != nil {
			log.Warnf("failed to store pool data: %v", err)
			errors = append(errors, err)
			continue
		}
		totalRecords++
	}

	// 存储代币数据 - UPSERT策略
	for _, token := range s.batchCache.Tokens {
		if err := s.upsertToken(ctx, token); err != nil {
			log.Warnf("failed to store token data: %v", err)
			errors = append(errors, err)
			continue
		}
		totalRecords++
	}

	// 存储储备数据 - UPSERT策略
	for _, reserve := range s.batchCache.Reserves {
		if err := s.upsertReserve(ctx, reserve); err != nil {
			log.Warnf("failed to store reserve data: %v", err)
			errors = append(errors, err)
			continue
		}
		totalRecords++
	}

	// 存储交易数据 - 直接INSERT策略
	for _, tx := range s.batchCache.Transactions {
		if err := s.insertTransaction(ctx, tx); err != nil {
			log.Warnf("failed to store transaction data: %v", err)
			errors = append(errors, err)
			continue
		}
		totalRecords++
	}

	// 存储流动性数据 - 直接INSERT策略
	for _, liquidity := range s.batchCache.Liquidities {
		if err := s.insertLiquidity(ctx, liquidity); err != nil {
			log.Warnf("failed to store liquidity data: %v", err)
			errors = append(errors, err)
			continue
		}
		totalRecords++
	}

	// 强制刷新到InfluxDB
	s.writeAPI.Flush()

	// 清空批量缓存
	s.clearBatchCache()

	duration := time.Since(startTime)
	log.Infof("[batch flush] completed: success=%d, failed=%d, duration=%v",
		totalRecords, len(errors), duration)

	// 如果有错误，返回第一个错误
	if len(errors) > 0 {
		return fmt.Errorf("批量写入部分失败: %d个错误, 第一个错误: %w", len(errors), errors[0])
	}

	return nil
}

// clearBatchCache 清空批量缓存
func (s *SimpleInfluxDBStorage) clearBatchCache() {
	s.batchCache.Pools = s.batchCache.Pools[:0]
	s.batchCache.Tokens = s.batchCache.Tokens[:0]
	s.batchCache.Reserves = s.batchCache.Reserves[:0]
	s.batchCache.Transactions = s.batchCache.Transactions[:0]
	s.batchCache.Liquidities = s.batchCache.Liquidities[:0]
	s.batchCache.TotalCount = 0
	s.batchCache.LastUpdated = time.Now()
}

// startBatchFlushTimer 启动定时刷新批量缓存的定时器
func (s *SimpleInfluxDBStorage) startBatchFlushTimer() {
	ticker := time.NewTicker(time.Duration(s.config.FlushTime) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// 检查是否有数据需要刷新
			count, lastUpdated := s.GetBatchCacheStatus()
			if count > 0 {
				maxWaitTime := time.Duration(s.config.FlushTime) * time.Second
				if time.Since(lastUpdated) >= maxWaitTime {
					log.Infof("[timer flush] flushing batch cache: %d records", count)
					if err := s.flushBatch(context.Background()); err != nil {
						log.Warnf("[timer flush] flush failed: %v", err)
					}
				}
			}
		case <-s.ctx.Done():
			// 上下文取消，退出定时器
			log.Infof("[timer] batch flush timer stopped")
			return
		}
	}
}

// GetTransactionsByHash 根据哈希获取多个交易
func (s *SimpleInfluxDBStorage) GetTransactionsByHash(ctx context.Context, hashes []string) ([]types.UnifiedTransaction, error) {
	return []types.UnifiedTransaction{}, nil
}

// GetStorageStats 获取存储统计信息
func (s *SimpleInfluxDBStorage) GetStorageStats(ctx context.Context) (map[string]interface{}, error) {
	stats := make(map[string]interface{})
	stats["storage_type"] = "influxdb"
	stats["bucket"] = s.config.Bucket
	stats["url"] = s.config.URL
	stats["status"] = "connected"
	return stats, nil
}

// HealthCheck 健康检查
func (s *SimpleInfluxDBStorage) HealthCheck(ctx context.Context) error {
	health, err := s.client.Health(ctx)
	if err != nil {
		return fmt.Errorf("InfluxDB健康检查失败: %w", err)
	}

	if health.Status != "pass" {
		message := ""
		if health.Message != nil {
			message = *health.Message
		}
		return fmt.Errorf("InfluxDB健康检查失败: %s", message)
	}

	return nil
}

// FlushBatchCache 强制刷新批量缓存
func (s *SimpleInfluxDBStorage) FlushBatchCache(ctx context.Context) error {
	return s.flushBatch(ctx)
}

// GetBatchCacheStatus 获取批量缓存状态
func (s *SimpleInfluxDBStorage) GetBatchCacheStatus() (int, time.Time) {
	s.batchMutex.RLock()
	defer s.batchMutex.RUnlock()
	return s.batchCache.TotalCount, s.batchCache.LastUpdated
}

// GetAllPoolTokens returns addr -> {token0, token1} for every pool that has
// non-empty token addresses recorded in dex_pools. Pools produced only by
// the lazy-pool placeholder path (no PairCreated/PoolCreated scanned, no
// successful eth_call resolution) have empty token_0/token_1 tags and are
// excluded — there is nothing useful to warm the cache with for those.
//
// Used at startup to pre-populate the in-memory seenPools cache so a process
// restart doesn't re-trigger eth_call lookups for pools already resolved
// before the restart.
func (s *SimpleInfluxDBStorage) GetAllPoolTokens(ctx context.Context) (map[string][2]string, error) {
	query := fmt.Sprintf(`
		import "influxdata/influxdb/schema"
		from(bucket: "%s")
		|> range(start: -365d)
		|> filter(fn: (r) => r._measurement == "dex_pools")
		|> filter(fn: (r) => r._field == "fee")
		|> group(columns: ["addr", "token_0", "token_1"])
		|> last()
		|> group()
		|> keep(columns: ["addr", "token_0", "token_1"])
	`, s.config.Bucket)

	result, err := s.queryAPI.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query pool tokens: %w", err)
	}
	defer result.Close()

	out := make(map[string][2]string)
	for result.Next() {
		rec := result.Record()
		addr, _ := rec.ValueByKey("addr").(string)
		token0, _ := rec.ValueByKey("token_0").(string)
		token1, _ := rec.ValueByKey("token_1").(string)
		if addr == "" || token0 == "" || token1 == "" {
			continue // skip placeholder pools with unresolved tokens
		}
		out[addr] = [2]string{token0, token1}
	}
	if result.Err() != nil {
		return nil, fmt.Errorf("read pool tokens: %w", result.Err())
	}
	return out, nil
}

// Close 关闭存储引擎
func (s *SimpleInfluxDBStorage) Close() error {
	// 在关闭前刷新剩余的批量缓存
	if s.batchCache.TotalCount > 0 {
		log.Infof("[close] flushing remaining cache before shutdown: %d records", s.batchCache.TotalCount)
		if err := s.flushBatch(context.Background()); err != nil {
			log.Warnf("[close] failed to flush cache before shutdown: %v", err)
		}
	}

	if s.writeAPI != nil {
		s.writeAPI.Flush()
	}
	if s.client != nil {
		s.client.Close()
	}
	log.Infof("influxdb storage engine closed")
	return nil
}

// upsertPool 池子数据UPSERT操作
func (s *SimpleInfluxDBStorage) upsertPool(ctx context.Context, pool model.Pool) error {
	tags := map[string]string{
		"addr":     pool.Addr,
		"protocol": pool.Protocol,
		"factory":  pool.Factory,
	}

	fields := map[string]interface{}{
		"fee": pool.Fee,
	}

	// 添加代币信息
	for idx, token := range pool.Tokens {
		tags[fmt.Sprintf("token_%d", idx)] = token
	}

	// 添加额外信息
	timestamp := time.Now()
	if pool.Extra != nil {
		fields["tx_hash"] = pool.Extra.Hash
		fields["tx_from"] = pool.Extra.From
		fields["tx_time"] = pool.Extra.Time
		fields["stable"] = pool.Extra.Stable
		if pool.Extra.Time > 0 {
			timestamp = time.Unix(int64(pool.Extra.Time), 0)
		}
	}

	// InfluxDB upsert semantics: writing the same measurement+tags+timestamp
	// overwrites the previous point's fields. No need to query existence
	// first just to pick between "created"/"updated" markers — that round
	// trip (previously poolExists) was costing one query per pool with no
	// downstream logic depending on the distinction.
	fields["updated"] = true
	fields["update_time"] = time.Now().Unix()

	point := influxdb2.NewPoint(
		"dex_pools",
		tags,
		fields,
		timestamp,
	)
	s.writeAPI.WritePoint(point)
	return nil
}

// upsertToken 代币数据UPSERT操作
func (s *SimpleInfluxDBStorage) upsertToken(ctx context.Context, token model.Token) error {
	tags := map[string]string{
		"addr":   token.Addr,
		"name":   token.Name,
		"symbol": token.Symbol,
	}

	fields := map[string]interface{}{
		"decimals":      token.Decimals,
		"trl_threshold": token.TRLThreshold,
		"is_stable":     token.IsStable,
		"created_at":    token.CreatedAt,
	}

	// See upsertPool: InfluxDB overwrites same measurement+tags+timestamp
	// points, so no existence query is needed just to pick this marker.
	fields["updated"] = true
	fields["update_time"] = time.Now().Unix()

	point := influxdb2.NewPoint(
		"dex_tokens",
		tags,
		fields,
		time.Now(),
	)
	s.writeAPI.WritePoint(point)
	return nil
}

// upsertReserve 储备数据UPSERT操作
func (s *SimpleInfluxDBStorage) upsertReserve(ctx context.Context, reserve model.Reserve) error {
	tags := map[string]string{
		"addr":     reserve.Addr,
		"protocol": reserve.Protocol,
	}

	fields := map[string]interface{}{
		"event_time": reserve.Time,
	}

	// 添加金额信息
	for idx, amount := range reserve.Amounts {
		fields[fmt.Sprintf("amount_%d", idx)] = amount.String()
	}

	// 添加价值信息
	for idx, value := range reserve.Value {
		fields[fmt.Sprintf("value_%d", idx)] = value
	}

	// See upsertPool: InfluxDB overwrites same measurement+tags+timestamp
	// points, so no existence query is needed just to pick this marker.
	// This was the single biggest contributor to slow batch flushes — with
	// reserves dominating most batches by count, the per-record existence
	// query (one round trip per reserve) was the majority of total flush time.
	fields["updated"] = true
	fields["update_time"] = time.Now().Unix()

	point := influxdb2.NewPoint(
		"dex_reserves",
		tags,
		fields,
		time.Unix(int64(reserve.Time), 0),
	)
	s.writeAPI.WritePoint(point)
	return nil
}

// insertTransaction 直接插入交易数据
func (s *SimpleInfluxDBStorage) insertTransaction(ctx context.Context, tx model.Transaction) error {
	tags := map[string]string{
		"addr":     tx.Addr,
		"hash":     tx.Hash,
		"from":     tx.From,
		"side":     tx.Side,
		"pool":     tx.Pool,
		"router":   tx.Router,
		"factory":  tx.Factory,
		"protocol": tx.Protocol,
	}

	fields := map[string]interface{}{
		"amount":       tx.Amount.String(),
		"price":        tx.Price,
		"value":        tx.Value,
		"event_time":   tx.Time,
		"event_index":  tx.EventIndex,
		"tx_index":     tx.TxIndex,
		"swap_index":   tx.SwapIndex,
		"block_number": tx.BlockNumber,
		"created":      true,
		"create_time":  time.Now().Unix(),
	}

	point := influxdb2.NewPoint(
		"dex_transactions",
		tags,
		fields,
		time.Unix(int64(tx.Time), 0),
	)
	s.writeAPI.WritePoint(point)
	return nil
}

// insertLiquidity 直接插入流动性数据
func (s *SimpleInfluxDBStorage) insertLiquidity(ctx context.Context, liquidity model.Liquidity) error {
	tags := map[string]string{
		"addr":    liquidity.Addr,
		"hash":    liquidity.Hash,
		"from":    liquidity.From,
		"pool":    liquidity.Pool,
		"router":  liquidity.Router,
		"factory": liquidity.Factory,
		"side":    liquidity.Side,
		"pos":     liquidity.Pos,
		"key":     liquidity.Key,
	}

	fields := map[string]interface{}{
		"amount":      liquidity.Amount.String(),
		"value":       liquidity.Value,
		"event_time":  liquidity.Time,
		"created":     true,
		"create_time": time.Now().Unix(),
	}

	point := influxdb2.NewPoint(
		"dex_liquidities",
		tags,
		fields,
		time.Unix(int64(liquidity.Time), 0),
	)
	s.writeAPI.WritePoint(point)
	return nil
}

// poolExists 检查池子是否存在
func (s *SimpleInfluxDBStorage) poolExists(ctx context.Context, addr string) (bool, error) {
	query := fmt.Sprintf(`
		from(bucket: "%s")
		|> range(start: -30d)
		|> filter(fn: (r) => r._measurement == "dex_pools")
		|> filter(fn: (r) => r.addr == "%s")
		|> limit(n: 1)
	`, s.config.Bucket, addr)

	result, err := s.queryAPI.Query(ctx, query)
	if err != nil {
		return false, err
	}
	defer result.Close()

	return result.Next(), nil
}

// tokenExists 检查代币是否存在
func (s *SimpleInfluxDBStorage) tokenExists(ctx context.Context, addr string) (bool, error) {
	query := fmt.Sprintf(`
		from(bucket: "%s")
		|> range(start: -30d)
		|> filter(fn: (r) => r._measurement == "dex_tokens")
		|> filter(fn: (r) => r.addr == "%s")
		|> limit(n: 1)
	`, s.config.Bucket, addr)

	result, err := s.queryAPI.Query(ctx, query)
	if err != nil {
		return false, err
	}
	defer result.Close()

	return result.Next(), nil
}

// reserveExists 检查储备是否存在
func (s *SimpleInfluxDBStorage) reserveExists(ctx context.Context, addr string, timestamp uint64) (bool, error) {
	query := fmt.Sprintf(`
		from(bucket: "%s")
		|> range(start: -30d)
		|> filter(fn: (r) => r._measurement == "dex_reserves")
		|> filter(fn: (r) => r.addr == "%s")
		|> filter(fn: (r) => r.time == %d)
		|> limit(n: 1)
	`, s.config.Bucket, addr, timestamp)

	result, err := s.queryAPI.Query(ctx, query)
	if err != nil {
		return false, err
	}
	defer result.Close()

	return result.Next(), nil
}

// NewInfluxDBStorage 创建InfluxDB存储引擎（使用简化版本）
func NewInfluxDBStorage(config *InfluxDBConfig) (types.StorageEngine, error) {
	return NewSimpleInfluxDBStorage(config)
}
