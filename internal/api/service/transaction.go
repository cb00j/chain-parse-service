package service

import (
	"context"
	"errors"

	"unified-tx-parser/internal/types"
)

var ErrTransactionNotFound = errors.New("transaction not found")

type TransactionService struct {
	storage types.StorageEngine
}

func NewTransactionService(storage types.StorageEngine) *TransactionService {
	return &TransactionService{storage: storage}
}

func (s *TransactionService) GetByHash(ctx context.Context, hash string) (interface{}, error) {
	txs, err := s.storage.GetTransactionsByHash(ctx, []string{hash})
	if err != nil {
		return nil, err
	}
	if len(txs) == 0 {
		return nil, ErrTransactionNotFound
	}
	return txs[0], nil
}
