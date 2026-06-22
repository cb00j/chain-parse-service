package utils

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/sirupsen/logrus"
)

// RetryConfig controls retry behavior.
type RetryConfig struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
	Multiplier float64
}

// DefaultRetryConfig returns sensible defaults for storage operations.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries: 3,
		BaseDelay:  500 * time.Millisecond,
		MaxDelay:   10 * time.Second,
		Multiplier: 2.0,
	}
}

// Retry executes fn with exponential backoff.
func Retry(ctx context.Context, log *logrus.Entry, name string, cfg RetryConfig, fn func() error) error {
	var lastErr error
	for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%s: context cancelled: %w", name, err)
		}
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		if attempt < cfg.MaxRetries {
			delay := time.Duration(float64(cfg.BaseDelay) * math.Pow(cfg.Multiplier, float64(attempt)))
			if delay > cfg.MaxDelay {
				delay = cfg.MaxDelay
			}
			log.Warnf("%s: attempt %d/%d failed, retrying in %v: %v", name, attempt+1, cfg.MaxRetries, delay, lastErr)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return fmt.Errorf("%s: all %d attempts failed: %w", name, cfg.MaxRetries+1, lastErr)
}
