package suidex

import (
	"context"
	"encoding/json"
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

var cetusLog = logrus.WithFields(logrus.Fields{"service": "parser", "module": "dex-cetus"})

// Cetus协议常量 - 基于官方文档 https://cetus-1.gitbook.io/cetus-developer-docs/
const (
	// Cetus CLMM合约地址 - Mainnet (官方文档确认)
	// package-id: 0x1eabed72c53feb3805120a081dc15963c204dc8d091542592abaf7a35689b2fb
	// published-at: 0x25ebb9a7c50eb17b3fa9c5a30fb8b5ad8f97caaf4928943acbcff7153dfee5e3
	cetusClmmPoolAddr = "0x1eabed72c53feb3805120a081dc15963c204dc8d091542592abaf7a35689b2fb"

	// Cetus Integrate合约地址 - Mainnet
	// package-id: 0x996c4d9480708fb8b92aa7acf819fb0497b5ec8e65ba06601cae2fb6db3312c3
	cetusIntegrateAddr = "0x996c4d9480708fb8b92aa7acf819fb0497b5ec8e65ba06601cae2fb6db3312c3"

	// Cetus Config合约地址 - Mainnet
	// package-id: 0x95b8d278b876cae22206131fb9724f701c9444515813042f54f0a426c9a3bc2f
	cetusConfigAddr = "0x95b8d278b876cae22206131fb9724f701c9444515813042f54f0a426c9a3bc2f"

	// Cetus DLMM合约地址 - Mainnet
	// package-id: 0x5664f9d3fd82c84023870cfbda8ea84e14c8dd56ce557ad2116e0668581a682b
	cetusDlmmAddr = "0x5664f9d3fd82c84023870cfbda8ea84e14c8dd56ce557ad2116e0668581a682b"

	// CETUS Token地址 - Mainnet (官方文档确认)
	cetusTokenAddr = "0x6864a6f921804860930db6ddbe2e16acdf8504495ea7481637a1c8b9a8fe54b::cetus::CETUS"

	// xCETUS Token地址 - Mainnet (官方文档确认)
	xCetusTokenAddr = "0x9e69acc50ca03bc943c4f7c5304c2a6002d507b51c11913b247159c60422c606::xcetus::XCETUS"

	// Cetus 其他模块地址（从链上实际发现）
	// 这个地址用于某些特定版本的流动性事件
	cetusLiquidityModuleAddr = "0xdb5cd62a06c79695bfc9982eb08534706d3752fe123b48e0144f480209b3117f"

	// Cetus CLMM事件类型 (基于官方合约结构和链上实际事件)
	cetusPoolCreatedEventType     = cetusClmmPoolAddr + "::factory::CreatePoolEvent"
	cetusAddLiquidityEventType    = cetusClmmPoolAddr + "::pool::AddLiquidityEvent"
	cetusRemoveLiquidityEventType = cetusClmmPoolAddr + "::pool::RemoveLiquidityEvent"
	cetusSwapEventType            = cetusClmmPoolAddr + "::pool::SwapEvent"
	cetusCollectFeeEventType      = cetusClmmPoolAddr + "::pool::CollectFeeEvent"
	cetusCollectRewardEventType   = cetusClmmPoolAddr + "::pool::CollectRewardEvent"

	// Cetus V2 流动性事件类型（从链上发现）
	cetusAddLiquidityV2EventType    = cetusLiquidityModuleAddr + "::pool::AddLiquidityV2Event"
	cetusRemoveLiquidityV2EventType = cetusLiquidityModuleAddr + "::pool::RemoveLiquidityV2Event"

	// Cetus DLMM事件类型
	cetusDlmmSwapEventType            = cetusDlmmAddr + "::pool::SwapEvent"
	cetusDlmmAddLiquidityEventType    = cetusDlmmAddr + "::pool::AddLiquidityEvent"
	cetusDlmmRemoveLiquidityEventType = cetusDlmmAddr + "::pool::RemoveLiquidityEvent"
)

// CetusConfig Cetus 配置信息
// 基于官方文档: https://cetus-1.gitbook.io/cetus-developer-docs/
// 和链上实际观察到的合约地址
type CetusConfig struct {
	ClmmPoolAddr         string // CLMM 池子合约地址
	IntegrateAddr        string // Integrate 合约地址
	ConfigAddr           string // Config 合约地址
	DlmmAddr             string // DLMM 合约地址
	LiquidityModuleAddr  string // 流动性模块地址（用于 V2 事件）
	CetusTokenAddr       string // CETUS Token 地址
}

// DefaultCetusConfig 返回主网默认配置
// 包含官方文档地址和链上实际发现的地址
func DefaultCetusConfig() *CetusConfig {
	return &CetusConfig{
		ClmmPoolAddr:        cetusClmmPoolAddr,
		IntegrateAddr:       cetusIntegrateAddr,
		ConfigAddr:          cetusConfigAddr,
		DlmmAddr:            cetusDlmmAddr,
		LiquidityModuleAddr: cetusLiquidityModuleAddr,
		CetusTokenAddr:      cetusTokenAddr,
	}
}

// CetusExtractor Cetus DEX数据提取器
// 支持 CLMM (Concentrated Liquidity Market Maker) 协议
type CetusExtractor struct {
	client      *sui.SuiProcessor
	tokenCache  map[string]*TokenCacheItem
	cacheMutex  sync.RWMutex
	config      *CetusConfig
	quoteAssets map[string]int // addr → rank, rank>=90 视为 USD 稳定币
}

// NewCetusExtractor 创建Cetus提取器（使用主网默认配置）
func NewCetusExtractor() *CetusExtractor {
	return &CetusExtractor{
		tokenCache:  make(map[string]*TokenCacheItem),
		config:      DefaultCetusConfig(),
		quoteAssets: defaultSuiQuoteAssets,
	}
}

// NewCetusExtractorWithConfig 创建带自定义配置的Cetus提取器
func NewCetusExtractorWithConfig(config *CetusConfig) *CetusExtractor {
	return &CetusExtractor{
		tokenCache:  make(map[string]*TokenCacheItem),
		config:      config,
		quoteAssets: defaultSuiQuoteAssets,
	}
}

// SetSuiProcessor 设置Sui处理器（用于获取链上数据）
func (c *CetusExtractor) SetSuiProcessor(processor interface{}) {
	if suiProcessor, ok := processor.(*sui.SuiProcessor); ok {
		c.client = suiProcessor
	}
}

// SetQuoteAssets 设置报价资产（从配置加载），addr → rank
func (c *CetusExtractor) SetQuoteAssets(assets map[string]int) {
	if len(assets) > 0 {
		c.cacheMutex.Lock()
		defer c.cacheMutex.Unlock()
		c.quoteAssets = assets
	}
}

// isStableCoin 判断地址是否为 USD 稳定币 (rank >= stableRankThreshold)
func (c *CetusExtractor) isStableCoin(addr string) bool {
	c.cacheMutex.RLock()
	defer c.cacheMutex.RUnlock()
	rank, ok := c.quoteAssets[addr]
	return ok && rank >= stableRankThreshold
}

// GetSupportedProtocols 获取支持的协议
func (c *CetusExtractor) GetSupportedProtocols() []string {
	return []string{"cetus"}
}

// GetSupportedChains 获取支持的链类型
func (c *CetusExtractor) GetSupportedChains() []types.ChainType {
	return []types.ChainType{types.ChainTypeSui}
}

// ExtractDexData 从统一区块数据中提取Cetus DEX相关数据
func (c *CetusExtractor) ExtractDexData(ctx context.Context, blocks []types.UnifiedBlock) (*types.DexData, error) {
	// 检查是否设置了Sui处理器
	if c.client == nil {
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
			suiEvents := c.extractSuiEventsFromTransaction(&tx)
			if len(suiEvents) == 0 {
				continue
			}

			// 处理每个事件
			for _, event := range suiEvents {
				if !c.isCetusEvent(event) {
					continue
				}

				// 根据事件类型处理
				eventType := c.getEventType(event)
				switch eventType {
				case "swap":
					swapData := c.processSwapEvent(ctx, event, &tx)
					if swapData != nil {
						dexData.Pools = append(dexData.Pools, swapData.Pool)
						dexData.Tokens = append(dexData.Tokens, swapData.Tokens...)
						dexData.Reserves = append(dexData.Reserves, swapData.Reserve)
						dexData.Transactions = append(dexData.Transactions, swapData.Transactions...)
					}

				case "add_liquidity":
					if liquidityData := c.createLiquidityFromEvent(ctx, event, &tx, "add"); liquidityData != nil {
						dexData.Pools = append(dexData.Pools, liquidityData.Pool)
						dexData.Tokens = append(dexData.Tokens, liquidityData.Tokens...)
						dexData.Reserves = append(dexData.Reserves, liquidityData.Reserve)
						dexData.Liquidities = append(dexData.Liquidities, liquidityData.Liquidities...)
					}

				case "remove_liquidity":
					if liquidityData := c.createLiquidityFromEvent(ctx, event, &tx, "remove"); liquidityData != nil {
						dexData.Pools = append(dexData.Pools, liquidityData.Pool)
						dexData.Tokens = append(dexData.Tokens, liquidityData.Tokens...)
						dexData.Reserves = append(dexData.Reserves, liquidityData.Reserve)
						dexData.Liquidities = append(dexData.Liquidities, liquidityData.Liquidities...)
					}

				case "pool_created":
					if pool := c.createPoolFromEvent(ctx, event, &tx); pool != nil {
						dexData.Pools = append(dexData.Pools, *pool)
					}
				}
			}
		}
	}

	return dexData, nil
}

// processSwapEvent 处理交换事件 - 按照Cetus SwapEvent结构
func (c *CetusExtractor) processSwapEvent(ctx context.Context, event map[string]interface{}, tx *types.UnifiedTransaction) *SwapEventData {
	// 提取事件基本信息
	eventSeq := c.extractEventSeq(event)
	sender := c.extractSender(event)

	// 提取parsedJson中的关键字段
	parsedJson := event["parsedJson"]
	if parsedJson == nil {
		cetusLog.Warn("swap event parsedJson is nil")
		return nil
	}

	fields, ok := parsedJson.(map[string]interface{})
	if !ok {
		cetusLog.Warn("swap event parsedJson is not a map")
		return nil
	}

	// 提取Cetus Swap事件的关键字段
	// Cetus的SwapEvent结构: pool, amount_in, amount_out, ref_amount, fee_amount, vault_a_amount, vault_b_amount, before_sqrt_price, after_sqrt_price, steps, partner
	poolId := c.getStringField(fields, "pool")
	amountIn := c.getBigIntField(fields, "amount_in")
	amountOut := c.getBigIntField(fields, "amount_out")
	atob := c.getBoolField(fields, "atob") // Cetus特有：swap方向标识

	// 使用公共方法获取池子数据
	poolEventData, err := c.getPoolEventData(ctx, poolId, tx)
	if err != nil {
		cetusLog.Errorf("swap event failed to get pool event data: %v", err)
		return nil
	}

	// 获取token地址
	token0 := poolEventData.Pool.Tokens[0]
	token1 := poolEventData.Pool.Tokens[1]

	// 根据atob确定买卖方向
	var sellToken, buyToken string
	if atob {
		// a to b: token0 -> token1
		sellToken = token0
		buyToken = token1
	} else {
		// b to a: token1 -> token0
		sellToken = token1
		buyToken = token0
	}

	pv := c.calcSwapPriceValue(sellToken, buyToken, amountIn, amountOut, poolEventData.Tokens)

	return &SwapEventData{
		Pool:    poolEventData.Pool,
		Tokens:  poolEventData.Tokens,
		Reserve: poolEventData.Reserve,
		Transactions: []model.Transaction{
			{
				Addr:        sellToken,
				Factory:     cetusClmmPoolAddr,
				Pool:        poolId,
				Hash:        tx.TxHash,
				EventIndex:  c.parseEventSeq(eventSeq),
				TxIndex:     int64(tx.TxIndex),
				BlockNumber: c.getBlockNumber(tx),
				Time:        uint64(tx.Timestamp.Unix()),
				From:        sender,
				Side:        "sell",
				Amount:      amountIn,
				Price:       pv.SellPrice,
				Value:       pv.TradeValue,
				Extra: &model.TransactionExtra{
					QuoteAddr:     buyToken,
					Type:          "swap",
					TokenDecimals: 9,
				},
			},
			{
				Addr:        buyToken,
				Factory:     cetusClmmPoolAddr,
				Pool:        poolId,
				Hash:        tx.TxHash,
				EventIndex:  c.parseEventSeq(eventSeq),
				TxIndex:     int64(tx.TxIndex),
				BlockNumber: c.getBlockNumber(tx),
				Time:        uint64(tx.Timestamp.Unix()),
				From:        sender,
				Side:        "buy",
				Amount:      amountOut,
				Price:       pv.BuyPrice,
				Value:       pv.TradeValue,
				Extra: &model.TransactionExtra{
					QuoteAddr:     sellToken,
					Type:          "swap",
					TokenDecimals: 9,
				},
			},
		},
	}
}

// createLiquidityFromEvent 从事件创建流动性记录 - 按照Cetus AddLiquidityEvent/RemoveLiquidityEvent结构
func (c *CetusExtractor) createLiquidityFromEvent(ctx context.Context, event map[string]interface{}, tx *types.UnifiedTransaction, side string) *LiquidityEventData {
	// 提取事件基本信息
	eventSeq := c.extractEventSeq(event)
	sender := c.extractSender(event)

	parsedFields := event["parsedJson"]
	if parsedFields == nil {
		cetusLog.Warnf("liquidity-%s parsedJson is nil", side)
		return nil
	}

	fields, ok := parsedFields.(map[string]interface{})
	if !ok {
		cetusLog.Warnf("liquidity-%s parsedJson is not a map", side)
		return nil
	}

	// 提取关键字段
	// Cetus CLMM的AddLiquidityEvent结构: pool, tick_lower, tick_upper, liquidity, after_liquidity, amount_a, amount_b
	// Cetus CLMM的RemoveLiquidityEvent结构: pool, tick_lower, tick_upper, liquidity, after_liquidity, amount_a, amount_b
	poolAddr := c.getStringField(fields, "pool")
	if poolAddr == "" {
		cetusLog.Warnf("liquidity-%s pool is empty", side)
		return nil
	}

	// 使用公共方法获取池子数据
	poolEventData, err := c.getPoolEventData(ctx, poolAddr, tx)
	if err != nil {
		cetusLog.Errorf("liquidity-%s failed to get pool event data: %v", side, err)
		return nil
	}

	token0 := poolEventData.Pool.Tokens[0]
	token1 := poolEventData.Pool.Tokens[1]

	amountA := c.getBigIntField(fields, "amount_a")
	amountB := c.getBigIntField(fields, "amount_b")

	valueA := c.calcTokenUSDValue(token0, amountA, poolEventData.Tokens)
	valueB := c.calcTokenUSDValue(token1, amountB, poolEventData.Tokens)

	if valueA == 0 && valueB > 0 && amountB != nil && amountB.Sign() > 0 && amountA != nil && amountA.Sign() > 0 {
		valueA = valueB * rawToHuman(amountA, c.getTokenDecimals(token0, poolEventData.Tokens)) /
			rawToHuman(amountB, c.getTokenDecimals(token1, poolEventData.Tokens))
	} else if valueB == 0 && valueA > 0 && amountA != nil && amountA.Sign() > 0 && amountB != nil && amountB.Sign() > 0 {
		valueB = valueA * rawToHuman(amountB, c.getTokenDecimals(token1, poolEventData.Tokens)) /
			rawToHuman(amountA, c.getTokenDecimals(token0, poolEventData.Tokens))
	}

	baseKey := tx.TxHash + "_" + side + "_" + eventSeq

	liquidity0 := model.Liquidity{
		Addr:    token0,
		Factory: cetusClmmPoolAddr,
		Pool:    poolAddr,
		Hash:    tx.TxHash,
		From:    sender,
		Side:    side,
		Amount:  amountA,
		Value:   valueA,
		Time:    uint64(tx.Timestamp.Unix()),
		Key:     baseKey + "_0",
	}

	liquidity1 := model.Liquidity{
		Addr:    token1,
		Factory: cetusClmmPoolAddr,
		Pool:    poolAddr,
		Hash:    tx.TxHash,
		From:    sender,
		Side:    side,
		Amount:  amountB,
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
func (c *CetusExtractor) createPoolFromEvent(ctx context.Context, event map[string]interface{}, tx *types.UnifiedTransaction) *model.Pool {
	parsedFields := event["parsedJson"]
	if parsedFields == nil {
		return nil
	}

	fields, ok := parsedFields.(map[string]interface{})
	if !ok {
		return nil
	}

	// Cetus的CreatePoolEvent结构: pool_id, coin_type_a, coin_type_b, tick_spacing, fee_rate, initialize_sqrt_price
	poolAddr := c.getStringField(fields, "pool_id")
	if poolAddr == "" {
		return nil
	}

	// 获取池对象
	poolObject, err := c.getPoolObject(ctx, poolAddr)
	if err != nil {
		cetusLog.Errorf("create pool event failed to get pool object for %s: %v", poolAddr, err)
		return nil
	}

	// 提取token地址
	token0, token1 := c.ExtractPoolCoin(poolObject.Data.Type)
	if token0 == "" || token1 == "" {
		cetusLog.Errorf("create pool event failed to extract token addresses from pool type: %s", poolObject.Data.Type)
		return nil
	}

	// 获取费率
	feeRate := c.getIntField(fields, "fee_rate")

	return &model.Pool{
		Addr:     poolAddr,
		Factory:  cetusClmmPoolAddr,
		Protocol: "cetus",
		Tokens: map[int]string{
			0: token0,
			1: token1,
		},
		Fee: feeRate,
	}
}

// SupportsBlock 检查是否支持该区块
func (c *CetusExtractor) SupportsBlock(block *types.UnifiedBlock) bool {
	// 只支持Sui链
	if block.ChainType != types.ChainTypeSui {
		return false
	}

	// 检查区块中是否有任何交易包含Cetus事件
	for _, tx := range block.Transactions {
		suiEvents := c.extractSuiEventsFromTransaction(&tx)
		for _, event := range suiEvents {
			if c.isCetusEvent(event) {
				return true
			}
		}
	}
	return false
}

// extractSuiEventsFromTransaction 从交易中提取Sui事件
func (c *CetusExtractor) extractSuiEventsFromTransaction(tx *types.UnifiedTransaction) []map[string]interface{} {
	// 根据不同的原始数据类型处理
	switch rawData := tx.RawData.(type) {
	case map[string]interface{}:
		// 如果是map格式，尝试获取events字段
		if events, ok := rawData["events"]; ok {
			return c.parseEventsFromInterface(events)
		}
	default:
		// 尝试通过JSON解析
		data, err := json.Marshal(rawData)
		if err != nil {
			return nil
		}

		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			return nil
		}

		if events, ok := result["events"]; ok {
			return c.parseEventsFromInterface(events)
		}
	}

	return nil
}

// parseEventsFromInterface 解析事件接口
func (c *CetusExtractor) parseEventsFromInterface(events interface{}) []map[string]interface{} {
	return utils.ParseEventsFromInterface(events)
}

// isCetusEvent 检查是否是Cetus事件
// 支持 CLMM、Integrate、DLMM、Liquidity Module 等多个合约的事件识别
func (c *CetusExtractor) isCetusEvent(event map[string]interface{}) bool {
	eventType, ok := event["type"].(string)
	if !ok {
		return false
	}

	// 检查是否包含 Cetus 合约地址（支持多个合约和版本）
	// 1. CLMM 核心事件
	if eventType == cetusPoolCreatedEventType ||
		eventType == cetusAddLiquidityEventType ||
		eventType == cetusRemoveLiquidityEventType ||
		eventType == cetusSwapEventType ||
		eventType == cetusCollectFeeEventType ||
		eventType == cetusCollectRewardEventType {
		return true
	}

	// 2. V2 流动性事件（链上实际发现）
	if eventType == cetusAddLiquidityV2EventType ||
		eventType == cetusRemoveLiquidityV2EventType {
		return true
	}

	// 3. DLMM 事件
	if eventType == cetusDlmmSwapEventType ||
		eventType == cetusDlmmAddLiquidityEventType ||
		eventType == cetusDlmmRemoveLiquidityEventType {
		return true
	}

	// 4. 灵活匹配：检查是否包含任何已知的 Cetus 合约地址
	// 这样可以捕获新的或未明确定义的事件类型
	cetusAddresses := []string{
		cetusClmmPoolAddr,
		cetusIntegrateAddr,
		cetusDlmmAddr,
		cetusLiquidityModuleAddr,
		cetusConfigAddr,
	}

	for _, addr := range cetusAddresses {
		if strings.Contains(eventType, addr) {
			return true
		}
	}

	return false
}

// getEventType 获取事件类型
// 支持 CLMM、V2、DLMM 等多种事件类型
func (c *CetusExtractor) getEventType(event map[string]interface{}) string {
	eventType, ok := event["type"].(string)
	if !ok {
		return ""
	}

	// 精确匹配已知事件类型
	switch eventType {
	// CLMM 核心事件
	case cetusSwapEventType:
		return "swap"
	case cetusAddLiquidityEventType:
		return "add_liquidity"
	case cetusRemoveLiquidityEventType:
		return "remove_liquidity"
	case cetusPoolCreatedEventType:
		return "pool_created"
	case cetusCollectFeeEventType:
		return "collect_fee"
	case cetusCollectRewardEventType:
		return "collect_reward"

	// V2 流动性事件（链上实际发现）
	case cetusAddLiquidityV2EventType:
		return "add_liquidity"
	case cetusRemoveLiquidityV2EventType:
		return "remove_liquidity"

	// DLMM 事件
	case cetusDlmmSwapEventType:
		return "swap"
	case cetusDlmmAddLiquidityEventType:
		return "add_liquidity"
	case cetusDlmmRemoveLiquidityEventType:
		return "remove_liquidity"
	}

	// 模糊匹配：根据事件名称后缀判断类型
	// 这样可以支持未来的新事件类型
	if strings.HasSuffix(eventType, "::SwapEvent") {
		return "swap"
	}
	if strings.Contains(eventType, "AddLiquidity") {
		return "add_liquidity"
	}
	if strings.Contains(eventType, "RemoveLiquidity") {
		return "remove_liquidity"
	}
	if strings.Contains(eventType, "CreatePool") || strings.Contains(eventType, "PoolCreated") {
		return "pool_created"
	}
	if strings.Contains(eventType, "CollectFee") {
		return "collect_fee"
	}
	if strings.Contains(eventType, "CollectReward") {
		return "collect_reward"
	}

	return ""
}

// ExtractPoolCoin 获取pool池里面代币地址
func (c *CetusExtractor) ExtractPoolCoin(coinType string) (string, string) {
	token0, token1 := utils.ExtractPoolTokens(coinType)

	// 特殊处理SUI代币
	if strings.EqualFold(token0, shortCoinType) {
		token0 = shortCoinType
	}
	if strings.EqualFold(token1, shortCoinType) {
		token1 = shortCoinType
	}

	// 标准化 CETUS 代币地址（官方文档定义）
	if c.isCetusToken(token0) {
		token0 = cetusTokenAddr
	}
	if c.isCetusToken(token1) {
		token1 = cetusTokenAddr
	}

	return token0, token1
}

// isCetusToken 检查是否是 CETUS 代币
func (c *CetusExtractor) isCetusToken(tokenAddr string) bool {
	// 支持多种 CETUS 代币格式
	return strings.Contains(strings.ToLower(tokenAddr), "::cetus::cetus") ||
		strings.EqualFold(tokenAddr, cetusTokenAddr)
}

// getPoolObject 获取池对象
func (c *CetusExtractor) getPoolObject(ctx context.Context, poolId string) (models.SuiObjectResponse, error) {
	return c.client.GetObject(ctx, models.SuiGetObjectRequest{
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
}

// getPoolTokenBalances 获取池子代币余额
func (c *CetusExtractor) getPoolTokenBalances(token0, token1 string, poolObject models.SuiObjectResponse) (*big.Int, *big.Int, error) {
	// Cetus CLMM池子的余额字段可能与AMM不同，需要根据实际结构调整
	// 通常CLMM池子会有coin_a和coin_b字段
	coinABalance := c.getStringField(poolObject.Data.Content.Fields, "coin_a")
	coinBBalance := c.getStringField(poolObject.Data.Content.Fields, "coin_b")

	if coinABalance == "" || coinBBalance == "" {
		return &big.Int{}, &big.Int{}, fmt.Errorf("无法获取池子代币余额")
	}

	balanceCoinAValue, ok := new(big.Int).SetString(coinABalance, 10)
	if !ok {
		return &big.Int{}, &big.Int{}, fmt.Errorf("无法解析池子代币余额")
	}

	balanceCoinBValue, ok := new(big.Int).SetString(coinBBalance, 10)
	if !ok {
		return &big.Int{}, &big.Int{}, fmt.Errorf("无法解析池子代币余额")
	}

	return balanceCoinAValue, balanceCoinBValue, nil
}

// getPoolEventData 获取池子事件的通用数据（消除重复代码）
func (c *CetusExtractor) getPoolEventData(ctx context.Context, poolId string, tx *types.UnifiedTransaction) (*PoolEventData, error) {
	if poolId == "" {
		return nil, fmt.Errorf("pool_id is empty")
	}

	// 获取池对象
	poolObject, err := c.getPoolObject(ctx, poolId)
	if err != nil {
		return nil, fmt.Errorf("failed to get pool object for %s: %w", poolId, err)
	}

	// 提取token地址
	token0, token1 := c.ExtractPoolCoin(poolObject.Data.Type)
	if token0 == "" || token1 == "" {
		return nil, fmt.Errorf("failed to extract token addresses from pool type: %s", poolObject.Data.Type)
	}

	// 获取token储备量
	token0Reserve, token1Reserve, _ := c.getPoolTokenBalances(token0, token1, poolObject)

	// 获取token元数据（使用缓存）
	token0Metadata, token1Metadata, err := c.getTokensMetadataWithCache(ctx, token0, token1)
	if err != nil {
		return nil, fmt.Errorf("failed to get tokens metadata: %w", err)
	}

	return &PoolEventData{
		Pool: model.Pool{
			Addr:     poolId,
			Factory:  cetusClmmPoolAddr,
			Protocol: "cetus",
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
func (c *CetusExtractor) getTokensMetadataWithCache(ctx context.Context, token0, token1 string) (model.Token, model.Token, error) {
	// 获取token0元数据
	token0Metadata, err := c.getTokenMetadataWithCache(ctx, token0)
	if err != nil {
		return model.Token{}, model.Token{}, fmt.Errorf("failed to get token0 metadata: %w", err)
	}

	// 获取token1元数据
	token1Metadata, err := c.getTokenMetadataWithCache(ctx, token1)
	if err != nil {
		return model.Token{}, model.Token{}, fmt.Errorf("failed to get token1 metadata: %w", err)
	}

	return token0Metadata, token1Metadata, nil
}

// getTokenMetadataWithCache 获取单个token元数据（带缓存）
func (c *CetusExtractor) getTokenMetadataWithCache(ctx context.Context, tokenAddr string) (model.Token, error) {
	// 先检查缓存
	c.cacheMutex.RLock()
	if item, exists := c.tokenCache[tokenAddr]; exists {
		// 检查是否过期（缓存1小时）
		if time.Now().Before(item.ExpiresAt) {
			c.cacheMutex.RUnlock()
			return item.Token, nil
		}
	}
	c.cacheMutex.RUnlock()

	// 缓存未命中或已过期，从链上获取
	tokenMetadata, err := c.client.GetToken(ctx, models.SuiXGetCoinMetadataRequest{
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
		IsStable: c.isStableCoin(tokenAddr),
	}

	c.cacheMutex.Lock()
	c.tokenCache[tokenAddr] = &TokenCacheItem{
		Token:     token,
		ExpiresAt: time.Now().Add(tokenCacheTTL),
	}
	c.cacheMutex.Unlock()

	return token, nil
}

// calcSwapPriceValue 计算 swap 的价格和 USD 价值
func (c *CetusExtractor) calcSwapPriceValue(
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

	result.TradeValue = c.estimateUSDValue(sellAddr, buyAddr, humanIn, humanOut)

	return result
}

// calcTokenUSDValue 计算单个 token 金额的 USD 价值
func (c *CetusExtractor) calcTokenUSDValue(tokenAddr string, amount *big.Int, tokens []model.Token) float64 {
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
	if c.isStableCoin(tokenAddr) {
		return human
	}
	return 0
}

// estimateUSDValue 根据 quoteAssets 排名估算交易的 USD 价值
func (c *CetusExtractor) estimateUSDValue(addrA, addrB string, humanA, humanB float64) float64 {
	c.cacheMutex.RLock()
	rankA := c.quoteAssets[addrA]
	rankB := c.quoteAssets[addrB]
	c.cacheMutex.RUnlock()

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
func (c *CetusExtractor) getTokenDecimals(addr string, tokens []model.Token) int {
	for _, t := range tokens {
		if t.Addr == addr {
			return t.Decimals
		}
	}
	return 0
}

// 辅助方法：安全获取字段值

// getStringField 安全获取字符串字段
func (c *CetusExtractor) getStringField(fields map[string]interface{}, key string) string {
	return utils.GetStringField(fields, key)
}

// getBigIntField 安全获取大整数字段
func (c *CetusExtractor) getBigIntField(fields map[string]interface{}, key string) *big.Int {
	return utils.GetBigIntField(fields, key)
}

// getIntField 安全获取整数字段
func (c *CetusExtractor) getIntField(fields map[string]interface{}, key string) int {
	if val, ok := fields[key]; ok {
		switch v := val.(type) {
		case int:
			return v
		case int64:
			return int(v)
		case float64:
			return int(v)
		case string:
			if intVal, err := strconv.Atoi(v); err == nil {
				return intVal
			}
		}
	}
	return 0
}

// getBoolField 安全获取布尔字段
func (c *CetusExtractor) getBoolField(fields map[string]interface{}, key string) bool {
	if val, ok := fields[key]; ok {
		if boolVal, ok := val.(bool); ok {
			return boolVal
		}
	}
	return false
}

// extractEventSeq 从事件中提取eventSeq
func (c *CetusExtractor) extractEventSeq(event map[string]interface{}) string {
	if id, ok := event["id"].(map[string]interface{}); ok {
		if eventSeq, ok := id["eventSeq"].(string); ok {
			return eventSeq
		}
	}
	return ""
}

// extractSender 从事件中提取sender
func (c *CetusExtractor) extractSender(event map[string]interface{}) string {
	if sender, ok := event["sender"].(string); ok {
		return sender
	}
	return ""
}

// parseEventSeq 将eventSeq字符串转换为整数
func (c *CetusExtractor) parseEventSeq(eventSeq string) int64 {
	if eventSeq == "" {
		return 0
	}
	if val, err := strconv.ParseInt(eventSeq, 10, 64); err == nil {
		return val
	}
	return 0
}

// getBlockNumber 安全获取区块号
func (c *CetusExtractor) getBlockNumber(tx *types.UnifiedTransaction) int64 {
	if tx.BlockNumber != nil {
		return tx.BlockNumber.Int64()
	}
	return 0
}

// calculatePriceFromSqrtPrice 根据 sqrt_price 计算价格
// Cetus CLMM 使用 sqrt_price 表示价格，需要转换为实际价格
// price = (sqrt_price / 2^64)^2
// 参考: Cetus CLMM 协议文档
func (c *CetusExtractor) calculatePriceFromSqrtPrice(sqrtPriceStr string) float64 {
	if sqrtPriceStr == "" {
		return 0
	}

	// 解析 sqrt_price
	sqrtPrice := new(big.Float)
	if _, ok := sqrtPrice.SetString(sqrtPriceStr); !ok {
		return 0
	}

	// Q64.64 格式: 除以 2^64
	q64 := new(big.Float).SetInt(new(big.Int).Lsh(big.NewInt(1), 64))
	normalizedSqrtPrice := new(big.Float).Quo(sqrtPrice, q64)

	// 计算价格: price = sqrt_price^2
	price := new(big.Float).Mul(normalizedSqrtPrice, normalizedSqrtPrice)

	// 转换为 float64
	priceFloat, _ := price.Float64()
	return priceFloat
}

// calculateSwapPrice 计算交换价格
// price = amount_out / amount_in
func (c *CetusExtractor) calculateSwapPrice(amountIn, amountOut *big.Int, decimalsIn, decimalsOut int) float64 {
	if amountIn == nil || amountOut == nil || amountIn.Cmp(big.NewInt(0)) == 0 {
		return 0
	}

	// 调整精度
	amountInFloat := new(big.Float).SetInt(amountIn)
	amountOutFloat := new(big.Float).SetInt(amountOut)

	// 根据 decimals 调整
	if decimalsIn > 0 {
		divisor := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimalsIn)), nil))
		amountInFloat.Quo(amountInFloat, divisor)
	}
	if decimalsOut > 0 {
		divisor := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimalsOut)), nil))
		amountOutFloat.Quo(amountOutFloat, divisor)
	}

	// 计算价格
	price := new(big.Float).Quo(amountOutFloat, amountInFloat)
	priceFloat, _ := price.Float64()
	return priceFloat
}
