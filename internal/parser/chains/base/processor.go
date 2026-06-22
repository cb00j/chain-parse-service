package base

import (
	"unified-tx-parser/internal/types"

	"github.com/sirupsen/logrus"
)

// Processor provides shared fields and utilities for all chain processors.
type Processor struct {
	ChainType   types.ChainType
	RPCEndpoint string
	BatchSize   int
	Log         *logrus.Entry
	Retry       RetryConfig
}

// NewProcessor creates a base processor with standard configuration.
func NewProcessor(chain types.ChainType, rpcEndpoint string, batchSize int) Processor {
	return Processor{
		ChainType:   chain,
		RPCEndpoint: rpcEndpoint,
		BatchSize:   batchSize,
		Log:         logrus.WithFields(logrus.Fields{"service": "parser", "module": "chain-" + string(chain)}),
		Retry:       DefaultRetryConfig(),
	}
}

// GetChainType returns the chain type.
func (p *Processor) GetChainType() types.ChainType {
	return p.ChainType
}
