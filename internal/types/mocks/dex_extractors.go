package mocks

import (
	"context"

	"unified-tx-parser/internal/types"

	"github.com/stretchr/testify/mock"
)

// MockDexExtractors is a mock implementation of types.DexExtractors.
type MockDexExtractors struct {
	mock.Mock
}

func (m *MockDexExtractors) GetSupportedProtocols() []string {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).([]string)
}

func (m *MockDexExtractors) GetSupportedChains() []types.ChainType {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).([]types.ChainType)
}

func (m *MockDexExtractors) ExtractDexData(ctx context.Context, blocks []types.UnifiedBlock) (*types.DexData, error) {
	args := m.Called(ctx, blocks)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.DexData), args.Error(1)
}

func (m *MockDexExtractors) SupportsBlock(block *types.UnifiedBlock) bool {
	args := m.Called(block)
	return args.Bool(0)
}
