// Package fallback provides a ProgressTracker that uses Redis as primary storage
// and a relational DB as a fallback / periodic checkpoint store.
//
// Design goals:
//   - Redis unavailable  → system continues running, progress written to DB only.
//   - Redis data lost    → on restart, progress loaded from DB (may be slightly behind).
//   - Duplicate re-scan  → safe because all storage writes are idempotent (upsert / INSERT IGNORE).
//   - No strong consistency required: DB progress lags Redis by at most syncInterval batches.
package fallback

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"

	"unified-tx-parser/internal/types"
)

// FallbackProgressTracker tries the primary (Redis) first and falls back to
// the secondary (DB) on any error. It also syncs Redis progress to the DB
// every SyncInterval updates so the DB checkpoint stays reasonably fresh.
type FallbackProgressTracker struct {
	primary   types.ProgressTracker // Redis — fast, ephemeral
	secondary types.ProgressTracker // DB    — durable, slightly stale

	// syncInterval: write to DB every N calls to UpdateProgress.
	// Default 10 → DB is at most 10 batches behind Redis.
	syncInterval uint64
	updateCount  atomic.Uint64

	mu             sync.RWMutex
	primaryHealthy bool // last known Redis health
}

// NewFallbackProgressTracker creates a tracker that wraps primary (Redis) and
// secondary (DB). syncInterval controls how often progress is synced to DB.
// A syncInterval of 0 defaults to 10.
func NewFallbackProgressTracker(primary, secondary types.ProgressTracker, syncInterval uint64) *FallbackProgressTracker {
	if syncInterval == 0 {
		syncInterval = 10
	}
	return &FallbackProgressTracker{
		primary:        primary,
		secondary:      secondary,
		syncInterval:   syncInterval,
		primaryHealthy: true,
	}
}

// MarkPrimaryUnhealthy forces the fallback to treat the primary as down.
// Call this at startup when Redis is known to be unreachable.
func (f *FallbackProgressTracker) MarkPrimaryUnhealthy() {
	f.markPrimary(false)
}

// isPrimaryHealthy returns the cached health state (cheap read).
func (f *FallbackProgressTracker) isPrimaryHealthy() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.primaryHealthy
}

// markPrimary records whether Redis is healthy and logs transitions.
func (f *FallbackProgressTracker) markPrimary(healthy bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.primaryHealthy != healthy {
		if healthy {
			log.Warn("[progress] Redis recovered — switching back to primary")
		} else {
			log.Warn("[progress] Redis unavailable — falling back to DB progress tracker")
		}
		f.primaryHealthy = healthy
	}
}

// GetProgress reads progress, preferring Redis. Falls back to DB on error.
// On restart with empty Redis: DB row is used → system re-scans from DB
// checkpoint → idempotent upserts ensure no duplicate data.
func (f *FallbackProgressTracker) GetProgress(chainType types.ChainType) (*types.ProcessProgress, error) {
	if f.isPrimaryHealthy() {
		p, err := f.primary.GetProgress(chainType)
		if err == nil {
			f.markPrimary(true)
			return p, nil
		}
		f.markPrimary(false)
		log.Warnf("[progress] Redis GetProgress failed (%v), reading from DB", err)
	}
	return f.secondary.GetProgress(chainType)
}

// UpdateProgress writes to Redis (if healthy) and periodically syncs to DB.
// If Redis is down, writes go directly to DB so progress is never lost.
func (f *FallbackProgressTracker) UpdateProgress(chainType types.ChainType, progress *types.ProcessProgress) error {
	count := f.updateCount.Add(1)
	shouldSync := count%f.syncInterval == 0

	if f.isPrimaryHealthy() {
		err := f.primary.UpdateProgress(chainType, progress)
		if err != nil {
			f.markPrimary(false)
			log.Warnf("[progress] Redis UpdateProgress failed (%v), writing to DB", err)
			// Fall through to DB write below.
		} else {
			f.markPrimary(true)
			// Periodic sync to DB.
			if shouldSync {
				if dbErr := f.secondary.UpdateProgress(chainType, progress); dbErr != nil {
					log.Warnf("[progress] DB sync failed (non-fatal): %v", dbErr)
				}
			}
			return nil
		}
	}

	// Primary down: write directly to DB on every update (not just sync interval)
	// so the checkpoint stays as fresh as possible during degraded mode.
	return f.secondary.UpdateProgress(chainType, progress)
}

// ResetProgress resets both stores.
func (f *FallbackProgressTracker) ResetProgress(chainType types.ChainType) error {
	var firstErr error
	if err := f.primary.ResetProgress(chainType); err != nil {
		log.Warnf("[progress] Redis ResetProgress failed (non-fatal): %v", err)
		firstErr = err
	}
	if err := f.secondary.ResetProgress(chainType); err != nil {
		return err
	}
	_ = firstErr
	return nil
}

// GetAllProgress prefers Redis; falls back to DB.
func (f *FallbackProgressTracker) GetAllProgress() (map[types.ChainType]*types.ProcessProgress, error) {
	if f.isPrimaryHealthy() {
		result, err := f.primary.GetAllProgress()
		if err == nil {
			return result, nil
		}
		f.markPrimary(false)
	}
	return f.secondary.GetAllProgress()
}

// UpdateMultipleProgress writes to both stores (best-effort on Redis).
func (f *FallbackProgressTracker) UpdateMultipleProgress(progresses map[types.ChainType]*types.ProcessProgress) error {
	if f.isPrimaryHealthy() {
		if err := f.primary.UpdateMultipleProgress(progresses); err != nil {
			f.markPrimary(false)
			log.Warnf("[progress] Redis UpdateMultipleProgress failed: %v", err)
		} else {
			f.markPrimary(true)
		}
	}
	return f.secondary.UpdateMultipleProgress(progresses)
}

// GetProcessingStats delegates to Redis (richer data); falls back to DB.
func (f *FallbackProgressTracker) GetProcessingStats(chainType types.ChainType) (*types.ProcessingStats, error) {
	if f.isPrimaryHealthy() {
		s, err := f.primary.GetProcessingStats(chainType)
		if err == nil {
			return s, nil
		}
		f.markPrimary(false)
	}
	return f.secondary.GetProcessingStats(chainType)
}

// GetGlobalStats delegates to Redis; falls back to DB.
func (f *FallbackProgressTracker) GetGlobalStats() (*types.GlobalProcessingStats, error) {
	if f.isPrimaryHealthy() {
		s, err := f.primary.GetGlobalStats()
		if err == nil {
			return s, nil
		}
		f.markPrimary(false)
	}
	return f.secondary.GetGlobalStats()
}

// SetProcessingStatus writes to both stores.
func (f *FallbackProgressTracker) SetProcessingStatus(chainType types.ChainType, status types.ProcessingStatus) error {
	if f.isPrimaryHealthy() {
		if err := f.primary.SetProcessingStatus(chainType, status); err != nil {
			f.markPrimary(false)
		} else {
			f.markPrimary(true)
		}
	}
	return f.secondary.SetProcessingStatus(chainType, status)
}

// GetProcessingStatus prefers Redis; falls back to DB.
func (f *FallbackProgressTracker) GetProcessingStatus(chainType types.ChainType) (types.ProcessingStatus, error) {
	if f.isPrimaryHealthy() {
		s, err := f.primary.GetProcessingStatus(chainType)
		if err == nil {
			return s, nil
		}
		f.markPrimary(false)
	}
	return f.secondary.GetProcessingStatus(chainType)
}

// RecordError records to Redis (with full error detail) and increments the
// DB error counter. If Redis is down, only the DB counter is updated.
func (f *FallbackProgressTracker) RecordError(chainType types.ChainType, recErr error) error {
	if f.isPrimaryHealthy() {
		if err := f.primary.RecordError(chainType, recErr); err != nil {
			f.markPrimary(false)
		} else {
			f.markPrimary(true)
		}
	}
	// Always increment DB error counter so it stays roughly accurate.
	if err := f.secondary.RecordError(chainType, recErr); err != nil {
		log.Warnf("[progress] DB RecordError failed (non-fatal): %v", err)
	}
	return nil
}

// GetErrorHistory reads from Redis (full history); falls back to empty slice from DB.
func (f *FallbackProgressTracker) GetErrorHistory(chainType types.ChainType, limit int) ([]types.ProcessingError, error) {
	if f.isPrimaryHealthy() {
		h, err := f.primary.GetErrorHistory(chainType, limit)
		if err == nil {
			return h, nil
		}
		f.markPrimary(false)
	}
	return f.secondary.GetErrorHistory(chainType, limit)
}

// StartHealthCheck launches a background goroutine that periodically probes
// Redis and restores the primary when it recovers. Call this after creation.
// The goroutine stops when the done channel is closed.
func (f *FallbackProgressTracker) StartHealthCheck(interval time.Duration, done <-chan struct{}) {
	type healthChecker interface {
		HealthCheck() error
	}
	checker, ok := f.primary.(healthChecker)
	if !ok {
		return // Redis tracker doesn't expose HealthCheck — skip.
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if err := checker.HealthCheck(); err != nil {
					f.markPrimary(false)
				} else {
					f.markPrimary(true)
				}
			}
		}
	}()
}

// ClearErrorHistory clears from both stores.
func (f *FallbackProgressTracker) ClearErrorHistory(chainType types.ChainType) error {
	if f.isPrimaryHealthy() {
		if err := f.primary.ClearErrorHistory(chainType); err != nil {
			f.markPrimary(false)
		} else {
			f.markPrimary(true)
		}
	}
	return f.secondary.ClearErrorHistory(chainType)
}

// RecordProcessingMetrics records to both stores (best-effort on Redis).
func (f *FallbackProgressTracker) RecordProcessingMetrics(chainType types.ChainType, metrics *types.ProcessingMetrics) error {
	if f.isPrimaryHealthy() {
		if err := f.primary.RecordProcessingMetrics(chainType, metrics); err != nil {
			f.markPrimary(false)
		} else {
			f.markPrimary(true)
		}
	}
	return f.secondary.RecordProcessingMetrics(chainType, metrics)
}

// GetPerformanceMetrics prefers Redis (richer data); falls back to DB.
func (f *FallbackProgressTracker) GetPerformanceMetrics(chainType types.ChainType, duration time.Duration) (*types.PerformanceReport, error) {
	if f.isPrimaryHealthy() {
		r, err := f.primary.GetPerformanceMetrics(chainType, duration)
		if err == nil {
			return r, nil
		}
		f.markPrimary(false)
	}
	return f.secondary.GetPerformanceMetrics(chainType, duration)
}

// HealthCheck reports healthy if either store is reachable.
// Redis is checked first; if it is healthy the DB check is skipped.
func (f *FallbackProgressTracker) HealthCheck() error {
	if err := f.primary.HealthCheck(); err == nil {
		f.markPrimary(true)
		return nil
	}
	f.markPrimary(false)
	// Fall back to DB health check.
	if err := f.secondary.HealthCheck(); err != nil {
		return fmt.Errorf("both primary (Redis) and secondary (DB) are unhealthy: %w", err)
	}
	return nil
}

// Cleanup delegates to both stores.
func (f *FallbackProgressTracker) Cleanup(olderThan time.Duration) error {
	var firstErr error
	if f.isPrimaryHealthy() {
		if err := f.primary.Cleanup(olderThan); err != nil {
			log.Warnf("[progress] Redis Cleanup failed (non-fatal): %v", err)
			firstErr = err
		}
	}
	if err := f.secondary.Cleanup(olderThan); err != nil {
		return err
	}
	_ = firstErr
	return nil
}
