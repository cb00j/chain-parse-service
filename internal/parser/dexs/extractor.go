package dex

import (
	"context"

	"unified-tx-parser/internal/model"
	"unified-tx-parser/internal/types"

	"github.com/sirupsen/logrus"
)

var extractorLog = logrus.WithFields(logrus.Fields{"service": "parser", "module": "dex-extractor"})

// DEXExtractor 通用DEX事件提取器
type DEXExtractor struct {
	supportedChains []types.ChainType
	factory         *ExtractorFactory
}

// NewDEXExtractor 使用指定的工厂创建DEX事件提取器
func NewDEXExtractor(factory *ExtractorFactory) *DEXExtractor {
	return &DEXExtractor{
		supportedChains: make([]types.ChainType, 0),
		factory:         factory,
	}
}

// GetSupportedProtocols 获取支持的协议
func (d *DEXExtractor) GetSupportedProtocols() []string {
	if d.factory == nil {
		return nil
	}
	extractors := d.factory.GetAllExtractors()
	protocols := make([]string, 0, len(extractors))
	for _, ext := range extractors {
		protocols = append(protocols, ext.GetSupportedProtocols()...)
	}
	return protocols
}

// GetSupportedChains 获取支持的链类型
func (d *DEXExtractor) GetSupportedChains() []types.ChainType {
	return d.supportedChains
}

// ExtractDexData 从统一区块数据中提取DEX相关数据
func (d *DEXExtractor) ExtractDexData(ctx context.Context, blocks []types.UnifiedBlock) (*types.DexData, error) {
	return d.extractWithFactory(ctx, blocks)
}

// extractWithFactory 使用工厂模式提取数据
func (d *DEXExtractor) extractWithFactory(ctx context.Context, blocks []types.UnifiedBlock) (*types.DexData, error) {
	// 创建空的DEX数据结构
	dexData := &types.DexData{
		Pools:        make([]model.Pool, 0),
		Transactions: make([]model.Transaction, 0),
		Liquidities:  make([]model.Liquidity, 0),
		Reserves:     make([]model.Reserve, 0),
		Tokens:       make([]model.Token, 0),
	}

	// 获取所有提取器
	extractors := d.factory.GetAllExtractors()

	// 使用每个提取器处理数据
	for _, extractor := range extractors {
		// 过滤支持的区块
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
		extractorData, err := extractor.ExtractDexData(ctx, supportedBlocks)
		if err != nil {
			extractorLog.Warnf("DEX extractor data extraction failed: %v", err)
			continue
		}

		// 合并数据
		dexData.Pools = append(dexData.Pools, extractorData.Pools...)
		dexData.Transactions = append(dexData.Transactions, extractorData.Transactions...)
		dexData.Liquidities = append(dexData.Liquidities, extractorData.Liquidities...)
		dexData.Reserves = append(dexData.Reserves, extractorData.Reserves...)
		dexData.Tokens = append(dexData.Tokens, extractorData.Tokens...)
	}

	return dexData, nil
}

// SupportsBlock 检查是否支持该区块
func (d *DEXExtractor) SupportsBlock(block *types.UnifiedBlock) bool {
	if d.factory == nil {
		return false
	}
	for _, ext := range d.factory.GetAllExtractors() {
		if ext.SupportsBlock(block) {
			return true
		}
	}
	return false
}
