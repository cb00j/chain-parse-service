package bsc

import (
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

// makeMinimalEthLog creates a minimal *ethtypes.Log with a single topic for testing
// internal methods like getLogType, isPancakeSwapLog, etc.
func makeMinimalEthLog(topic0 string) *ethtypes.Log {
	log := &ethtypes.Log{}
	if topic0 != "" {
		log.Topics = []common.Hash{common.HexToHash(topic0)}
	}
	return log
}

// makeMinimalEthLogWithAddr creates a minimal *ethtypes.Log with address and topic.
func makeMinimalEthLogWithAddr(addr, topic0 string) *ethtypes.Log {
	log := &ethtypes.Log{
		Address: common.HexToAddress(addr),
	}
	if topic0 != "" {
		log.Topics = []common.Hash{common.HexToHash(topic0)}
	}
	return log
}
