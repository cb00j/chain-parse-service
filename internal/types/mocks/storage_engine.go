package mocks

import (
	"context"

	"unified-tx-parser/internal/types"

	"github.com/stretchr/testify/mock"
)

// MockStorageEngine is a mock implementation of types.StorageEngine.
type MockStorageEngine struct {
	mock.Mock
}

func (m *MockStorageEngine) StoreBlocks(ctx context.Context, blocks []types.UnifiedBlock) error {
	args := m.Called(ctx, blocks)
	return args.Error(0)
}

func (m *MockStorageEngine) StoreTransactions(ctx context.Context, txs []types.UnifiedTransaction) error {
	args := m.Called(ctx, txs)
	return args.Error(0)
}

func (m *MockStorageEngine) StoreDexData(ctx context.Context, dexData *types.DexData) error {
	args := m.Called(ctx, dexData)
	return args.Error(0)
}

func (m *MockStorageEngine) GetTransactionsByHash(ctx context.Context, hashes []string) ([]types.UnifiedTransaction, error) {
	args := m.Called(ctx, hashes)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]types.UnifiedTransaction), args.Error(1)
}

func (m *MockStorageEngine) GetStorageStats(ctx context.Context) (map[string]interface{}, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]interface{}), args.Error(1)
}

func (m *MockStorageEngine) HealthCheck(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockStorageEngine) Close() error {
	args := m.Called()
	return args.Error(0)
}
