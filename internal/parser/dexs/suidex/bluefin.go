package suidex

import (
	"context"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"

	"unified-tx-parser/internal/model"
	"unified-tx-parser/internal/parser/chains/sui"
	"unified-tx-parser/internal/types"
	"unified-tx-parser/internal/utils"

	"github.com/block-vision/sui-go-sdk/models"
	"github.com/sirupsen/logrus"
)

var bluefinLog = logrus.WithFields(logrus.Fields{"service": "parser", "module": "dex-bluefin"})

// Bluefin协议常量
const (
	// Bluefin AMM合约地址
	bluefinAmmAddr = "0x3492c874c1e3b3e2984e8c41b589e642d4d0a5d6459e5a9cfc2d52fd7c89c267"

	// Bluefin事件类型
	bluefinPoolCreatedEventType     = bluefinAmmAddr + "::events::PoolCreated"
	bluefinAddLiquidityEventType    = bluefinAmmAddr + "::events::LiquidityProvided"
	bluefinRemoveLiquidityEventType = bluefinAmmAddr + "::events::LiquidityRemoved"
	bluefinAssetSwapEventType       = bluefinAmmAddr + "::events::AssetSwap"
	bluefinFlashSwapEventType       = bluefinAmmAddr + "::events::FlashSwap"
)

// TokenCacheItem 代币缓存项
type TokenCacheItem struct {
	Token     model.Token
	ExpiresAt time.Time
}

// PoolCacheItem 池对象缓存项
type PoolCacheItem struct {
	Object    models.SuiObjectResponse
	ExpiresAt time.Time
}

const (
	poolCacheTTL  = 60 * time.Second
	tokenCacheTTL = 1 * time.Hour

	stableRankThreshold = 90 // rank >= 90 视为 USD 稳定币 (price ≈ 1)
)

// defaultSuiQuoteAssets 兜底默认值，优先从配置加载
var defaultSuiQuoteAssets = map[string]int{
	"0xdba34672e30cb065b1f93e3ab55318768fd6fef66c15942c9f7cb846e2f900e7::usdc::USDC": 100,
	"0xc060006111016b8a020ad5b33834984a437aaa7d3c74c18e09a95d48aceab08c::coin::COIN":  99,
	"0x5d4b302506645c37ff133b98c4b50a5ae14841659738d6d733d59d0d217a93bf::coin::COIN":  98,
}

// BluefinExtractor Bluefin DEX数据提取器
type BluefinExtractor struct {
	client      *sui.SuiProcessor
	tokenCache  map[string]*TokenCacheItem
	poolCache   map[string]*PoolCacheItem
	cacheMutex  sync.RWMutex
	quoteAssets map[string]int // addr → rank, rank>=90 视为 USD 稳定币
}

// NewBluefinExtractor 创建Bluefin提取器
func NewBluefinExtractor() *BluefinExtractor {
	return &BluefinExtractor{
		tokenCache:  make(map[string]*TokenCacheItem),
		poolCache:   make(map[string]*PoolCacheItem),
		quoteAssets: defaultSuiQuoteAssets,
	}
}

// SetSuiProcessor 设置Sui处理器（用于获取链上数据）
func (b *BluefinExtractor) SetSuiProcessor(processor interface{}) {
	if suiProcessor, ok := processor.(*sui.SuiProcessor); ok {
		b.client = suiProcessor
	}
}

// SetQuoteAssets 设置报价资产（从配置加载），addr → rank
func (b *BluefinExtractor) SetQuoteAssets(assets map[string]int) {
	if len(assets) > 0 {
		b.cacheMutex.Lock()
		defer b.cacheMutex.Unlock()
		b.quoteAssets = assets
	}
}

// isStableCoin 判断地址是否为 USD 稳定币 (rank >= stableRankThreshold)
func (b *BluefinExtractor) isStableCoin(addr string) bool {
	b.cacheMutex.RLock()
	defer b.cacheMutex.RUnlock()
	rank, ok := b.quoteAssets[addr]
	return ok && rank >= stableRankThreshold
}

// GetSupportedProtocols 获取支持的协议
func (b *BluefinExtractor) GetSupportedProtocols() []string {
	return []string{"bluefin"}
}

// GetSupportedChains 获取支持的链类型
func (b *BluefinExtractor) GetSupportedChains() []types.ChainType {
	return []types.ChainType{types.ChainTypeSui}
}

// ExtractDexData 从统一区块数据中提取Bluefin DEX相关数据 - 简化实现
func (b *BluefinExtractor) ExtractDexData(ctx context.Context, blocks []types.UnifiedBlock) (*types.DexData, error) {
	// 检查是否设置了Sui处理器
	if b.client == nil {
		return nil, fmt.Errorf("Sui处理器未设置，请先调用SetSuiProcessor方法")
	}

	dexData := &types.DexData{
		Pools:        make([]model.Pool, 0),
		Transactions: make([]model.Transaction, 0),
		Liquidities:  make([]model.Liquidity, 0),
		Reserves:     make([]model.Reserve, 0),
		Tokens:       make([]model.Token, 0),
	}

	// 遍历所有区块
	for _, block := range blocks {
		// 只处理Sui链的区块
		if block.ChainType != types.ChainTypeSui {
			continue
		}

		// 遍历区块中的所有交易
		for _, tx := range block.Transactions {
			// 直接从交易的原始数据中提取Sui事件
			suiEvents := b.extractSuiEventsFromTransaction(&tx)
			if len(suiEvents) == 0 {
				continue
			}

			// 处理每个事件
			for _, event := range suiEvents {
				if !b.isBluefinEvent(event) {
					continue
				}

				// 根据事件类型处理
				eventType := b.getEventType(event)
				switch eventType {
				case "swap":
					swapData := b.processSwapEvent(ctx, event, &tx)
					if swapData != nil {
						dexData.Pools = append(dexData.Pools, swapData.Pool)
						dexData.Tokens = append(dexData.Tokens, swapData.Tokens...)
						dexData.Reserves = append(dexData.Reserves, swapData.Reserve)
						dexData.Transactions = append(dexData.Transactions, swapData.Transactions...)
					}

				case "add_liquidity":
					if liquidityData := b.createLiquidityFromEvent(ctx, event, &tx, "add"); liquidityData != nil {
						dexData.Pools = append(dexData.Pools, liquidityData.Pool)
						dexData.Tokens = append(dexData.Tokens, liquidityData.Tokens...)
						dexData.Reserves = append(dexData.Reserves, liquidityData.Reserve)
						dexData.Liquidities = append(dexData.Liquidities, liquidityData.Liquidities...)
					}

				case "remove_liquidity":
					if liquidityData := b.createLiquidityFromEvent(ctx, event, &tx, "remove"); liquidityData != nil {
						dexData.Pools = append(dexData.Pools, liquidityData.Pool)
						dexData.Tokens = append(dexData.Tokens, liquidityData.Tokens...)
						dexData.Reserves = append(dexData.Reserves, liquidityData.Reserve)
						dexData.Liquidities = append(dexData.Liquidities, liquidityData.Liquidities...)
					}

				case "pool_created":
					if pool := b.createPoolFromEvent(ctx, event, &tx); pool != nil {
						dexData.Pools = append(dexData.Pools, *pool)
					}
				}
			}
		}
	}

	return dexData, nil
}

const shortCoinType = "0x2::sui::SUI"

// SwapEventData 交换事件处理结果
type SwapEventData struct {
	Pool         model.Pool
	Tokens       []model.Token
	Reserve      model.Reserve
	Transactions []model.Transaction
}

// LiquidityEventData 流动性事件处理结果
type LiquidityEventData struct {
	Pool        model.Pool
	Tokens      []model.Token
	Reserve     model.Reserve
	Liquidities []model.Liquidity
}

// PoolEventData 池子事件的通用数据结构
type PoolEventData struct {
	Pool    model.Pool
	Tokens  []model.Token
	Reserve model.Reserve
}

// ExtractPoolCoin 获取pool池里面代币地址
func (b *BluefinExtractor) ExtractPoolCoin(coinType string) (string, string) {
	token0, token1 := utils.ExtractPoolTokens(coinType)

	// 特殊处理SUI代币
	if strings.EqualFold(token0, shortCoinType) {
		token0 = shortCoinType
	}
	if strings.EqualFold(token1, shortCoinType) {
		token1 = shortCoinType
	}

	return token0, token1
}

// processSwapEvent 处理交换事件
// AssetSwap/FlashSwap 事件字段:
//   a2b (bool)              - true: coinA→coinB, false: coinB→coinA
//   amount_in / amount_out  - 输入/输出金额
//   fee                     - 手续费
//   pool_coin_a_amount / pool_coin_b_amount - swap后的池子余额
func (b *BluefinExtractor) processSwapEvent(ctx context.Context, event map[string]interface{}, tx *types.UnifiedTransaction) *SwapEventData {
	eventSeq := b.extractEventSeq(event)
	sender := b.extractSender(event)

	parsedJson := event["parsedJson"]
	if parsedJson == nil {
		bluefinLog.Warn("asset swap parsedJson is nil")
		return nil
	}

	fields, ok := parsedJson.(map[string]interface{})
	if !ok {
		bluefinLog.Warn("asset swap parsedJson is not a map")
		return nil
	}

	poolId := b.getStringField(fields, "pool_id")
	amountIn := b.getBigIntField(fields, "amount_in")
	amountOut := b.getBigIntField(fields, "amount_out")
	a2b := b.getBoolField(fields, "a2b")

	poolEventData, err := b.getPoolEventData(ctx, poolId, tx)
	if err != nil {
		bluefinLog.Errorf("asset swap failed to get pool event data: %v", err)
		return nil
	}

	token0 := poolEventData.Pool.Tokens[0]
	token1 := poolEventData.Pool.Tokens[1]

	// 根据 a2b 确定 sell/buy 对应的 token
	//   a2b=true:  用户卖出 coinA(token0), 买入 coinB(token1)
	//   a2b=false: 用户卖出 coinB(token1), 买入 coinA(token0)
	var sellAddr, buyAddr string
	if a2b {
		sellAddr = token0
		buyAddr = token1
	} else {
		sellAddr = token1
		buyAddr = token0
	}

	// 事件中已包含 swap 后的池子余额，优先使用
	coinAReserve := b.getBigIntField(fields, "pool_coin_a_amount")
	coinBReserve := b.getBigIntField(fields, "pool_coin_b_amount")
	if coinAReserve != nil && coinBReserve != nil {
		poolEventData.Reserve.Amounts[0] = coinAReserve
		poolEventData.Reserve.Amounts[1] = coinBReserve
	}

	pv := b.calcSwapPriceValue(sellAddr, buyAddr, amountIn, amountOut, poolEventData.Tokens)

	return &SwapEventData{
		Pool:    poolEventData.Pool,
		Tokens:  poolEventData.Tokens,
		Reserve: poolEventData.Reserve,
		Transactions: []model.Transaction{
			{
				Addr:        sellAddr,
				Factory:     bluefinAmmAddr,
				Pool:        poolId,
				Hash:        tx.TxHash,
				EventIndex:  b.parseEventSeq(eventSeq),
				TxIndex:     int64(tx.TxIndex),
				BlockNumber: b.getBlockNumber(tx),
				Time:        uint64(tx.Timestamp.Unix()),
				From:        sender,
				Side:        "sell",
				Amount:      amountIn,
				Price:       pv.SellPrice,
				Value:       pv.TradeValue,
			},
			{
				Addr:        buyAddr,
				Factory:     bluefinAmmAddr,
				Pool:        poolId,
				Hash:        tx.TxHash,
				EventIndex:  b.parseEventSeq(eventSeq),
				TxIndex:     int64(tx.TxIndex),
				BlockNumber: b.getBlockNumber(tx),
				Time:        uint64(tx.Timestamp.Unix()),
				From:        sender,
				Side:        "buy",
				Amount:      amountOut,
				Price:       pv.BuyPrice,
				Value:       pv.TradeValue,
			},
		},
	}
}

func (b *BluefinExtractor) getPoolTokenBalances(poolObject models.SuiObjectResponse) (*big.Int, *big.Int, error) {
	if poolObject.Data.Content.Fields == nil {
		return nil, nil, fmt.Errorf("pool content fields is nil")
	}

	balanceCoinA, ok := poolObject.Data.Content.Fields["coin_a"].(string)
	if !ok || balanceCoinA == "" {
		return nil, nil, fmt.Errorf("coin_a field missing or not a string")
	}
	balanceCoinB, ok := poolObject.Data.Content.Fields["coin_b"].(string)
	if !ok || balanceCoinB == "" {
		return nil, nil, fmt.Errorf("coin_b field missing or not a string")
	}

	coinAValue, ok := new(big.Int).SetString(balanceCoinA, 10)
	if !ok {
		return nil, nil, fmt.Errorf("failed to parse coin_a balance: %s", balanceCoinA)
	}
	coinBValue, ok := new(big.Int).SetString(balanceCoinB, 10)
	if !ok {
		return nil, nil, fmt.Errorf("failed to parse coin_b balance: %s", balanceCoinB)
	}
	return coinAValue, coinBValue, nil
}

// SupportsBlock 检查是否支持该区块
// 只做链类型判断，事件级过滤由 ExtractDexData 内部处理，避免重复解析
func (b *BluefinExtractor) SupportsBlock(block *types.UnifiedBlock) bool {
	return block.ChainType == types.ChainTypeSui
}

// extractSuiEventsFromTransaction 从交易中提取Sui事件
func (b *BluefinExtractor) extractSuiEventsFromTransaction(tx *types.UnifiedTransaction) []map[string]interface{} {
	if tx.RawData == nil {
		return nil
	}

	switch rawData := tx.RawData.(type) {
	case *models.SuiTransactionBlockResponse:
		if rawData == nil || len(rawData.Events) == 0 {
			return nil
		}
		events := make([]map[string]interface{}, 0, len(rawData.Events))
		for _, ev := range rawData.Events {
			event := map[string]interface{}{
				"type":              ev.Type,
				"sender":            ev.Sender,
				"parsedJson":        ev.ParsedJson,
				"packageId":         ev.PackageId,
				"transactionModule": ev.TransactionModule,
			}
			if ev.Id.EventSeq != "" {
				event["id"] = map[string]interface{}{
					"eventSeq": ev.Id.EventSeq,
					"txDigest": ev.Id.TxDigest,
				}
			}
			events = append(events, event)
		}
		return events
	case map[string]interface{}:
		if events, ok := rawData["events"]; ok {
			return b.parseEventsFromInterface(events)
		}
	}

	return nil
}

// parseEventsFromInterface 解析事件接口
func (b *BluefinExtractor) parseEventsFromInterface(events interface{}) []map[string]interface{} {
	return utils.ParseEventsFromInterface(events)
}

// isBluefinEvent 检查是否是Bluefin事件
func (b *BluefinExtractor) isBluefinEvent(event map[string]interface{}) bool {
	eventType, ok := event["type"].(string)
	if !ok {
		return false
	}

	// 检查是否包含Bluefin合约地址
	return eventType == bluefinPoolCreatedEventType ||
		eventType == bluefinAddLiquidityEventType ||
		eventType == bluefinRemoveLiquidityEventType ||
		eventType == bluefinAssetSwapEventType ||
		eventType == bluefinFlashSwapEventType
}

// getEventType 获取事件类型
func (b *BluefinExtractor) getEventType(event map[string]interface{}) string {
	eventType, ok := event["type"].(string)
	if !ok {
		return ""
	}

	switch eventType {
	case bluefinAssetSwapEventType, bluefinFlashSwapEventType:
		return "swap"
	case bluefinAddLiquidityEventType:
		return "add_liquidity"
	case bluefinRemoveLiquidityEventType:
		return "remove_liquidity"
	case bluefinPoolCreatedEventType:
		return "pool_created"
	default:
		return ""
	}
}

// createLiquidityFromEvent 从事件创建流动性记录 - 重点关注LiquidityRemoved事件的特定字段
func (b *BluefinExtractor) createLiquidityFromEvent(ctx context.Context, event map[string]interface{}, tx *types.UnifiedTransaction, side string) *LiquidityEventData {
	// 提取事件基本信息
	eventSeq := b.extractEventSeq(event)
	sender := b.extractSender(event)
	parsedFields := event["parsedJson"]
	if parsedFields == nil {
		bluefinLog.Warn("liquidity parsedJson is nil")
		return nil
	}

	fields, ok := parsedFields.(map[string]interface{})
	if !ok {
		bluefinLog.Warn("liquidity parsedJson is not a map")
		return nil
	}

	// 提取关键字段
	poolAddr := b.getStringField(fields, "pool_id")
	if poolAddr == "" {
		bluefinLog.Warn("liquidity pool_id is empty")
		return nil
	}

	// 使用公共方法获取池子数据
	poolEventData, err := b.getPoolEventData(ctx, poolAddr, tx)
	if err != nil {
		bluefinLog.Errorf("liquidity failed to get pool event data: %v", err)
		return nil
	}

	token0 := poolEventData.Pool.Tokens[0]
	token1 := poolEventData.Pool.Tokens[1]

	coinAAmount := b.getBigIntField(fields, "coin_a_amount")
	coinBAmount := b.getBigIntField(fields, "coin_b_amount")

	valueA := b.calcTokenUSDValue(token0, coinAAmount, poolEventData.Tokens)
	valueB := b.calcTokenUSDValue(token1, coinBAmount, poolEventData.Tokens)

	// 如果自身不是稳定币，尝试用对方的价格估算
	if valueA == 0 && valueB > 0 && coinBAmount != nil && coinBAmount.Sign() > 0 && coinAAmount != nil && coinAAmount.Sign() > 0 {
		valueA = valueB * rawToHuman(coinAAmount, b.getTokenDecimals(token0, poolEventData.Tokens)) /
			rawToHuman(coinBAmount, b.getTokenDecimals(token1, poolEventData.Tokens))
	} else if valueB == 0 && valueA > 0 && coinAAmount != nil && coinAAmount.Sign() > 0 && coinBAmount != nil && coinBAmount.Sign() > 0 {
		valueB = valueA * rawToHuman(coinBAmount, b.getTokenDecimals(token1, poolEventData.Tokens)) /
			rawToHuman(coinAAmount, b.getTokenDecimals(token0, poolEventData.Tokens))
	}

	baseKey := tx.TxHash + "_" + side + "_" + eventSeq

	liquidity0 := model.Liquidity{
		Addr:    token0,
		Factory: bluefinAmmAddr,
		Pool:    poolAddr,
		Hash:    tx.TxHash,
		From:    sender,
		Side:    side,
		Amount:  coinAAmount,
		Value:   valueA,
		Time:    uint64(tx.Timestamp.Unix()),
		Key:     baseKey + "_0",
	}
	liquidity1 := model.Liquidity{
		Addr:    token1,
		Factory: bluefinAmmAddr,
		Pool:    poolAddr,
		Hash:    tx.TxHash,
		From:    sender,
		Side:    side,
		Amount:  coinBAmount,
		Value:   valueB,
		Time:    uint64(tx.Timestamp.Unix()),
		Key:     baseKey + "_1",
	}

	return &LiquidityEventData{
		Pool:        poolEventData.Pool,
		Tokens:      poolEventData.Tokens,
		Reserve:     poolEventData.Reserve,
		Liquidities: []model.Liquidity{liquidity0, liquidity1},
	}
}

// createPoolFromEvent 从事件创建池子记录
func (b *BluefinExtractor) createPoolFromEvent(ctx context.Context, event map[string]interface{}, tx *types.UnifiedTransaction) *model.Pool {
	parsedFields := event["parsedJson"]
	if parsedFields == nil {
		return nil
	}

	fields, ok := parsedFields.(map[string]interface{})
	if !ok {
		return nil
	}

	poolAddr := b.getStringField(fields, "pool_id")
	if poolAddr == "" {
		return nil
	}
	// 获取池对象
	poolObject, err := b.getPoolObject(ctx, poolAddr)
	if err != nil {
		bluefinLog.Errorf("failed to get pool object for %s: %v", poolAddr, err)
		return nil
	}

	// 提取token地址
	token0, token1 := b.ExtractPoolCoin(poolObject.Data.Type)
	if token0 == "" || token1 == "" {
		bluefinLog.Errorf("failed to extract token addresses from pool type: %s", poolObject.Data.Type)
		return nil
	}

	var feeBps int
	if poolObject.Data.Content.Fields != nil {
		if feeRaw, ok := poolObject.Data.Content.Fields["fee_rate"]; ok {
			switch v := feeRaw.(type) {
			case string:
				if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
					feeBps = int(parsed / 100)
				}
			case float64:
				feeBps = int(v / 100)
			}
		}
	}

	return &model.Pool{
		Addr:     poolAddr,
		Factory:  bluefinAmmAddr,
		Protocol: "bluefin",
		Fee:      feeBps,
		Tokens: map[int]string{
			0: token0,
			1: token1,
		},
	}
}

// getStringField 安全获取字符串字段
func (b *BluefinExtractor) getStringField(fields map[string]interface{}, key string) string {
	return utils.GetStringField(fields, key)
}

// getBigIntField 安全获取大整数字段
func (b *BluefinExtractor) getBigIntField(fields map[string]interface{}, key string) *big.Int {
	return utils.GetBigIntField(fields, key)
}

// getBoolField 安全获取布尔字段
func (b *BluefinExtractor) getBoolField(fields map[string]interface{}, key string) bool {
	return utils.GetBoolField(fields, key)
}

// extractEventSeq 从事件中提取eventSeq
func (b *BluefinExtractor) extractEventSeq(event map[string]interface{}) string {
	if id, ok := event["id"].(map[string]interface{}); ok {
		if eventSeq, ok := id["eventSeq"].(string); ok {
			return eventSeq
		}
	}
	return ""
}

// extractSender 从事件中提取sender
func (b *BluefinExtractor) extractSender(event map[string]interface{}) string {
	if sender, ok := event["sender"].(string); ok {
		return sender
	}
	return ""
}

// parseEventSeq 将eventSeq字符串转换为整数
func (b *BluefinExtractor) parseEventSeq(eventSeq string) int64 {
	if eventSeq == "" {
		return 0
	}
	if val, err := strconv.ParseInt(eventSeq, 10, 64); err == nil {
		return val
	}
	return 0
}

// getBlockNumber 安全获取区块号
func (b *BluefinExtractor) getBlockNumber(tx *types.UnifiedTransaction) int64 {
	if tx.BlockNumber != nil {
		return tx.BlockNumber.Int64()
	}
	return 0
}

// swapPriceResult 存放 swap 的价格和价值计算结果
type swapPriceResult struct {
	SellPrice  float64 // 每 1 个 sell token 能换多少 buy token
	BuyPrice   float64 // 每 1 个 buy token 需要多少 sell token
	TradeValue float64 // 这笔交易的 USD 价值（如果涉及稳定币）
}

// calcSwapPriceValue 计算 swap 的价格和 USD 价值
//
//	sellAddr / buyAddr: 卖出/买入的 token 地址
//	amountIn / amountOut: 原始金额（含 decimals）
//	tokens: 池中两个 token 的元数据（用于获取 decimals）
func (b *BluefinExtractor) calcSwapPriceValue(
	sellAddr, buyAddr string,
	amountIn, amountOut *big.Int,
	tokens []model.Token,
) swapPriceResult {
	var result swapPriceResult

	var sellDecimals, buyDecimals int
	for _, t := range tokens {
		if t.Addr == sellAddr {
			sellDecimals = t.Decimals
		}
		if t.Addr == buyAddr {
			buyDecimals = t.Decimals
		}
	}

	humanIn := rawToHuman(amountIn, sellDecimals)
	humanOut := rawToHuman(amountOut, buyDecimals)

	if humanIn > 0 && humanOut > 0 {
		result.SellPrice = humanOut / humanIn
		result.BuyPrice = humanIn / humanOut
	}

	result.TradeValue = b.estimateUSDValue(sellAddr, buyAddr, humanIn, humanOut)

	return result
}

// calcTokenUSDValue 计算单个 token 金额的 USD 价值
// 使用 quote asset rank 确定计价基准
func (b *BluefinExtractor) calcTokenUSDValue(tokenAddr string, amount *big.Int, tokens []model.Token) float64 {
	if amount == nil || amount.Sign() == 0 {
		return 0
	}
	var decimals int
	for _, t := range tokens {
		if t.Addr == tokenAddr {
			decimals = t.Decimals
			break
		}
	}
	human := rawToHuman(amount, decimals)
	if b.isStableCoin(tokenAddr) {
		return human
	}
	return 0
}

// estimateUSDValue 根据 quoteAssets 排名估算交易的 USD 价值
// rank >= stableRankThreshold 的资产直接当 1:1 USD；
// rank < stableRankThreshold (如 SUI) 暂时无法直接估算，返回 0（需要外部价格源）
func (b *BluefinExtractor) estimateUSDValue(addrA, addrB string, humanA, humanB float64) float64 {
	b.cacheMutex.RLock()
	rankA := b.quoteAssets[addrA]
	rankB := b.quoteAssets[addrB]
	b.cacheMutex.RUnlock()

	aIsStable := rankA >= stableRankThreshold
	bIsStable := rankB >= stableRankThreshold

	switch {
	case aIsStable && bIsStable:
		if rankA >= rankB {
			return humanA
		}
		return humanB
	case aIsStable:
		return humanA
	case bIsStable:
		return humanB
	default:
		return 0
	}
}

// getTokenDecimals 从 token 列表中查找 decimals
func (b *BluefinExtractor) getTokenDecimals(addr string, tokens []model.Token) int {
	for _, t := range tokens {
		if t.Addr == addr {
			return t.Decimals
		}
	}
	return 0
}

// rawToHuman 将链上原始金额（含 decimals）转为人类可读浮点数
// 使用 big.Float 避免大数溢出
func rawToHuman(amount *big.Int, decimals int) float64 {
	if amount == nil || amount.Sign() == 0 || decimals < 0 {
		return 0
	}
	exp := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	result, _ := new(big.Float).Quo(
		new(big.Float).SetInt(amount),
		new(big.Float).SetInt(exp),
	).Float64()
	return result
}

// getPoolObject 获取池对象（带缓存，TTL 60秒）
func (b *BluefinExtractor) getPoolObject(ctx context.Context, poolId string) (models.SuiObjectResponse, error) {
	b.cacheMutex.RLock()
	if item, exists := b.poolCache[poolId]; exists && time.Now().Before(item.ExpiresAt) {
		b.cacheMutex.RUnlock()
		return item.Object, nil
	}
	b.cacheMutex.RUnlock()

	obj, err := b.client.GetObject(ctx, models.SuiGetObjectRequest{
		ObjectId: poolId,
		Options: models.SuiObjectDataOptions{
			ShowType:                true,
			ShowContent:             true,
			ShowBcs:                 false,
			ShowOwner:               false,
			ShowPreviousTransaction: false,
			ShowStorageRebate:       false,
			ShowDisplay:             false,
		},
	})
	if err != nil {
		return obj, err
	}

	b.cacheMutex.Lock()
	b.poolCache[poolId] = &PoolCacheItem{
		Object:    obj,
		ExpiresAt: time.Now().Add(poolCacheTTL),
	}
	b.cacheMutex.Unlock()

	return obj, nil
}

// getPoolEventData 获取池子事件的通用数据（消除重复代码）
func (b *BluefinExtractor) getPoolEventData(ctx context.Context, poolId string, tx *types.UnifiedTransaction) (*PoolEventData, error) {
	if poolId == "" {
		return nil, fmt.Errorf("pool_id is empty")
	}

	// 获取池对象
	poolObject, err := b.getPoolObject(ctx, poolId)
	if err != nil {
		return nil, fmt.Errorf("failed to get pool object for %s: %w", poolId, err)
	}

	// 提取token地址
	token0, token1 := b.ExtractPoolCoin(poolObject.Data.Type)
	if token0 == "" || token1 == "" {
		return nil, fmt.Errorf("failed to extract token addresses from pool type: %s", poolObject.Data.Type)
	}

	token0Reserve, token1Reserve, err := b.getPoolTokenBalances(poolObject)
	if err != nil {
		bluefinLog.Warnf("failed to get pool token balances for %s: %v", poolId, err)
		token0Reserve = big.NewInt(0)
		token1Reserve = big.NewInt(0)
	}

	// 获取token元数据（使用缓存）
	token0Metadata, token1Metadata, err := b.getTokensMetadataWithCache(ctx, token0, token1)
	if err != nil {
		return nil, fmt.Errorf("failed to get tokens metadata: %w", err)
	}

	var feeBps int
	if poolObject.Data.Content.Fields != nil {
		if feeRaw, ok := poolObject.Data.Content.Fields["fee_rate"]; ok {
			switch v := feeRaw.(type) {
			case string:
				if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
					feeBps = int(parsed / 100) // fee_rate 用 1e6 表示, 3000 → 30 bps (0.3%)
				}
			case float64:
				feeBps = int(v / 100)
			}
		}
	}

	return &PoolEventData{
		Pool: model.Pool{
			Addr:     poolId,
			Factory:  bluefinAmmAddr,
			Protocol: "bluefin",
			Fee:      feeBps,
			Tokens: map[int]string{
				0: token0,
				1: token1,
			},
		},
		Tokens: []model.Token{token0Metadata, token1Metadata},
		Reserve: model.Reserve{
			Addr: poolId,
			Amounts: map[int]*big.Int{
				0: token0Reserve,
				1: token1Reserve,
			},
			Time: uint64(tx.Timestamp.Unix()),
		},
	}, nil
}

// getTokensMetadataWithCache 获取token元数据（带缓存）
func (b *BluefinExtractor) getTokensMetadataWithCache(ctx context.Context, token0, token1 string) (model.Token, model.Token, error) {
	// 获取token0元数据
	token0Metadata, err := b.getTokenMetadataWithCache(ctx, token0)
	if err != nil {
		return model.Token{}, model.Token{}, fmt.Errorf("failed to get token0 metadata: %w", err)
	}

	// 获取token1元数据
	token1Metadata, err := b.getTokenMetadataWithCache(ctx, token1)
	if err != nil {
		return model.Token{}, model.Token{}, fmt.Errorf("failed to get token1 metadata: %w", err)
	}

	return token0Metadata, token1Metadata, nil
}

// getTokenMetadataWithCache 获取单个token元数据（带缓存）
func (b *BluefinExtractor) getTokenMetadataWithCache(ctx context.Context, tokenAddr string) (model.Token, error) {
	b.cacheMutex.RLock()
	if item, exists := b.tokenCache[tokenAddr]; exists {
		if time.Now().Before(item.ExpiresAt) {
			b.cacheMutex.RUnlock()
			return item.Token, nil
		}
	}
	b.cacheMutex.RUnlock()

	// 缓存未命中或已过期，从链上获取
	tokenMetadata, err := b.client.GetToken(ctx, models.SuiXGetCoinMetadataRequest{
		CoinType: tokenAddr,
	})
	if err != nil {
		return model.Token{}, fmt.Errorf("failed to get token metadata: %w", err)
	}

	token := model.Token{
		Addr:     tokenAddr,
		Symbol:   tokenMetadata.Symbol,
		Decimals: tokenMetadata.Decimals,
		Name:     tokenMetadata.Name,
		IsStable: b.isStableCoin(tokenAddr),
	}

	b.cacheMutex.Lock()
	b.tokenCache[tokenAddr] = &TokenCacheItem{
		Token:     token,
		ExpiresAt: time.Now().Add(tokenCacheTTL),
	}
	b.cacheMutex.Unlock()

	return token, nil
}
