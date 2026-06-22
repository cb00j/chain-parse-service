package mocks

import (
	"context"
	"math/big"

	"unified-tx-parser/internal/types"

	"github.com/stretchr/testify/mock"
)

// MockChainProcessor is a mock implementation of types.ChainProcessor.
type MockChainProcessor struct {
	mock.Mock
}

func (m *MockChainProcessor) GetChainType() types.ChainType {
	args := m.Called()
	return args.Get(0).(types.ChainType)
}

func (m *MockChainProcessor) GetChainID() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockChainProcessor) GetLatestBlockNumber(ctx context.Context) (*big.Int, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*big.Int), args.Error(1)
}

func (m *MockChainProcessor) GetBlocksByRange(ctx context.Context, startBlock, endBlock *big.Int) ([]types.UnifiedBlock, error) {
	args := m.Called(ctx, startBlock, endBlock)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]types.UnifiedBlock), args.Error(1)
}

func (m *MockChainProcessor) GetBlock(ctx context.Context, blockNumber *big.Int) (*types.UnifiedBlock, error) {
	args := m.Called(ctx, blockNumber)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.UnifiedBlock), args.Error(1)
}

func (m *MockChainProcessor) GetTransaction(ctx context.Context, txHash string) (*types.UnifiedTransaction, error) {
	args := m.Called(ctx, txHash)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*types.UnifiedTransaction), args.Error(1)
}

func (m *MockChainProcessor) HealthCheck(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}
