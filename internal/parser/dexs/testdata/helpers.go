package testdata

import (
	"math/big"
	"time"

	"unified-tx-parser/internal/model"
	"unified-tx-parser/internal/types"
)

// BSCSwapBlock creates a BSC block with a single swap event for testing.
func BSCSwapBlock(blockNum int64) types.UnifiedBlock {
	return types.UnifiedBlock{
		BlockNumber: big.NewInt(blockNum),
		BlockHash:   "0xbscblock",
		ChainType:   types.ChainTypeBSC,
		ChainID:     "56",
		Timestamp:   time.Now(),
		TxCount:     1,
		Transactions: []types.UnifiedTransaction{
			{
				TxHash:      "0xbsctx1",
				ChainType:   types.ChainTypeBSC,
				ChainID:     "56",
				BlockNumber: big.NewInt(blockNum),
				FromAddress: "0xuser1",
				ToAddress:   "0xrouter",
				Value:       big.NewInt(0),
				Status:      types.TransactionStatusSuccess,
				Timestamp:   time.Now(),
			},
		},
		Events: []types.UnifiedEvent{
			{
				EventID:     "swap-1",
				ChainType:   types.ChainTypeBSC,
				ChainID:     "56",
				BlockNumber: big.NewInt(blockNum),
				TxHash:      "0xbsctx1",
				EventIndex:  0,
				EventType:   "Swap",
				Address:     "0xpancakepair",
				Timestamp:   time.Now(),
			},
		},
	}
}

// EthereumSwapBlock creates an Ethereum block with a single swap event for testing.
func EthereumSwapBlock(blockNum int64) types.UnifiedBlock {
	return types.UnifiedBlock{
		BlockNumber: big.NewInt(blockNum),
		BlockHash:   "0xethblock",
		ChainType:   types.ChainTypeEthereum,
		ChainID:     "1",
		Timestamp:   time.Now(),
		TxCount:     1,
		Transactions: []types.UnifiedTransaction{
			{
				TxHash:      "0xethtx1",
				ChainType:   types.ChainTypeEthereum,
				ChainID:     "1",
				BlockNumber: big.NewInt(blockNum),
				FromAddress: "0xuser2",
				ToAddress:   "0xuniswaprouter",
				Value:       big.NewInt(0),
				Status:      types.TransactionStatusSuccess,
				Timestamp:   time.Now(),
			},
		},
		Events: []types.UnifiedEvent{
			{
				EventID:     "swap-eth-1",
				ChainType:   types.ChainTypeEthereum,
				ChainID:     "1",
				BlockNumber: big.NewInt(blockNum),
				TxHash:      "0xethtx1",
				EventIndex:  0,
				EventType:   "Swap",
				Address:     "0xuniswappair",
				Timestamp:   time.Now(),
			},
		},
	}
}

// EmptyBlock creates a block with no transactions or events.
func EmptyBlock(chainType types.ChainType, blockNum int64) types.UnifiedBlock {
	return types.UnifiedBlock{
		BlockNumber:  big.NewInt(blockNum),
		BlockHash:    "0xemptyblock",
		ChainType:    chainType,
		ChainID:      string(chainType),
		Timestamp:    time.Now(),
		TxCount:      0,
		Transactions: []types.UnifiedTransaction{},
		Events:       []types.UnifiedEvent{},
	}
}

// SampleDexData creates a DexData with one pool, one transaction, and one token.
func SampleDexData() *types.DexData {
	return &types.DexData{
		Pools: []model.Pool{
			{
				Addr:     "0xpool1",
				Protocol: "test-protocol",
				Tokens:   map[int]string{0: "0xtokenA", 1: "0xtokenB"},
				Fee:      30,
			},
		},
		Transactions: []model.Transaction{
			{
				Pool:   "0xpool1",
				Hash:   "0xtx1",
				From:   "0xuser",
				Side:   "buy",
				Amount: big.NewInt(1000000),
				Time:   uint64(time.Now().Unix()),
			},
		},
		Tokens: []model.Token{
			{
				Addr:     "0xtokenA",
				Name:     "Token A",
				Symbol:   "TKA",
				Decimals: 18,
			},
		},
		Liquidities: []model.Liquidity{},
		Reserves:    []model.Reserve{},
	}
}
