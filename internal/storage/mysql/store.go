package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	apperr "unified-tx-parser/internal/errors"
	"unified-tx-parser/internal/model"
	"unified-tx-parser/internal/types"
	"unified-tx-parser/internal/utils"

	_ "github.com/go-sql-driver/mysql"
	"github.com/sirupsen/logrus"
)

var log = logrus.WithFields(logrus.Fields{"service": "parser", "module": "storage-mysql"})

// batchChunkSize controls the max rows per multi-row INSERT statement.
const batchChunkSize = 500

// MySQLStore MySQL存储引擎
type MySQLStore struct {
	db     *sql.DB
	config *MySQLConfig
}

// MySQLConfig MySQL配置
type MySQLConfig struct {
	Host         string `json:"host"`
	Port         int    `json:"port"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	Database     string `json:"database"`
	MaxOpenConns int    `json:"max_open_conns"`
	MaxIdleConns int    `json:"max_idle_conns"`
}

// NewMySQLStore 创建MySQL存储引擎
func NewMySQLStore(config *MySQLConfig) (*MySQLStore, error) {
	if config == nil {
		config = &MySQLConfig{
			Host:         "localhost",
			Port:         3306,
			Username:     "root",
			Password:     "password",
			Database:     "unified_tx_parser",
			MaxOpenConns: 100,
			MaxIdleConns: 10,
		}
	}

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		config.Username, config.Password, config.Host, config.Port, config.Database)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, apperr.NewStorageError(apperr.StoreConnectionError, "connecting to MySQL", err).WithOperation("NewMySQLStore")
	}

	db.SetMaxOpenConns(config.MaxOpenConns)
	db.SetMaxIdleConns(config.MaxIdleConns)
	db.SetConnMaxLifetime(time.Hour)

	store := &MySQLStore{
		db:     db,
		config: config,
	}

	if err := store.HealthCheck(context.Background()); err != nil {
		return nil, apperr.NewStorageError(apperr.StoreConnectionError, "MySQL health check failed", err).WithOperation("NewMySQLStore")
	}

	log.Infof("mysql storage engine initialized: %s:%d/%s", config.Host, config.Port, config.Database)
	return store, nil
}

// StoreBlocks stores unified blocks into MySQL.
func (m *MySQLStore) StoreBlocks(ctx context.Context, blocks []types.UnifiedBlock) error {
	if len(blocks) == 0 {
		return nil
	}

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return apperr.NewStorageError(apperr.StoreTransactionErr, "begin tx", err).WithOperation("mysql")
	}
	defer tx.Rollback()

	for i := 0; i < len(blocks); i += batchChunkSize {
		end := i + batchChunkSize
		if end > len(blocks) {
			end = len(blocks)
		}
		if err := m.batchInsertBlocks(ctx, tx, blocks[i:end]); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return apperr.NewStorageError(apperr.StoreTransactionErr, "commit blocks", err).WithOperation("StoreBlocks")
	}

	log.Infof("stored %d blocks", len(blocks))
	return nil
}

func (m *MySQLStore) batchInsertBlocks(ctx context.Context, tx *sql.Tx, blocks []types.UnifiedBlock) error {
	if len(blocks) == 0 {
		return nil
	}

	var b strings.Builder
	b.WriteString(`INSERT INTO blocks (block_number, block_hash, chain_type, chain_id, parent_hash,
		timestamp, tx_count, size) VALUES `)

	args := make([]interface{}, 0, len(blocks)*8)
	for i, block := range blocks {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("(?,?,?,?,?,?,?,?)")
		args = append(args,
			utils.BigIntToNullInt64(block.BlockNumber),
			block.BlockHash,
			string(block.ChainType),
			block.ChainID,
			block.ParentHash,
			block.Timestamp,
			block.TxCount,
			block.Size,
		)
	}
	b.WriteString(` ON DUPLICATE KEY UPDATE
		block_number=VALUES(block_number), tx_count=VALUES(tx_count),
		size=VALUES(size), updated_at=CURRENT_TIMESTAMP`)

	_, err := tx.ExecContext(ctx, b.String(), args...)
	if err != nil {
		return apperr.NewStorageError(apperr.StoreInsertError, "batch insert blocks", err).WithOperation("StoreBlocks")
	}
	return nil
}

// StoreTransactions stores unified transactions using batch multi-row INSERT.
func (m *MySQLStore) StoreTransactions(ctx context.Context, txs []types.UnifiedTransaction) error {
	if len(txs) == 0 {
		return nil
	}

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return apperr.NewStorageError(apperr.StoreTransactionErr, "begin tx", err).WithOperation("mysql")
	}
	defer tx.Rollback()

	for i := 0; i < len(txs); i += batchChunkSize {
		end := i + batchChunkSize
		if end > len(txs) {
			end = len(txs)
		}
		if err := m.batchInsertTransactions(ctx, tx, txs[i:end]); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return apperr.NewStorageError(apperr.StoreTransactionErr, "commit transactions", err).WithOperation("StoreTransactions")
	}

	log.Infof("stored %d transactions", len(txs))
	return nil
}

func (m *MySQLStore) batchInsertTransactions(ctx context.Context, tx *sql.Tx, txs []types.UnifiedTransaction) error {
	if len(txs) == 0 {
		return nil
	}

	var b strings.Builder
	b.WriteString(`INSERT INTO transactions (
		tx_hash, chain_type, chain_id, block_number, block_hash, tx_index,
		from_address, to_address, value, gas_limit, gas_used, gas_price,
		status, timestamp, raw_data
	) VALUES `)

	args := make([]interface{}, 0, len(txs)*15)
	for i, transaction := range txs {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)")

		rawDataJSON, _ := utils.ToJSON(transaction.RawData)
		args = append(args,
			transaction.TxHash,
			string(transaction.ChainType),
			transaction.ChainID,
			utils.BigIntToNullInt64(transaction.BlockNumber),
			utils.StringToNullString(transaction.BlockHash),
			utils.IntToNullInt(transaction.TxIndex),
			utils.StringToNullString(transaction.FromAddress),
			utils.StringToNullString(transaction.ToAddress),
			utils.BigIntToNullString(transaction.Value),
			utils.BigIntToNullInt64(transaction.GasLimit),
			utils.BigIntToNullInt64(transaction.GasUsed),
			utils.BigIntToNullInt64(transaction.GasPrice),
			string(transaction.Status),
			transaction.Timestamp,
			rawDataJSON,
		)
	}

	b.WriteString(` ON DUPLICATE KEY UPDATE
		block_number = VALUES(block_number),
		block_hash = VALUES(block_hash),
		tx_index = VALUES(tx_index),
		from_address = VALUES(from_address),
		to_address = VALUES(to_address),
		value = VALUES(value),
		gas_limit = VALUES(gas_limit),
		gas_used = VALUES(gas_used),
		gas_price = VALUES(gas_price),
		status = VALUES(status),
		timestamp = VALUES(timestamp),
		raw_data = VALUES(raw_data),
		updated_at = CURRENT_TIMESTAMP`)

	_, err := tx.ExecContext(ctx, b.String(), args...)
	if err != nil {
		return apperr.NewStorageError(apperr.StoreInsertError, "batch insert transactions", err).WithOperation("StoreTransactions")
	}
	return nil
}

// StoreDexData stores all DEX data types atomically in a single transaction.
func (m *MySQLStore) StoreDexData(ctx context.Context, dexData *types.DexData) error {
	if dexData == nil {
		return nil
	}

	dbTx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return apperr.NewStorageError(apperr.StoreTransactionErr, "begin tx", err).WithOperation("mysql")
	}
	defer dbTx.Rollback()

	if err := m.batchUpsertPools(ctx, dbTx, dexData.Pools); err != nil {
		return apperr.NewStorageError(apperr.StoreInsertError, "batch upsert pools", err).WithOperation("StoreDexData")
	}
	if err := m.batchUpsertTokens(ctx, dbTx, dexData.Tokens); err != nil {
		return apperr.NewStorageError(apperr.StoreInsertError, "batch upsert tokens", err).WithOperation("StoreDexData")
	}
	if err := m.batchInsertDexTransactions(ctx, dbTx, dexData.Transactions); err != nil {
		return apperr.NewStorageError(apperr.StoreInsertError, "batch insert dex transactions", err).WithOperation("StoreDexData")
	}
	if err := m.batchInsertLiquidities(ctx, dbTx, dexData.Liquidities); err != nil {
		return apperr.NewStorageError(apperr.StoreInsertError, "batch insert liquidities", err).WithOperation("StoreDexData")
	}
	if err := m.batchUpsertReserves(ctx, dbTx, dexData.Reserves); err != nil {
		return apperr.NewStorageError(apperr.StoreInsertError, "batch upsert reserves", err).WithOperation("StoreDexData")
	}

	if err := dbTx.Commit(); err != nil {
		return apperr.NewStorageError(apperr.StoreTransactionErr, "commit dex data", err).WithOperation("StoreDexData")
	}

	log.Infof("stored dex data: pools=%d, tokens=%d, txs=%d, liquidities=%d, reserves=%d",
		len(dexData.Pools), len(dexData.Tokens), len(dexData.Transactions),
		len(dexData.Liquidities), len(dexData.Reserves))
	return nil
}

func (m *MySQLStore) batchUpsertPools(ctx context.Context, tx *sql.Tx, pools []model.Pool) error {
	for i := 0; i < len(pools); i += batchChunkSize {
		end := i + batchChunkSize
		if end > len(pools) {
			end = len(pools)
		}
		if err := m.batchUpsertPoolsChunk(ctx, tx, pools[i:end]); err != nil {
			return err
		}
	}
	return nil
}

func (m *MySQLStore) batchUpsertPoolsChunk(ctx context.Context, tx *sql.Tx, pools []model.Pool) error {
	if len(pools) == 0 {
		return nil
	}

	var b strings.Builder
	b.WriteString(`INSERT INTO dex_pools (addr, factory, protocol, token0, token1, fee, extra) VALUES `)

	args := make([]interface{}, 0, len(pools)*7)
	for i, pool := range pools {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("(?,?,?,?,?,?,?)")

		token0, token1 := "", ""
		if v, ok := pool.Tokens[0]; ok {
			token0 = v
		}
		if v, ok := pool.Tokens[1]; ok {
			token1 = v
		}
		var extraJSON *string
		if pool.Extra != nil {
			if s, e := utils.ToJSON(pool.Extra); e == nil {
				extraJSON = &s
			}
		}
		args = append(args, pool.Addr, pool.Factory, pool.Protocol, token0, token1, pool.Fee, extraJSON)
	}

	b.WriteString(` ON DUPLICATE KEY UPDATE
		factory=VALUES(factory), protocol=VALUES(protocol),
		token0=VALUES(token0), token1=VALUES(token1),
		fee=VALUES(fee), extra=COALESCE(VALUES(extra), extra)`)

	_, err := tx.ExecContext(ctx, b.String(), args...)
	return err
}

func (m *MySQLStore) batchUpsertTokens(ctx context.Context, tx *sql.Tx, tokens []model.Token) error {
	for i := 0; i < len(tokens); i += batchChunkSize {
		end := i + batchChunkSize
		if end > len(tokens) {
			end = len(tokens)
		}
		if err := m.batchUpsertTokensChunk(ctx, tx, tokens[i:end]); err != nil {
			return err
		}
	}
	return nil
}

func (m *MySQLStore) batchUpsertTokensChunk(ctx context.Context, tx *sql.Tx, tokens []model.Token) error {
	if len(tokens) == 0 {
		return nil
	}

	var b strings.Builder
	b.WriteString(`INSERT INTO dex_tokens (addr, name, symbol, decimals, is_stable) VALUES `)

	args := make([]interface{}, 0, len(tokens)*5)
	for i, token := range tokens {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("(?,?,?,?,?)")
		args = append(args, token.Addr, token.Name, token.Symbol, token.Decimals, token.IsStable)
	}

	b.WriteString(` ON DUPLICATE KEY UPDATE
		name=VALUES(name), symbol=VALUES(symbol),
		decimals=VALUES(decimals), is_stable=VALUES(is_stable)`)

	_, err := tx.ExecContext(ctx, b.String(), args...)
	return err
}

func (m *MySQLStore) batchInsertDexTransactions(ctx context.Context, tx *sql.Tx, dexTxs []model.Transaction) error {
	for i := 0; i < len(dexTxs); i += batchChunkSize {
		end := i + batchChunkSize
		if end > len(dexTxs) {
			end = len(dexTxs)
		}
		if err := m.batchInsertDexTransactionsChunk(ctx, tx, dexTxs[i:end]); err != nil {
			return err
		}
	}
	return nil
}

func (m *MySQLStore) batchInsertDexTransactionsChunk(ctx context.Context, tx *sql.Tx, dexTxs []model.Transaction) error {
	if len(dexTxs) == 0 {
		return nil
	}

	var b strings.Builder
	b.WriteString(`INSERT IGNORE INTO dex_transactions (addr, router, factory, pool, hash, from_addr, side,
		amount, price, value, time, event_index, tx_index, swap_index, block_number, extra)
		VALUES `)

	args := make([]interface{}, 0, len(dexTxs)*16)
	for i, dt := range dexTxs {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)")

		var extraJSON *string
		if dt.Extra != nil {
			if s, e := utils.ToJSON(dt.Extra); e == nil {
				extraJSON = &s
			}
		}
		args = append(args,
			dt.Addr, dt.Router, dt.Factory, dt.Pool, dt.Hash, dt.From, dt.Side,
			utils.BigIntToNullString(dt.Amount), dt.Price, dt.Value, dt.Time,
			dt.EventIndex, dt.TxIndex, dt.SwapIndex, dt.BlockNumber, extraJSON)
	}

	_, err := tx.ExecContext(ctx, b.String(), args...)
	return err
}

func (m *MySQLStore) batchInsertLiquidities(ctx context.Context, tx *sql.Tx, liqs []model.Liquidity) error {
	for i := 0; i < len(liqs); i += batchChunkSize {
		end := i + batchChunkSize
		if end > len(liqs) {
			end = len(liqs)
		}
		if err := m.batchInsertLiquiditiesChunk(ctx, tx, liqs[i:end]); err != nil {
			return err
		}
	}
	return nil
}

func (m *MySQLStore) batchInsertLiquiditiesChunk(ctx context.Context, tx *sql.Tx, liqs []model.Liquidity) error {
	if len(liqs) == 0 {
		return nil
	}

	var b strings.Builder
	b.WriteString("INSERT IGNORE INTO dex_liquidities (addr, router, factory, pool, hash, from_addr, pos, side, " +
		"amount, value, time, `key`, extra) VALUES ")

	args := make([]interface{}, 0, len(liqs)*13)
	for i, liq := range liqs {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("(?,?,?,?,?,?,?,?,?,?,?,?,?)")

		var extraJSON *string
		if liq.Extra != nil {
			if s, e := utils.ToJSON(liq.Extra); e == nil {
				extraJSON = &s
			}
		}
		args = append(args,
			liq.Addr, liq.Router, liq.Factory, liq.Pool, liq.Hash, liq.From, liq.Pos, liq.Side,
			utils.BigIntToNullString(liq.Amount), liq.Value, liq.Time, liq.Key, extraJSON)
	}

	_, err := tx.ExecContext(ctx, b.String(), args...)
	return err
}

func (m *MySQLStore) batchUpsertReserves(ctx context.Context, tx *sql.Tx, reserves []model.Reserve) error {
	for i := 0; i < len(reserves); i += batchChunkSize {
		end := i + batchChunkSize
		if end > len(reserves) {
			end = len(reserves)
		}
		if err := m.batchUpsertReservesChunk(ctx, tx, reserves[i:end]); err != nil {
			return err
		}
	}
	return nil
}

func (m *MySQLStore) batchUpsertReservesChunk(ctx context.Context, tx *sql.Tx, reserves []model.Reserve) error {
	if len(reserves) == 0 {
		return nil
	}

	var b strings.Builder
	b.WriteString(`INSERT INTO dex_reserves (addr, amount0, amount1, time) VALUES `)

	args := make([]interface{}, 0, len(reserves)*4)
	for i, res := range reserves {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("(?,?,?,?)")

		var a0, a1 string
		if v, ok := res.Amounts[0]; ok && v != nil {
			a0 = v.String()
		}
		if v, ok := res.Amounts[1]; ok && v != nil {
			a1 = v.String()
		}
		args = append(args, res.Addr, a0, a1, res.Time)
	}

	b.WriteString(` ON DUPLICATE KEY UPDATE
		amount0=VALUES(amount0), amount1=VALUES(amount1)`)

	_, err := tx.ExecContext(ctx, b.String(), args...)
	return err
}

// GetTransactionsByHash 根据哈希获取交易
func (m *MySQLStore) GetTransactionsByHash(ctx context.Context, hashes []string) ([]types.UnifiedTransaction, error) {
	if len(hashes) == 0 {
		return nil, nil
	}

	placeholders := strings.Repeat("?,", len(hashes)-1) + "?"
	query := fmt.Sprintf(`
		SELECT tx_hash, chain_type, chain_id, block_number, block_hash, tx_index,
			   from_address, to_address, value, gas_limit, gas_used, gas_price,
			   status, timestamp, raw_data
		FROM transactions
		WHERE tx_hash IN (%s)
	`, placeholders)

	args := make([]interface{}, len(hashes))
	for i, hash := range hashes {
		args[i] = hash
	}

	rows, err := m.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, apperr.NewStorageError(apperr.StoreQueryError, "query transactions", err).WithOperation("GetTransactionsByHash")
	}
	defer rows.Close()

	var transactions []types.UnifiedTransaction
	for rows.Next() {
		tx, err := m.scanTransaction(rows)
		if err != nil {
			return nil, apperr.NewStorageError(apperr.StoreQueryError, "scan transaction", err).WithOperation("GetTransactionsByHash")
		}
		transactions = append(transactions, tx)
	}

	return transactions, nil
}

// GetStorageStats 获取存储统计
func (m *MySQLStore) GetStorageStats(ctx context.Context) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	var txCount int64
	if err := m.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM transactions").Scan(&txCount); err != nil {
		return nil, apperr.NewStorageError(apperr.StoreQueryError, "query transaction count", err).WithOperation("GetStorageStats")
	}
	stats["total_transactions"] = txCount

	var dexTxCount int64
	if err := m.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM dex_transactions").Scan(&dexTxCount); err != nil {
		log.Warnf("query dex_transactions count failed: %v", err)
	}
	stats["total_dex_transactions"] = dexTxCount

	chainStats := make(map[string]map[string]int64)
	rows, err := m.db.QueryContext(ctx, `
		SELECT chain_type, COUNT(*) AS tx_count
		FROM transactions
		GROUP BY chain_type
	`)
	if err != nil {
		return nil, apperr.NewStorageError(apperr.StoreQueryError, "query chain stats", err).WithOperation("GetStorageStats")
	}
	defer rows.Close()

	for rows.Next() {
		var chainType string
		var count int64
		if err := rows.Scan(&chainType, &count); err != nil {
			return nil, apperr.NewStorageError(apperr.StoreQueryError, "scan chain stats", err).WithOperation("GetStorageStats")
		}
		chainStats[chainType] = map[string]int64{"transactions": count}
	}
	stats["chain_stats"] = chainStats

	return stats, nil
}

// HealthCheck 健康检查
func (m *MySQLStore) HealthCheck(ctx context.Context) error {
	return m.db.PingContext(ctx)
}

// Close 关闭连接
func (m *MySQLStore) Close() error {
	return m.db.Close()
}

// scanTransaction 扫描交易行
func (m *MySQLStore) scanTransaction(rows *sql.Rows) (types.UnifiedTransaction, error) {
	var tx types.UnifiedTransaction
	var rawDataJSON sql.NullString

	err := rows.Scan(
		&tx.TxHash,
		&tx.ChainType,
		&tx.ChainID,
		utils.NullInt64ToBigInt(&tx.BlockNumber),
		utils.NullStringToString(&tx.BlockHash),
		utils.NullIntToInt(&tx.TxIndex),
		utils.NullStringToString(&tx.FromAddress),
		utils.NullStringToString(&tx.ToAddress),
		utils.NullStringToBigInt(&tx.Value),
		utils.NullInt64ToBigInt(&tx.GasLimit),
		utils.NullInt64ToBigInt(&tx.GasUsed),
		utils.NullInt64ToBigInt(&tx.GasPrice),
		&tx.Status,
		&tx.Timestamp,
		&rawDataJSON,
	)
	if err != nil {
		return tx, err
	}

	if rawDataJSON.Valid {
		if err := utils.FromJSON(rawDataJSON.String, &tx.RawData); err != nil {
			log.Warnf("failed to parse raw data: %v", err)
		}
	}

	return tx, nil
}
