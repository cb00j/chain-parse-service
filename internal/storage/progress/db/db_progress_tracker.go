package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"unified-tx-parser/internal/types"
)

// Dialect selects the SQL placeholder style.
type Dialect string

const (
	DialectMySQL    Dialect = "mysql"    // uses ? placeholders
	DialectPostgres Dialect = "postgres" // uses $1 $2 ... placeholders
)

// DBProgressTracker implements types.ProgressTracker using a relational database.
//
// Design notes:
//   - Core progress (block number, status, counters) is stored in processing_progress.
//   - Error history is stored in processing_errors (one row per error, capped by maxErrorHistory).
//   - Metrics history is stored in processing_metrics (one row per batch, capped by maxMetricsHistory).
//   - This is a durable fallback store, not a real-time tracker. All writes are synchronous.
type DBProgressTracker struct {
	db                *sql.DB
	dialect           Dialect
	maxErrorHistory   int
	maxMetricsHistory int
}

// NewDBProgressTracker creates a DB-backed progress tracker.
func NewDBProgressTracker(db *sql.DB, dialect Dialect) *DBProgressTracker {
	return &DBProgressTracker{
		db:                db,
		dialect:           dialect,
		maxErrorHistory:   1000,
		maxMetricsHistory: 10000,
	}
}

// ph returns the positional placeholder for position n (1-based).
func (d *DBProgressTracker) ph(n int) string {
	if d.dialect == DialectPostgres {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}

// ── processing_progress ────────────────────────────────────────────────────

func (d *DBProgressTracker) progressUpsertSQL() string {
	if d.dialect == DialectPostgres {
		return `INSERT INTO processing_progress
			(chain_type, last_processed_block, last_update_time,
			 total_transactions, total_events, status, error_count,
			 success_rate, start_time, extra)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
			ON CONFLICT (chain_type) DO UPDATE SET
				last_processed_block = EXCLUDED.last_processed_block,
				last_update_time     = EXCLUDED.last_update_time,
				total_transactions   = EXCLUDED.total_transactions,
				total_events         = EXCLUDED.total_events,
				status               = EXCLUDED.status,
				error_count          = EXCLUDED.error_count,
				success_rate         = EXCLUDED.success_rate,
				start_time           = EXCLUDED.start_time,
				extra                = EXCLUDED.extra,
				updated_at           = CURRENT_TIMESTAMP`
	}
	return `INSERT INTO processing_progress
		(chain_type, last_processed_block, last_update_time,
		 total_transactions, total_events, status, error_count,
		 success_rate, start_time, extra)
		VALUES (?,?,?,?,?,?,?,?,?,?)
		ON DUPLICATE KEY UPDATE
			last_processed_block = VALUES(last_processed_block),
			last_update_time     = VALUES(last_update_time),
			total_transactions   = VALUES(total_transactions),
			total_events         = VALUES(total_events),
			status               = VALUES(status),
			error_count          = VALUES(error_count),
			success_rate         = VALUES(success_rate),
			start_time           = VALUES(start_time),
			extra                = VALUES(extra),
			updated_at           = CURRENT_TIMESTAMP`
}

func (d *DBProgressTracker) upsertProgress(ctx context.Context, chainType types.ChainType, progress *types.ProcessProgress) error {
	if progress.TotalTransactions > 0 {
		successful := progress.TotalTransactions - progress.ErrorCount
		progress.SuccessRate = float64(successful) / float64(progress.TotalTransactions) * 100
	}
	var lastBlock int64
	if progress.LastProcessedBlock != nil {
		lastBlock = progress.LastProcessedBlock.Int64()
	}
	extra, _ := json.Marshal(map[string]interface{}{
		"last_error_time": progress.LastErrorTime,
	})
	_, err := d.db.ExecContext(ctx, d.progressUpsertSQL(),
		string(chainType), lastBlock,
		progress.LastUpdateTime, progress.TotalTransactions,
		progress.TotalEvents, string(progress.ProcessingStatus),
		progress.ErrorCount, progress.SuccessRate,
		progress.StartTime, string(extra),
	)
	return err
}

func (d *DBProgressTracker) GetProgress(chainType types.ChainType) (*types.ProcessProgress, error) {
	ctx := context.Background()
	q := fmt.Sprintf(`SELECT last_processed_block, last_update_time,
		total_transactions, total_events, status, error_count,
		success_rate, start_time
		FROM processing_progress WHERE chain_type = %s`, d.ph(1))

	var (
		lastBlock   int64
		lastUpdate  time.Time
		totalTxs    int64
		totalEvents int64
		status      string
		errorCount  int64
		successRate float64
		startTime   time.Time
	)
	err := d.db.QueryRowContext(ctx, q, string(chainType)).Scan(
		&lastBlock, &lastUpdate, &totalTxs, &totalEvents,
		&status, &errorCount, &successRate, &startTime,
	)
	if err == sql.ErrNoRows {
		return &types.ProcessProgress{
			ChainType:          chainType,
			LastProcessedBlock: big.NewInt(0),
			LastUpdateTime:     time.Now(),
			ProcessingStatus:   types.ProcessingStatusIdle,
			StartTime:          time.Now(),
			SuccessRate:        100.0,
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("db: GetProgress %s: %w", chainType, err)
	}
	return &types.ProcessProgress{
		ChainType:          chainType,
		LastProcessedBlock: big.NewInt(lastBlock),
		LastUpdateTime:     lastUpdate,
		TotalTransactions:  totalTxs,
		TotalEvents:        totalEvents,
		ProcessingStatus:   types.ProcessingStatus(status),
		ErrorCount:         errorCount,
		SuccessRate:        successRate,
		StartTime:          startTime,
	}, nil
}

func (d *DBProgressTracker) UpdateProgress(chainType types.ChainType, progress *types.ProcessProgress) error {
	if progress == nil {
		return fmt.Errorf("db: progress must not be nil")
	}
	progress.LastUpdateTime = time.Now()
	progress.ChainType = chainType
	if err := d.upsertProgress(context.Background(), chainType, progress); err != nil {
		return fmt.Errorf("db: UpdateProgress %s: %w", chainType, err)
	}
	return nil
}

func (d *DBProgressTracker) ResetProgress(chainType types.ChainType) error {
	ctx := context.Background()
	// Delete errors and metrics for this chain first.
	for _, table := range []string{"processing_errors", "processing_metrics"} {
		q := fmt.Sprintf("DELETE FROM %s WHERE chain_type = %s", table, d.ph(1))
		if _, err := d.db.ExecContext(ctx, q, string(chainType)); err != nil {
			return fmt.Errorf("db: ResetProgress clear %s: %w", table, err)
		}
	}
	// Reset the main progress row.
	defaults := &types.ProcessProgress{
		ChainType:          chainType,
		LastProcessedBlock: big.NewInt(0),
		LastUpdateTime:     time.Now(),
		ProcessingStatus:   types.ProcessingStatusIdle,
		StartTime:          time.Now(),
		SuccessRate:        100.0,
	}
	if err := d.upsertProgress(ctx, chainType, defaults); err != nil {
		return fmt.Errorf("db: ResetProgress upsert %s: %w", chainType, err)
	}
	return nil
}

func (d *DBProgressTracker) GetAllProgress() (map[types.ChainType]*types.ProcessProgress, error) {
	ctx := context.Background()
	rows, err := d.db.QueryContext(ctx, `SELECT chain_type, last_processed_block,
		last_update_time, total_transactions, total_events, status,
		error_count, success_rate, start_time FROM processing_progress`)
	if err != nil {
		return nil, fmt.Errorf("db: GetAllProgress: %w", err)
	}
	defer rows.Close()

	result := make(map[types.ChainType]*types.ProcessProgress)
	for rows.Next() {
		var (
			ct          string
			lastBlock   int64
			lastUpdate  time.Time
			totalTxs    int64
			totalEvents int64
			status      string
			errorCount  int64
			successRate float64
			startTime   time.Time
		)
		if err := rows.Scan(&ct, &lastBlock, &lastUpdate, &totalTxs,
			&totalEvents, &status, &errorCount, &successRate, &startTime); err != nil {
			continue
		}
		chainType := types.ChainType(ct)
		result[chainType] = &types.ProcessProgress{
			ChainType:          chainType,
			LastProcessedBlock: big.NewInt(lastBlock),
			LastUpdateTime:     lastUpdate,
			TotalTransactions:  totalTxs,
			TotalEvents:        totalEvents,
			ProcessingStatus:   types.ProcessingStatus(status),
			ErrorCount:         errorCount,
			SuccessRate:        successRate,
			StartTime:          startTime,
		}
	}
	return result, rows.Err()
}

func (d *DBProgressTracker) UpdateMultipleProgress(progresses map[types.ChainType]*types.ProcessProgress) error {
	ctx := context.Background()
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("db: UpdateMultipleProgress begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	stmt, err := tx.PrepareContext(ctx, d.progressUpsertSQL())
	if err != nil {
		return fmt.Errorf("db: UpdateMultipleProgress prepare: %w", err)
	}
	defer stmt.Close()

	for chainType, progress := range progresses {
		if progress == nil {
			continue
		}
		progress.LastUpdateTime = time.Now()
		progress.ChainType = chainType
		if progress.TotalTransactions > 0 {
			successful := progress.TotalTransactions - progress.ErrorCount
			progress.SuccessRate = float64(successful) / float64(progress.TotalTransactions) * 100
		}
		var lastBlock int64
		if progress.LastProcessedBlock != nil {
			lastBlock = progress.LastProcessedBlock.Int64()
		}
		extra, _ := json.Marshal(map[string]interface{}{"last_error_time": progress.LastErrorTime})
		if _, err := stmt.ExecContext(ctx,
			string(chainType), lastBlock, progress.LastUpdateTime,
			progress.TotalTransactions, progress.TotalEvents,
			string(progress.ProcessingStatus), progress.ErrorCount,
			progress.SuccessRate, progress.StartTime, string(extra),
		); err != nil {
			return fmt.Errorf("db: UpdateMultipleProgress upsert %s: %w", chainType, err)
		}
	}
	return tx.Commit()
}

func (d *DBProgressTracker) SetProcessingStatus(chainType types.ChainType, status types.ProcessingStatus) error {
	p, err := d.GetProgress(chainType)
	if err != nil {
		return err
	}
	p.ProcessingStatus = status
	return d.UpdateProgress(chainType, p)
}

func (d *DBProgressTracker) GetProcessingStatus(chainType types.ChainType) (types.ProcessingStatus, error) {
	p, err := d.GetProgress(chainType)
	if err != nil {
		return types.ProcessingStatusIdle, err
	}
	return p.ProcessingStatus, nil
}

// ── processing_stats (derived from progress row) ────────────────────────────

func (d *DBProgressTracker) GetProcessingStats(chainType types.ChainType) (*types.ProcessingStats, error) {
	p, err := d.GetProgress(chainType)
	if err != nil {
		return nil, err
	}
	return &types.ProcessingStats{
		ChainType: chainType,
		ErrorRate: 100 - p.SuccessRate,
	}, nil
}

func (d *DBProgressTracker) GetGlobalStats() (*types.GlobalProcessingStats, error) {
	all, err := d.GetAllProgress()
	if err != nil {
		return nil, err
	}
	gs := &types.GlobalProcessingStats{
		TotalChains:    len(all),
		ChainStats:     make(map[types.ChainType]*types.ProcessingStats),
		LastUpdateTime: time.Now(),
	}
	for ct, p := range all {
		gs.TotalTransactions += p.TotalTransactions
		gs.TotalEvents += p.TotalEvents
		if p.ProcessingStatus == types.ProcessingStatusRunning {
			gs.ActiveChains++
		}
		gs.ChainStats[ct] = &types.ProcessingStats{
			ChainType: ct,
			ErrorRate: 100 - p.SuccessRate,
		}
	}
	if gs.TotalChains > 0 {
		var totalRate float64
		for _, p := range all {
			totalRate += p.SuccessRate
		}
		gs.OverallSuccessRate = totalRate / float64(gs.TotalChains)
	}
	return gs, nil
}

// ── processing_errors ───────────────────────────────────────────────────────

func (d *DBProgressTracker) RecordError(chainType types.ChainType, recErr error) error {
	if recErr == nil {
		return nil
	}
	ctx := context.Background()

	// Insert error record.
	var q string
	if d.dialect == DialectPostgres {
		q = `INSERT INTO processing_errors (chain_type, error_time, error_type, error_msg)
			 VALUES ($1,$2,$3,$4)`
	} else {
		q = `INSERT INTO processing_errors (chain_type, error_time, error_type, error_msg)
			 VALUES (?,?,?,?)`
	}
	if _, err := d.db.ExecContext(ctx, q,
		string(chainType), time.Now(),
		fmt.Sprintf("%T", recErr), recErr.Error(),
	); err != nil {
		return fmt.Errorf("db: RecordError insert: %w", err)
	}

	// Cap history length by deleting oldest rows beyond maxErrorHistory.
	d.trimTable(ctx, "processing_errors", "error_time", chainType, d.maxErrorHistory)

	// Increment error_count in progress row.
	p, err := d.GetProgress(chainType)
	if err == nil {
		p.ErrorCount++
		p.LastErrorTime = time.Now()
		_ = d.UpdateProgress(chainType, p)
	}
	return nil
}

func (d *DBProgressTracker) GetErrorHistory(chainType types.ChainType, limit int) ([]types.ProcessingError, error) {
	ctx := context.Background()
	var q string
	if limit <= 0 {
		limit = d.maxErrorHistory
	}
	if d.dialect == DialectPostgres {
		q = `SELECT chain_type, error_time, error_type, error_msg
			 FROM processing_errors WHERE chain_type = $1
			 ORDER BY error_time DESC LIMIT $2`
	} else {
		q = `SELECT chain_type, error_time, error_type, error_msg
			 FROM processing_errors WHERE chain_type = ?
			 ORDER BY error_time DESC LIMIT ?`
	}
	rows, err := d.db.QueryContext(ctx, q, string(chainType), limit)
	if err != nil {
		return nil, fmt.Errorf("db: GetErrorHistory: %w", err)
	}
	defer rows.Close()

	var result []types.ProcessingError
	for rows.Next() {
		var (
			ct      string
			errTime time.Time
			errType string
			errMsg  string
		)
		if err := rows.Scan(&ct, &errTime, &errType, &errMsg); err != nil {
			continue
		}
		result = append(result, types.ProcessingError{
			ChainType: types.ChainType(ct),
			ErrorTime: errTime,
			ErrorType: errType,
			ErrorMsg:  errMsg,
		})
	}
	return result, rows.Err()
}

func (d *DBProgressTracker) ClearErrorHistory(chainType types.ChainType) error {
	ctx := context.Background()
	q := fmt.Sprintf("DELETE FROM processing_errors WHERE chain_type = %s", d.ph(1))
	if _, err := d.db.ExecContext(ctx, q, string(chainType)); err != nil {
		return fmt.Errorf("db: ClearErrorHistory: %w", err)
	}
	// Reset error_count in progress row.
	p, err := d.GetProgress(chainType)
	if err == nil {
		p.ErrorCount = 0
		p.LastErrorTime = time.Time{}
		_ = d.UpdateProgress(chainType, p)
	}
	return nil
}

// ── processing_metrics ──────────────────────────────────────────────────────

func (d *DBProgressTracker) RecordProcessingMetrics(chainType types.ChainType, metrics *types.ProcessingMetrics) error {
	if metrics == nil {
		return fmt.Errorf("db: metrics must not be nil")
	}
	ctx := context.Background()
	metrics.Timestamp = time.Now()
	metrics.ChainType = chainType

	var blockNum int64
	if metrics.BlockNumber != nil {
		blockNum = metrics.BlockNumber.Int64()
	}

	var q string
	if d.dialect == DialectPostgres {
		q = `INSERT INTO processing_metrics
			(chain_type, timestamp, block_number, processing_time_ns,
			 transaction_count, event_count, memory_usage, cpu_usage)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`
	} else {
		q = `INSERT INTO processing_metrics
			(chain_type, timestamp, block_number, processing_time_ns,
			 transaction_count, event_count, memory_usage, cpu_usage)
			VALUES (?,?,?,?,?,?,?,?)`
	}
	if _, err := d.db.ExecContext(ctx, q,
		string(chainType), metrics.Timestamp, blockNum,
		metrics.ProcessingTime.Nanoseconds(),
		metrics.TransactionCount, metrics.EventCount,
		metrics.MemoryUsage, metrics.CPUUsage,
	); err != nil {
		return fmt.Errorf("db: RecordProcessingMetrics insert: %w", err)
	}

	// Cap history length.
	d.trimTable(ctx, "processing_metrics", "timestamp", chainType, d.maxMetricsHistory)
	return nil
}

func (d *DBProgressTracker) GetPerformanceMetrics(chainType types.ChainType, duration time.Duration) (*types.PerformanceReport, error) {
	ctx := context.Background()
	cutoff := time.Now().Add(-duration)

	var q string
	if d.dialect == DialectPostgres {
		q = `SELECT processing_time_ns, transaction_count, event_count,
			 memory_usage, cpu_usage
			 FROM processing_metrics
			 WHERE chain_type = $1 AND timestamp >= $2
			 ORDER BY timestamp DESC`
	} else {
		q = `SELECT processing_time_ns, transaction_count, event_count,
			 memory_usage, cpu_usage
			 FROM processing_metrics
			 WHERE chain_type = ? AND timestamp >= ?
			 ORDER BY timestamp DESC`
	}

	rows, err := d.db.QueryContext(ctx, q, string(chainType), cutoff)
	if err != nil {
		return nil, fmt.Errorf("db: GetPerformanceMetrics: %w", err)
	}
	defer rows.Close()

	report := &types.PerformanceReport{
		ChainType:    chainType,
		ReportPeriod: duration,
	}

	var (
		count       int64
		totalTimeNs int64
		totalTxs    int64
		totalEvents int64
		totalMemory int64
		totalCPU    float64
		maxTimeNs   int64
		minTimeNs   int64 = -1
	)

	for rows.Next() {
		var (
			timeNs  int64
			txCount int
			evCount int
			mem     int64
			cpu     float64
		)
		if err := rows.Scan(&timeNs, &txCount, &evCount, &mem, &cpu); err != nil {
			continue
		}
		count++
		totalTimeNs += timeNs
		totalTxs += int64(txCount)
		totalEvents += int64(evCount)
		totalMemory += mem
		totalCPU += cpu
		if timeNs > maxTimeNs {
			maxTimeNs = timeNs
		}
		if minTimeNs < 0 || timeNs < minTimeNs {
			minTimeNs = timeNs
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if count > 0 {
		report.AverageProcessTime = time.Duration(totalTimeNs / count)
		report.MaxProcessTime = time.Duration(maxTimeNs)
		report.MinProcessTime = time.Duration(minTimeNs)
		report.TotalTransactions = totalTxs
		report.TotalEvents = totalEvents
		report.AverageMemoryUsage = totalMemory / count
		report.AverageCPUUsage = totalCPU / float64(count)
		if duration > 0 {
			report.ThroughputTPS = float64(totalTxs) / duration.Seconds()
		}
	}

	// Attach error count from error history table.
	errRows, err := d.GetErrorHistory(chainType, 0)
	if err == nil {
		for _, e := range errRows {
			if e.ErrorTime.After(cutoff) {
				report.ErrorCount++
			}
		}
	}
	return report, nil
}

// ── health check & cleanup ──────────────────────────────────────────────────

func (d *DBProgressTracker) HealthCheck() error {
	ctx := context.Background()
	if err := d.db.PingContext(ctx); err != nil {
		return fmt.Errorf("db: HealthCheck ping: %w", err)
	}
	return nil
}

func (d *DBProgressTracker) Cleanup(olderThan time.Duration) error {
	ctx := context.Background()
	cutoff := time.Now().Add(-olderThan)

	all, err := d.GetAllProgress()
	if err != nil {
		return fmt.Errorf("db: Cleanup GetAllProgress: %w", err)
	}

	for chainType := range all {
		var q string
		if d.dialect == DialectPostgres {
			q = "DELETE FROM processing_errors WHERE chain_type = $1 AND error_time < $2"
		} else {
			q = "DELETE FROM processing_errors WHERE chain_type = ? AND error_time < ?"
		}
		if _, err := d.db.ExecContext(ctx, q, string(chainType), cutoff); err != nil {
			return fmt.Errorf("db: Cleanup errors %s: %w", chainType, err)
		}

		if d.dialect == DialectPostgres {
			q = "DELETE FROM processing_metrics WHERE chain_type = $1 AND timestamp < $2"
		} else {
			q = "DELETE FROM processing_metrics WHERE chain_type = ? AND timestamp < ?"
		}
		if _, err := d.db.ExecContext(ctx, q, string(chainType), cutoff); err != nil {
			return fmt.Errorf("db: Cleanup metrics %s: %w", chainType, err)
		}
	}
	return nil
}

// ── internal helpers ────────────────────────────────────────────────────────

// trimTable deletes the oldest rows beyond maxRows for the given chain,
// keeping the most recent maxRows entries ordered by timeCol.
func (d *DBProgressTracker) trimTable(ctx context.Context, table, timeCol string, chainType types.ChainType, maxRows int) {
	var q string
	if d.dialect == DialectPostgres {
		q = fmt.Sprintf(`DELETE FROM %s WHERE chain_type = $1
			AND %s < (
				SELECT %s FROM %s WHERE chain_type = $2
				ORDER BY %s DESC LIMIT 1 OFFSET %d
			)`, table, timeCol, timeCol, table, timeCol, maxRows-1)
	} else {
		q = fmt.Sprintf(`DELETE FROM %s WHERE chain_type = ?
			AND %s < (
				SELECT t.%s FROM (
					SELECT %s FROM %s WHERE chain_type = ?
					ORDER BY %s DESC LIMIT 1 OFFSET %d
				) t
			)`, table, timeCol, timeCol, timeCol, table, timeCol, maxRows-1)
	}
	// Trim is best-effort; ignore errors.
	if d.dialect == DialectPostgres {
		d.db.ExecContext(ctx, q, string(chainType), string(chainType))
	} else {
		d.db.ExecContext(ctx, q, string(chainType), string(chainType))
	}
}
