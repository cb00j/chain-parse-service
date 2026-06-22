package dex

import (
	"fmt"
	"strings"

	"unified-tx-parser/internal/types"
)

// QuoteAssetSetter 支持配置报价资产的提取器
type QuoteAssetSetter interface {
	SetQuoteAssets(assets map[string]int)
}

// ExtractorFactory DEX提取器工厂
type ExtractorFactory struct {
	extractors map[string]types.DexExtractors
}

// NewExtractorFactory 创建提取器工厂
func NewExtractorFactory() *ExtractorFactory {
	return &ExtractorFactory{
		extractors: make(map[string]types.DexExtractors),
	}
}

// RegisterExtractor 注册DEX提取器
func (f *ExtractorFactory) RegisterExtractor(name string, extractor types.DexExtractors) {
	f.extractors[strings.ToLower(name)] = extractor
}

// GetExtractor 获取指定的DEX提取器
func (f *ExtractorFactory) GetExtractor(name string) (types.DexExtractors, error) {
	extractor, exists := f.extractors[strings.ToLower(name)]
	if !exists {
		return nil, fmt.Errorf("未找到DEX提取器: %s", name)
	}
	return extractor, nil
}

// GetAllExtractors 获取所有注册的DEX提取器
func (f *ExtractorFactory) GetAllExtractors() []types.DexExtractors {
	extractors := make([]types.DexExtractors, 0, len(f.extractors))
	for _, extractor := range f.extractors {
		extractors = append(extractors, extractor)
	}
	return extractors
}

// GetExtractorsByChain 获取支持指定链的所有提取器
func (f *ExtractorFactory) GetExtractorsByChain(chainType types.ChainType) []types.DexExtractors {
	var result []types.DexExtractors
	for _, extractor := range f.extractors {
		supportedChains := extractor.GetSupportedChains()
		for _, supportedChain := range supportedChains {
			if supportedChain == chainType {
				result = append(result, extractor)
				break
			}
		}
	}
	return result
}

// GetSupportedProtocols 获取所有支持的协议列表
func (f *ExtractorFactory) GetSupportedProtocols() []string {
	protocolSet := make(map[string]bool)
	for _, extractor := range f.extractors {
		protocols := extractor.GetSupportedProtocols()
		for _, protocol := range protocols {
			protocolSet[protocol] = true
		}
	}

	protocols := make([]string, 0, len(protocolSet))
	for protocol := range protocolSet {
		protocols = append(protocols, protocol)
	}
	return protocols
}

// GetSupportedChains 获取所有支持的链类型列表
func (f *ExtractorFactory) GetSupportedChains() []types.ChainType {
	chainSet := make(map[types.ChainType]bool)
	for _, extractor := range f.extractors {
		chains := extractor.GetSupportedChains()
		for _, chain := range chains {
			chainSet[chain] = true
		}
	}

	chains := make([]types.ChainType, 0, len(chainSet))
	for chain := range chainSet {
		chains = append(chains, chain)
	}
	return chains
}

// ExtractorInfo 提取器信息
type ExtractorInfo struct {
	Name               string           `json:"name"`
	SupportedProtocols []string         `json:"supported_protocols"`
	SupportedChains    []types.ChainType `json:"supported_chains"`
}

// GetExtractorInfo 获取所有提取器的信息
func (f *ExtractorFactory) GetExtractorInfo() []ExtractorInfo {
	var infos []ExtractorInfo

	for name, extractor := range f.extractors {
		info := ExtractorInfo{
			Name:               name,
			SupportedProtocols: extractor.GetSupportedProtocols(),
			SupportedChains:    extractor.GetSupportedChains(),
		}
		infos = append(infos, info)
	}

	return infos
}
