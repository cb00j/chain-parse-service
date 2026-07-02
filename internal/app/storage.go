// Package app provides shared application initialization helpers
// used by both the parser and API entry points.
package app

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"unified-tx-parser/internal/config"
	cursorStore "unified-tx-parser/internal/storage/cursor"
	"unified-tx-parser/internal/storage/influxdb"
	"unified-tx-parser/internal/storage/mysql"
	"unified-tx-parser/internal/storage/pgsql"
	dbTracker "unified-tx-parser/internal/storage/progress/db"
	fallbackTracker "unified-tx-parser/internal/storage/progress/fallback"
	redisTracker "unified-tx-parser/internal/storage/progress/redis"
	"unified-tx-parser/internal/types"

	_ "github.com/go-sql-driver/mysql"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	log "github.com/sirupsen/logrus"
)

// CreateStorageEngine creates a storage engine based on the config storage type.
func CreateStorageEngine(cfg *config.Config) (types.StorageEngine, error) {
	switch cfg.Storage.Type {
	case "mysql":
		mysqlCfg := &mysql.MySQLConfig{
			Host:         cfg.Storage.MySQL.Host,
			Port:         cfg.Storage.MySQL.Port,
			Username:     cfg.Storage.MySQL.Username,
			Password:     cfg.Storage.MySQL.Password,
			Database:     cfg.Storage.MySQL.Database,
			MaxOpenConns: cfg.Storage.MySQL.MaxOpenConns,
			MaxIdleConns: cfg.Storage.MySQL.MaxIdleConns,
		}
		return mysql.NewMySQLStore(mysqlCfg)

	case "influxdb":
		influxCfg := &influxdb.InfluxDBConfig{
			URL:       cfg.Storage.InfluxDB.URL,
			Token:     cfg.Storage.InfluxDB.Token,
			Org:       cfg.Storage.InfluxDB.Org,
			Bucket:    cfg.Storage.InfluxDB.Bucket,
			BatchSize: cfg.Storage.InfluxDB.BatchSize,
			FlushTime: cfg.Storage.InfluxDB.FlushTime,
			Precision: cfg.Storage.InfluxDB.Precision,
		}
		return influxdb.NewInfluxDBStorage(influxCfg)

	case "pgsql":
		pgCfg := &pgsql.PgSQLConfig{
			Host:            cfg.Storage.PgSQL.Host,
			Port:            cfg.Storage.PgSQL.Port,
			Username:        cfg.Storage.PgSQL.Username,
			Password:        cfg.Storage.PgSQL.Password,
			Database:        cfg.Storage.PgSQL.Database,
			SSLMode:         cfg.Storage.PgSQL.SSLMode,
			MaxOpenConns:    cfg.Storage.PgSQL.MaxOpenConns,
			MaxIdleConns:    cfg.Storage.PgSQL.MaxIdleConns,
			ConnMaxLifetime: cfg.Storage.PgSQL.ConnMaxLifetime,
		}
		return pgsql.NewPgSQLStore(pgCfg)

	default:
		return nil, fmt.Errorf("unsupported storage type: %s", cfg.Storage.Type)
	}
}

// CreateRedisClient builds a *redis.Client from cfg.Redis without any of
// the progress-tracker machinery CreateProgressTracker pulls in (DB
// fallback tracker, health-check goroutine, etc.) — for callers that just
// need a plain Redis connection, e.g. cmd/thegraph-sync wiring one into
// dexcache. Does not ping/verify connectivity; callers that care should
// call client.Ping themselves (mirroring CreateProgressTracker's own
// "Redis is optional, degrade gracefully" stance — a nil-safe caller
// doesn't need this to fail loudly here).
func CreateRedisClient(cfg *config.Config) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:       fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port),
		Password:   cfg.Redis.Password,
		DB:         cfg.Redis.DB,
		PoolSize:   cfg.Redis.PoolSize,
		MaxRetries: cfg.Redis.MaxRetries,
	})
}

// CreateProgressTracker creates a FallbackProgressTracker:
//   - primary:   Redis (fast, real-time)
//   - secondary: DB    (durable, synced every syncInterval batches)
//
// If Redis is unreachable at startup the system continues in DB-only mode.
// The returned *redis.Client may be nil if Redis fails — caller must handle this.
func CreateProgressTracker(cfg *config.Config) (types.ProgressTracker, *redis.Client, error) {
	// ── 1. DB tracker (mandatory — durable fallback) ──────────────────────────
	secondary, err := createDBProgressTracker(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create DB progress tracker: %w", err)
	}

	// ── 2. Redis tracker (optional — best-effort primary) ────────────────────
	redisClient := redis.NewClient(&redis.Options{
		Addr:       fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port),
		Password:   cfg.Redis.Password,
		DB:         cfg.Redis.DB,
		PoolSize:   cfg.Redis.PoolSize,
		MaxRetries: cfg.Redis.MaxRetries,
	})

	primary := redisTracker.NewRedisProgressTracker(redisClient, "unified_tx_parser")

	redisOK := redisClient.Ping(context.Background()).Err() == nil
	if !redisOK {
		log.Warn("[progress] Redis unavailable at startup — running in DB-only mode")
	}

	// ── 3. Wrap in FallbackProgressTracker ───────────────────────────────────
	// syncInterval=10: DB checkpoint written every 10 UpdateProgress calls.
	tracker := fallbackTracker.NewFallbackProgressTracker(primary, secondary, 10)
	if !redisOK {
		tracker.MarkPrimaryUnhealthy()
	}

	// Background health check: restore Redis primary when it recovers.
	done := make(chan struct{})
	tracker.StartHealthCheck(30*time.Second, done)

	return tracker, redisClient, nil
}

// CreateCursorStore creates a types.CursorStore appropriate for the
// configured storage backend:
//   - mysql/pgsql: DBCursorStore, using its own small dedicated *sql.DB
//     rather than sharing StorageEngine's connection (same approach as
//     createDBProgressTracker).
//   - influxdb: InfluxCursorStore, using its own influxdb2.Client (health
//     checked the same way NewSimpleInfluxDBStorage checks the main
//     storage engine's connection).
//
// All three backends persist cursors durably — there's no no-op fallback
// left; every supported storage.type gives incremental sync jobs (like
// internal/thegraph.Syncer) a real place to store progress.
func CreateCursorStore(cfg *config.Config) (types.CursorStore, error) {
	switch cfg.Storage.Type {
	case "mysql":
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true",
			cfg.Storage.MySQL.Username,
			cfg.Storage.MySQL.Password,
			cfg.Storage.MySQL.Host,
			cfg.Storage.MySQL.Port,
			cfg.Storage.MySQL.Database,
		)
		db, err := sql.Open("mysql", dsn)
		if err != nil {
			return nil, fmt.Errorf("open mysql for cursor store: %w", err)
		}
		db.SetMaxOpenConns(5)
		db.SetMaxIdleConns(2)
		if err := db.Ping(); err != nil {
			return nil, fmt.Errorf("ping mysql for cursor store: %w", err)
		}
		return cursorStore.NewDBCursorStore(db, cursorStore.DialectMySQL), nil

	case "pgsql":
		dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
			cfg.Storage.PgSQL.Host,
			cfg.Storage.PgSQL.Port,
			cfg.Storage.PgSQL.Username,
			cfg.Storage.PgSQL.Password,
			cfg.Storage.PgSQL.Database,
		)
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			return nil, fmt.Errorf("open pgsql for cursor store: %w", err)
		}
		db.SetMaxOpenConns(5)
		db.SetMaxIdleConns(2)
		if err := db.Ping(); err != nil {
			return nil, fmt.Errorf("ping pgsql for cursor store: %w", err)
		}
		return cursorStore.NewDBCursorStore(db, cursorStore.DialectPostgres), nil

	case "influxdb":
		client := influxdb2.NewClient(cfg.Storage.InfluxDB.URL, cfg.Storage.InfluxDB.Token)
		health, err := client.Health(context.Background())
		if err != nil {
			return nil, fmt.Errorf("connect influxdb for cursor store: %w", err)
		}
		if health.Status != "pass" {
			client.Close()
			msg := ""
			if health.Message != nil {
				msg = *health.Message
			}
			return nil, fmt.Errorf("influxdb health check failed for cursor store: %s", msg)
		}
		return cursorStore.NewInfluxCursorStore(client, cfg.Storage.InfluxDB.Org, cfg.Storage.InfluxDB.Bucket), nil

	default:
		return nil, fmt.Errorf("unsupported storage type for cursor store: %s", cfg.Storage.Type)
	}
}

// createDBProgressTracker opens a dedicated *sql.DB for the progress tracker
// using the same DSN as the main storage engine.
func createDBProgressTracker(cfg *config.Config) (types.ProgressTracker, error) {
	switch cfg.Storage.Type {
	case "mysql":
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true",
			cfg.Storage.MySQL.Username,
			cfg.Storage.MySQL.Password,
			cfg.Storage.MySQL.Host,
			cfg.Storage.MySQL.Port,
			cfg.Storage.MySQL.Database,
		)
		db, err := sql.Open("mysql", dsn)
		if err != nil {
			return nil, fmt.Errorf("open mysql for progress tracker: %w", err)
		}
		db.SetMaxOpenConns(5)
		db.SetMaxIdleConns(2)
		if err := db.Ping(); err != nil {
			return nil, fmt.Errorf("ping mysql for progress tracker: %w", err)
		}
		return dbTracker.NewDBProgressTracker(db, dbTracker.DialectMySQL), nil

	case "pgsql":
		dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
			cfg.Storage.PgSQL.Host,
			cfg.Storage.PgSQL.Port,
			cfg.Storage.PgSQL.Username,
			cfg.Storage.PgSQL.Password,
			cfg.Storage.PgSQL.Database,
		)
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			return nil, fmt.Errorf("open pgsql for progress tracker: %w", err)
		}
		db.SetMaxOpenConns(5)
		db.SetMaxIdleConns(2)
		if err := db.Ping(); err != nil {
			return nil, fmt.Errorf("ping pgsql for progress tracker: %w", err)
		}
		return dbTracker.NewDBProgressTracker(db, dbTracker.DialectPostgres), nil

	case "influxdb":
		// InfluxDB has no relational tables; DB progress unavailable.
		// Progress lives in Redis only (existing behaviour).
		log.Warn("[progress] storage.type=influxdb: no DB fallback, Redis is the only progress store")
		return &noopProgressTracker{}, nil

	default:
		return nil, fmt.Errorf("unsupported storage type for progress tracker: %s", cfg.Storage.Type)
	}
}

// noopProgressTracker is a do-nothing fallback when DB progress tracking is
// unavailable (e.g. when storage.type=influxdb).
type noopProgressTracker struct{}

func (n *noopProgressTracker) GetProgress(_ types.ChainType) (*types.ProcessProgress, error) {
	return &types.ProcessProgress{}, nil
}
func (n *noopProgressTracker) UpdateProgress(_ types.ChainType, _ *types.ProcessProgress) error {
	return nil
}
func (n *noopProgressTracker) ResetProgress(_ types.ChainType) error { return nil }
func (n *noopProgressTracker) GetAllProgress() (map[types.ChainType]*types.ProcessProgress, error) {
	return map[types.ChainType]*types.ProcessProgress{}, nil
}
func (n *noopProgressTracker) UpdateMultipleProgress(_ map[types.ChainType]*types.ProcessProgress) error {
	return nil
}
func (n *noopProgressTracker) GetProcessingStats(_ types.ChainType) (*types.ProcessingStats, error) {
	return &types.ProcessingStats{}, nil
}
func (n *noopProgressTracker) GetGlobalStats() (*types.GlobalProcessingStats, error) {
	return &types.GlobalProcessingStats{}, nil
}
func (n *noopProgressTracker) SetProcessingStatus(_ types.ChainType, _ types.ProcessingStatus) error {
	return nil
}
func (n *noopProgressTracker) GetProcessingStatus(_ types.ChainType) (types.ProcessingStatus, error) {
	return types.ProcessingStatusIdle, nil
}
func (n *noopProgressTracker) RecordError(_ types.ChainType, _ error) error { return nil }
func (n *noopProgressTracker) GetErrorHistory(_ types.ChainType, _ int) ([]types.ProcessingError, error) {
	return nil, nil
}
func (n *noopProgressTracker) ClearErrorHistory(_ types.ChainType) error { return nil }
func (n *noopProgressTracker) RecordProcessingMetrics(_ types.ChainType, _ *types.ProcessingMetrics) error {
	return nil
}
func (n *noopProgressTracker) GetPerformanceMetrics(_ types.ChainType, _ time.Duration) (*types.PerformanceReport, error) {
	return &types.PerformanceReport{}, nil
}
func (n *noopProgressTracker) HealthCheck() error            { return nil }
func (n *noopProgressTracker) Cleanup(_ time.Duration) error { return nil }
