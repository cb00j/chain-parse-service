package dex

import (
	"testing"

	"unified-tx-parser/internal/types"
	"unified-tx-parser/internal/types/mocks"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewExtractorFactory(t *testing.T) {
	factory := NewExtractorFactory()
	require.NotNil(t, factory)
	assert.Empty(t, factory.GetAllExtractors())
}

func TestRegisterAndGetExtractor(t *testing.T) {
	factory := NewExtractorFactory()
	mockExt := new(mocks.MockDexExtractors)
	mockExt.On("GetSupportedProtocols").Return([]string{"test-dex"})
	mockExt.On("GetSupportedChains").Return([]types.ChainType{types.ChainTypeBSC})

	factory.RegisterExtractor("TestDex", mockExt)

	// GetExtractor should be case-insensitive
	ext, err := factory.GetExtractor("testdex")
	require.NoError(t, err)
	assert.Equal(t, mockExt, ext)

	ext2, err := factory.GetExtractor("TESTDEX")
	require.NoError(t, err)
	assert.Equal(t, mockExt, ext2)
}

func TestGetExtractor_NotFound(t *testing.T) {
	factory := NewExtractorFactory()

	ext, err := factory.GetExtractor("nonexistent")
	assert.Nil(t, ext)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent")
}

func TestGetAllExtractors(t *testing.T) {
	factory := NewExtractorFactory()

	mock1 := new(mocks.MockDexExtractors)
	mock2 := new(mocks.MockDexExtractors)

	factory.RegisterExtractor("dex1", mock1)
	factory.RegisterExtractor("dex2", mock2)

	all := factory.GetAllExtractors()
	assert.Len(t, all, 2)
}

func TestGetExtractorsByChain(t *testing.T) {
	factory := NewExtractorFactory()

	bscExtractor := new(mocks.MockDexExtractors)
	bscExtractor.On("GetSupportedChains").Return([]types.ChainType{types.ChainTypeBSC})

	ethExtractor := new(mocks.MockDexExtractors)
	ethExtractor.On("GetSupportedChains").Return([]types.ChainType{types.ChainTypeEthereum})

	multiExtractor := new(mocks.MockDexExtractors)
	multiExtractor.On("GetSupportedChains").Return([]types.ChainType{types.ChainTypeBSC, types.ChainTypeEthereum})

	factory.RegisterExtractor("bsc-dex", bscExtractor)
	factory.RegisterExtractor("eth-dex", ethExtractor)
	factory.RegisterExtractor("multi-dex", multiExtractor)

	bscResults := factory.GetExtractorsByChain(types.ChainTypeBSC)
	assert.Len(t, bscResults, 2) // bsc-dex + multi-dex

	ethResults := factory.GetExtractorsByChain(types.ChainTypeEthereum)
	assert.Len(t, ethResults, 2) // eth-dex + multi-dex

	solResults := factory.GetExtractorsByChain(types.ChainTypeSolana)
	assert.Empty(t, solResults)
}

func TestGetSupportedProtocols(t *testing.T) {
	factory := NewExtractorFactory()

	mock1 := new(mocks.MockDexExtractors)
	mock1.On("GetSupportedProtocols").Return([]string{"proto-a", "proto-b"})

	mock2 := new(mocks.MockDexExtractors)
	mock2.On("GetSupportedProtocols").Return([]string{"proto-b", "proto-c"})

	factory.RegisterExtractor("dex1", mock1)
	factory.RegisterExtractor("dex2", mock2)

	protocols := factory.GetSupportedProtocols()
	// proto-b should be deduplicated
	assert.Len(t, protocols, 3)
	assert.ElementsMatch(t, []string{"proto-a", "proto-b", "proto-c"}, protocols)
}

func TestGetSupportedChains(t *testing.T) {
	factory := NewExtractorFactory()

	mock1 := new(mocks.MockDexExtractors)
	mock1.On("GetSupportedChains").Return([]types.ChainType{types.ChainTypeBSC})

	mock2 := new(mocks.MockDexExtractors)
	mock2.On("GetSupportedChains").Return([]types.ChainType{types.ChainTypeBSC, types.ChainTypeEthereum})

	factory.RegisterExtractor("dex1", mock1)
	factory.RegisterExtractor("dex2", mock2)

	chains := factory.GetSupportedChains()
	assert.Len(t, chains, 2) // BSC deduplicated
	assert.ElementsMatch(t, []types.ChainType{types.ChainTypeBSC, types.ChainTypeEthereum}, chains)
}

func TestGetExtractorInfo(t *testing.T) {
	factory := NewExtractorFactory()

	mockExt := new(mocks.MockDexExtractors)
	mockExt.On("GetSupportedProtocols").Return([]string{"pancakeswap"})
	mockExt.On("GetSupportedChains").Return([]types.ChainType{types.ChainTypeBSC})

	factory.RegisterExtractor("pancake", mockExt)

	infos := factory.GetExtractorInfo()
	require.Len(t, infos, 1)
	assert.Equal(t, "pancake", infos[0].Name)
	assert.Equal(t, []string{"pancakeswap"}, infos[0].SupportedProtocols)
	assert.Equal(t, []types.ChainType{types.ChainTypeBSC}, infos[0].SupportedChains)
}

func TestRegisterExtractor_OverwriteExisting(t *testing.T) {
	factory := NewExtractorFactory()

	mock1 := new(mocks.MockDexExtractors)
	mock2 := new(mocks.MockDexExtractors)

	factory.RegisterExtractor("dex", mock1)
	factory.RegisterExtractor("dex", mock2)

	ext, err := factory.GetExtractor("dex")
	require.NoError(t, err)
	assert.Equal(t, mock2, ext) // should be overwritten
	assert.Len(t, factory.GetAllExtractors(), 1)
}

