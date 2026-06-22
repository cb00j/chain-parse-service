package base

import (
	"context"
	"time"

	"unified-tx-parser/internal/utils"

	"github.com/sirupsen/logrus"
)

// RetryConfig controls retry behavior.
// This is a type alias for utils.RetryConfig to avoid breaking existing code.
type RetryConfig = utils.RetryConfig

// DefaultRetryConfig returns sensible defaults for chain operations.
func DefaultRetryConfig() RetryConfig {
	return utils.RetryConfig{
		MaxRetries: 3,
		BaseDelay:  time.Second,
		MaxDelay:   30 * time.Second,
		Multiplier: 2.0,
	}
}

// Retry executes fn with exponential backoff.
// Delegates to utils.Retry for the actual implementation.
func Retry(ctx context.Context, log *logrus.Entry, name string, cfg RetryConfig, fn func() error) error {
	return utils.Retry(ctx, log, name, cfg, fn)
}
