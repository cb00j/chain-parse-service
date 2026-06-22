// Package app provides shared application initialization helpers
// used by both the parser and API entry points.
package app

import (
	"context"
	"fmt"

	"unified-tx-parser/internal/config"
	"unified-tx-parser/internal/storage/influxdb"
	"unified-tx-parser/internal/storage/mysql"
	"unified-tx-parser/internal/storage/pgsql"
	redisTracker "unified-tx-parser/internal/storage/redis"
	"unified-tx-parser/internal/types"

	"github.com/redis/go-redis/v9"
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

// CreateProgressTracker creates a Redis-backed progress tracker.
// Returns the tracker, the underlying Redis client (for cleanup), and any error.
func CreateProgressTracker(cfg *config.Config) (types.ProgressTracker, *redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:       fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port),
		Password:   cfg.Redis.Password,
		DB:         cfg.Redis.DB,
		PoolSize:   cfg.Redis.PoolSize,
		MaxRetries: cfg.Redis.MaxRetries,
	})

	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, nil, fmt.Errorf("redis connection failed: %w", err)
	}

	tracker := redisTracker.NewRedisProgressTracker(client, "unified_tx_parser")
	return tracker, client, nil
}
