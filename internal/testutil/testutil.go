package testutil

import (
	"math/big"
	"time"

	"unified-tx-parser/internal/model"
	"unified-tx-parser/internal/types"
)

// NewTestBlock creates a UnifiedBlock with sensible defaults for testing.
func NewTestBlock(chainType types.ChainType, blockNum int64) types.UnifiedBlock {
	return types.UnifiedBlock{
		BlockNumber:  big.NewInt(blockNum),
		BlockHash:    "0xblock" + big.NewInt(blockNum).String(),
		ChainType:    chainType,
		ChainID:      string(chainType),
		ParentHash:   "0xparent",
		Timestamp:    time.Now(),
		GasLimit:     big.NewInt(30000000),
		GasUsed:      big.NewInt(1000000),
		TxCount:      0,
		Transactions: []types.UnifiedTransaction{},
		Events:       []types.UnifiedEvent{},
	}
}

// NewTestTransaction creates a UnifiedTransaction with sensible defaults for testing.
func NewTestTransaction(chainType types.ChainType, txHash string, blockNum int64) types.UnifiedTransaction {
	return types.UnifiedTransaction{
		TxHash:      txHash,
		ChainType:   chainType,
		ChainID:     string(chainType),
		BlockNumber: big.NewInt(blockNum),
		BlockHash:   "0xblock" + big.NewInt(blockNum).String(),
		TxIndex:     0,
		FromAddress: "0xfrom",
		ToAddress:   "0xto",
		Value:       big.NewInt(0),
		GasLimit:    big.NewInt(21000),
		GasUsed:     big.NewInt(21000),
		GasPrice:    big.NewInt(5000000000),
		Status:      types.TransactionStatusSuccess,
		Timestamp:   time.Now(),
	}
}

// NewTestEvent creates a UnifiedEvent with sensible defaults for testing.
func NewTestEvent(chainType types.ChainType, txHash string, blockNum int64, eventIndex int) types.UnifiedEvent {
	return types.UnifiedEvent{
		EventID:     txHash + "-" + big.NewInt(int64(eventIndex)).String(),
		ChainType:   chainType,
		ChainID:     string(chainType),
		BlockNumber: big.NewInt(blockNum),
		BlockHash:   "0xblock" + big.NewInt(blockNum).String(),
		TxHash:      txHash,
		TxIndex:     0,
		EventIndex:  eventIndex,
		Timestamp:   time.Now(),
	}
}

// NewTestDexData creates an empty DexData for testing.
func NewTestDexData() *types.DexData {
	return &types.DexData{
		Pools:        []model.Pool{},
		Transactions: []model.Transaction{},
		Liquidities:  []model.Liquidity{},
		Reserves:     []model.Reserve{},
		Tokens:       []model.Token{},
	}
}

// NewTestPool creates a Pool with test defaults.
func NewTestPool(addr, protocol string, tokens map[int]string) model.Pool {
	return model.Pool{
		Addr:     addr,
		Protocol: protocol,
		Tokens:   tokens,
		Fee:      30,
	}
}

// NewTestToken creates a Token with test defaults.
func NewTestToken(addr, symbol string, decimals int) model.Token {
	return model.Token{
		Addr:     addr,
		Name:     symbol + " Token",
		Symbol:   symbol,
		Decimals: decimals,
	}
}

// NewTestModelTransaction creates a model.Transaction with test defaults.
func NewTestModelTransaction(pool, hash, from, side string, amount *big.Int) model.Transaction {
	return model.Transaction{
		Pool:   pool,
		Hash:   hash,
		From:   from,
		Side:   side,
		Amount: amount,
		Time:   uint64(time.Now().Unix()),
	}
}

// NewTestBlockWithEvents creates a block containing the given events.
func NewTestBlockWithEvents(chainType types.ChainType, blockNum int64, events []types.UnifiedEvent) types.UnifiedBlock {
	block := NewTestBlock(chainType, blockNum)
	block.Events = events
	block.TxCount = len(events)
	return block
}
