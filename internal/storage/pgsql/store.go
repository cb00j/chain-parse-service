package pgsql

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

	_ "github.com/lib/pq"
	"github.com/sirupsen/logrus"
)

var log = logrus.WithFields(logrus.Fields{"service": "parser", "module": "storage-pgsql"})

// batchChunkSize controls the max rows per multi-row INSERT statement.
// Keeps query size reasonable and avoids exceeding postgres parameter limits (65535).
const batchChunkSize = 500

type PgSQLStore struct {
	db     *sql.DB
	config *PgSQLConfig
}

type PgSQLConfig struct {
	Host            string `json:"host"`
	Port            int    `json:"port"`
	Username        string `json:"username"`
	Password        string `json:"password"`
	Database        string `json:"database"`
	SSLMode         string `json:"sslmode"`
	MaxOpenConns    int    `json:"max_open_conns"`
	MaxIdleConns    int    `json:"max_idle_conns"`
	ConnMaxLifetime int    `json:"conn_max_lifetime"`
}

func NewPgSQLStore(config *PgSQLConfig) (*PgSQLStore, error) {
	if config == nil {
		config = &PgSQLConfig{
			Host:            "localhost",
			Port:            5432,
			Username:        "postgres",
			Password:        "password",
			Database:        "unified_tx_parser",
			SSLMode:         "disable",
			MaxOpenConns:    100,
			MaxIdleConns:    10,
			ConnMaxLifetime: 3600,
		}
	}

	dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		config.Username, config.Password, config.Host, config.Port, config.Database, config.SSLMode)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, apperr.NewStorageError(apperr.StoreConnectionError, "connecting to PostgreSQL", err).
			WithOperation("NewPgSQLStore")
	}

	db.SetMaxOpenConns(config.MaxOpenConns)
	db.SetMaxIdleConns(config.MaxIdleConns)
	if config.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(time.Duration(config.ConnMaxLifetime) * time.Second)
	} else {
		db.SetConnMaxLifetime(time.Hour)
	}

	store := &PgSQLStore{
		db:     db,
		config: config,
	}

	if err := store.HealthCheck(context.Background()); err != nil {
		return nil, apperr.NewStorageError(apperr.StoreConnectionError, "PostgreSQL health check failed", err).
			WithOperation("NewPgSQLStore")
	}

	log.Infof("pgsql storage engine initialized: %s:%d/%s", config.Host, config.Port, config.Database)
	return store, nil
}

// StoreBlocks stores unified blocks into PostgreSQL.
func (p *PgSQLStore) StoreBlocks(ctx context.Context, blocks []types.UnifiedBlock) error {
	if len(blocks) == 0 {
		return nil
	}

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return apperr.NewStorageError(apperr.StoreTransactionErr, "begin tx", err).WithOperation("pgsql")
	}
	defer tx.Rollback()

	for i := 0; i < len(blocks); i += batchChunkSize {
		end := i + batchChunkSize
		if end > len(blocks) {
			end = len(blocks)
		}
		if err := p.batchInsertBlocks(ctx, tx, blocks[i:end]); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return apperr.NewStorageError(apperr.StoreTransactionErr, "commit blocks", err).WithOperation("StoreBlocks")
	}

	log.Infof("stored %d blocks", len(blocks))
	return nil
}

func (p *PgSQLStore) batchInsertBlocks(ctx context.Context, tx *sql.Tx, blocks []types.UnifiedBlock) error {
	if len(blocks) == 0 {
		return nil
	}

	var b strings.Builder
	b.WriteString(`INSERT INTO blocks (block_number, block_hash, chain_type, chain_id, parent_hash,
		timestamp, tx_count, size)
		VALUES `)

	args := make([]interface{}, 0, len(blocks)*8)
	for i, block := range blocks {
		if i > 0 {
			b.WriteString(",")
		}
		base := i * 8
		fmt.Fprintf(&b, "($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8)
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
	b.WriteString(` ON CONFLICT (block_hash, chain_type) DO UPDATE SET
		block_number = EXCLUDED.block_number,
		tx_count = EXCLUDED.tx_count,
		size = EXCLUDED.size,
		updated_at = NOW()`)

	_, err := tx.ExecContext(ctx, b.String(), args...)
	if err != nil {
		return apperr.NewStorageError(apperr.StoreInsertError, "batch insert blocks", err).WithOperation("StoreBlocks")
	}
	return nil
}

// StoreTransactions stores unified transactions using batch multi-row INSERT.
func (p *PgSQLStore) StoreTransactions(ctx context.Context, txs []types.UnifiedTransaction) error {
	if len(txs) == 0 {
		return nil
	}

	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return apperr.NewStorageError(apperr.StoreTransactionErr, "begin tx", err).WithOperation("pgsql")
	}
	defer tx.Rollback()

	for i := 0; i < len(txs); i += batchChunkSize {
		end := i + batchChunkSize
		if end > len(txs) {
			end = len(txs)
		}
		if err := p.batchInsertTransactions(ctx, tx, txs[i:end]); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return apperr.NewStorageError(apperr.StoreTransactionErr, "commit transactions", err).WithOperation("StoreTransactions")
	}

	log.Infof("stored %d transactions", len(txs))
	return nil
}

func (p *PgSQLStore) batchInsertTransactions(ctx context.Context, tx *sql.Tx, txs []types.UnifiedTransaction) error {
	if len(txs) == 0 {
		return nil
	}

	const cols = 15
	var b strings.Builder
	b.WriteString(`INSERT INTO transactions (
		tx_hash, chain_type, chain_id, block_number, block_hash, tx_index,
		from_address, to_address, value, gas_limit, gas_used, gas_price,
		status, timestamp, raw_data
	) VALUES `)

	args := make([]interface{}, 0, len(txs)*cols)
	for i, transaction := range txs {
		if i > 0 {
			b.WriteString(",")
		}
		base := i * cols
		fmt.Fprintf(&b, "($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8,
			base+9, base+10, base+11, base+12, base+13, base+14, base+15)

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

	b.WriteString(` ON CONFLICT (tx_hash, chain_type) DO UPDATE SET
		block_number = EXCLUDED.block_number,
		block_hash = EXCLUDED.block_hash,
		tx_index = EXCLUDED.tx_index,
		from_address = EXCLUDED.from_address,
		to_address = EXCLUDED.to_address,
		value = EXCLUDED.value,
		gas_limit = EXCLUDED.gas_limit,
		gas_used = EXCLUDED.gas_used,
		gas_price = EXCLUDED.gas_price,
		status = EXCLUDED.status,
		timestamp = EXCLUDED.timestamp,
		raw_data = EXCLUDED.raw_data,
		updated_at = NOW()`)

	_, err := tx.ExecContext(ctx, b.String(), args...)
	if err != nil {
		return apperr.NewStorageError(apperr.StoreInsertError, "batch insert transactions", err).WithOperation("StoreTransactions")
	}
	return nil
}

// StoreDexData stores all DEX data types atomically in a single transaction.
func (p *PgSQLStore) StoreDexData(ctx context.Context, dexData *types.DexData) error {
	if dexData == nil {
		return nil
	}

	dbTx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return apperr.NewStorageError(apperr.StoreTransactionErr, "begin tx", err).WithOperation("pgsql")
	}
	defer dbTx.Rollback()

	if err := p.batchUpsertPools(ctx, dbTx, dexData.Pools); err != nil {
		return apperr.NewStorageError(apperr.StoreInsertError, "batch upsert pools", err).WithOperation("StoreDexData")
	}
	if err := p.batchUpsertTokens(ctx, dbTx, dexData.Tokens); err != nil {
		return apperr.NewStorageError(apperr.StoreInsertError, "batch upsert tokens", err).WithOperation("StoreDexData")
	}
	if err := p.batchInsertDexTransactions(ctx, dbTx, dexData.Transactions); err != nil {
		return apperr.NewStorageError(apperr.StoreInsertError, "batch insert dex transactions", err).WithOperation("StoreDexData")
	}
	if err := p.batchInsertLiquidities(ctx, dbTx, dexData.Liquidities); err != nil {
		return apperr.NewStorageError(apperr.StoreInsertError, "batch insert liquidities", err).WithOperation("StoreDexData")
	}
	if err := p.batchUpsertReserves(ctx, dbTx, dexData.Reserves); err != nil {
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

func (p *PgSQLStore) batchUpsertPools(ctx context.Context, tx *sql.Tx, pools []model.Pool) error {
	for i := 0; i < len(pools); i += batchChunkSize {
		end := i + batchChunkSize
		if end > len(pools) {
			end = len(pools)
		}
		if err := p.batchUpsertPoolsChunk(ctx, tx, pools[i:end]); err != nil {
			return err
		}
	}
	return nil
}

func (p *PgSQLStore) batchUpsertPoolsChunk(ctx context.Context, tx *sql.Tx, pools []model.Pool) error {
	if len(pools) == 0 {
		return nil
	}

	const cols = 8
	var b strings.Builder
	b.WriteString(`INSERT INTO dex_pools (addr, factory, protocol, token0, token1, fee, source, extra) VALUES `)

	args := make([]interface{}, 0, len(pools)*cols)
	for i, pool := range pools {
		if i > 0 {
			b.WriteString(",")
		}
		base := i * cols
		fmt.Fprintf(&b, "($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8)

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
		args = append(args, pool.Addr, pool.Factory, pool.Protocol, token0, token1, pool.Fee, string(pool.Source), extraJSON)
	}

	b.WriteString(` ON CONFLICT (addr) DO UPDATE SET
		factory=EXCLUDED.factory, protocol=EXCLUDED.protocol,
		token0=EXCLUDED.token0, token1=EXCLUDED.token1,
		fee=EXCLUDED.fee, source=EXCLUDED.source, extra=COALESCE(EXCLUDED.extra, dex_pools.extra),
		updated_at=NOW()`)

	_, err := tx.ExecContext(ctx, b.String(), args...)
	return err
}

func (p *PgSQLStore) batchUpsertTokens(ctx context.Context, tx *sql.Tx, tokens []model.Token) error {
	for i := 0; i < len(tokens); i += batchChunkSize {
		end := i + batchChunkSize
		if end > len(tokens) {
			end = len(tokens)
		}
		if err := p.batchUpsertTokensChunk(ctx, tx, tokens[i:end]); err != nil {
			return err
		}
	}
	return nil
}

func (p *PgSQLStore) batchUpsertTokensChunk(ctx context.Context, tx *sql.Tx, tokens []model.Token) error {
	if len(tokens) == 0 {
		return nil
	}

	const cols = 5
	var b strings.Builder
	b.WriteString(`INSERT INTO dex_tokens (addr, name, symbol, decimals, is_stable) VALUES `)

	args := make([]interface{}, 0, len(tokens)*cols)
	for i, token := range tokens {
		if i > 0 {
			b.WriteString(",")
		}
		base := i * cols
		fmt.Fprintf(&b, "($%d,$%d,$%d,$%d,$%d)",
			base+1, base+2, base+3, base+4, base+5)
		args = append(args, token.Addr, token.Name, token.Symbol, token.Decimals, token.IsStable)
	}

	b.WriteString(` ON CONFLICT (addr) DO UPDATE SET
		name=EXCLUDED.name, symbol=EXCLUDED.symbol,
		decimals=EXCLUDED.decimals, is_stable=EXCLUDED.is_stable,
		updated_at=NOW()`)

	_, err := tx.ExecContext(ctx, b.String(), args...)
	return err
}

func (p *PgSQLStore) batchInsertDexTransactions(ctx context.Context, tx *sql.Tx, dexTxs []model.Transaction) error {
	for i := 0; i < len(dexTxs); i += batchChunkSize {
		end := i + batchChunkSize
		if end > len(dexTxs) {
			end = len(dexTxs)
		}
		if err := p.batchInsertDexTransactionsChunk(ctx, tx, dexTxs[i:end]); err != nil {
			return err
		}
	}
	return nil
}

func (p *PgSQLStore) batchInsertDexTransactionsChunk(ctx context.Context, tx *sql.Tx, dexTxs []model.Transaction) error {
	if len(dexTxs) == 0 {
		return nil
	}

	const cols = 17
	var b strings.Builder
	b.WriteString(`INSERT INTO dex_transactions (addr, protocol, router, factory, pool, hash, from_addr, side,
		amount, price, value, time, event_index, tx_index, swap_index, block_number, extra)
		VALUES `)

	args := make([]interface{}, 0, len(dexTxs)*cols)
	for i, dt := range dexTxs {
		if i > 0 {
			b.WriteString(",")
		}
		base := i * cols
		fmt.Fprintf(&b, "($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8, base+9,
			base+10, base+11, base+12, base+13, base+14, base+15, base+16, base+17)

		var extraJSON *string
		if dt.Extra != nil {
			if s, e := utils.ToJSON(dt.Extra); e == nil {
				extraJSON = &s
			}
		}
		args = append(args,
			dt.Addr, dt.Protocol, dt.Router, dt.Factory, dt.Pool, dt.Hash, dt.From, dt.Side,
			utils.BigIntToNullString(dt.Amount), dt.Price, dt.Value, dt.Time,
			dt.EventIndex, dt.TxIndex, dt.SwapIndex, dt.BlockNumber, extraJSON)
	}

	b.WriteString(` ON CONFLICT (hash, event_index, side) DO NOTHING`)

	_, err := tx.ExecContext(ctx, b.String(), args...)
	return err
}

func (p *PgSQLStore) batchInsertLiquidities(ctx context.Context, tx *sql.Tx, liqs []model.Liquidity) error {
	for i := 0; i < len(liqs); i += batchChunkSize {
		end := i + batchChunkSize
		if end > len(liqs) {
			end = len(liqs)
		}
		if err := p.batchInsertLiquiditiesChunk(ctx, tx, liqs[i:end]); err != nil {
			return err
		}
	}
	return nil
}

func (p *PgSQLStore) batchInsertLiquiditiesChunk(ctx context.Context, tx *sql.Tx, liqs []model.Liquidity) error {
	if len(liqs) == 0 {
		return nil
	}

	const cols = 13
	var b strings.Builder
	b.WriteString(`INSERT INTO dex_liquidities (addr, router, factory, pool, hash, from_addr, pos, side,
		amount, value, time, key, extra)
		VALUES `)

	args := make([]interface{}, 0, len(liqs)*cols)
	for i, liq := range liqs {
		if i > 0 {
			b.WriteString(",")
		}
		base := i * cols
		fmt.Fprintf(&b, "($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8,
			base+9, base+10, base+11, base+12, base+13)

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

	b.WriteString(` ON CONFLICT (key, addr) DO NOTHING`)

	_, err := tx.ExecContext(ctx, b.String(), args...)
	return err
}

func (p *PgSQLStore) batchUpsertReserves(ctx context.Context, tx *sql.Tx, reserves []model.Reserve) error {
	for i := 0; i < len(reserves); i += batchChunkSize {
		end := i + batchChunkSize
		if end > len(reserves) {
			end = len(reserves)
		}
		if err := p.batchUpsertReservesChunk(ctx, tx, reserves[i:end]); err != nil {
			return err
		}
	}
	return nil
}

func (p *PgSQLStore) batchUpsertReservesChunk(ctx context.Context, tx *sql.Tx, reserves []model.Reserve) error {
	if len(reserves) == 0 {
		return nil
	}

	const cols = 5
	var b strings.Builder
	b.WriteString(`INSERT INTO dex_reserves (addr, protocol, amount0, amount1, time) VALUES `)

	args := make([]interface{}, 0, len(reserves)*cols)
	for i, res := range reserves {
		if i > 0 {
			b.WriteString(",")
		}
		base := i * cols
		fmt.Fprintf(&b, "($%d,$%d,$%d,$%d,$%d)",
			base+1, base+2, base+3, base+4, base+5)

		var a0, a1 string
		if v, ok := res.Amounts[0]; ok && v != nil {
			a0 = v.String()
		}
		if v, ok := res.Amounts[1]; ok && v != nil {
			a1 = v.String()
		}
		args = append(args, res.Addr, res.Protocol, a0, a1, res.Time)
	}

	b.WriteString(` ON CONFLICT (addr, time) DO UPDATE SET
		protocol=EXCLUDED.protocol, amount0=EXCLUDED.amount0, amount1=EXCLUDED.amount1`)

	_, err := tx.ExecContext(ctx, b.String(), args...)
	return err
}

func (p *PgSQLStore) GetTransactionsByHash(ctx context.Context, hashes []string) ([]types.UnifiedTransaction, error) {
	if len(hashes) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(hashes))
	args := make([]interface{}, len(hashes))
	for i, hash := range hashes {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = hash
	}

	query := fmt.Sprintf(`
		SELECT tx_hash, chain_type, chain_id, block_number, block_hash, tx_index,
			   from_address, to_address, value, gas_limit, gas_used, gas_price,
			   status, timestamp, raw_data
		FROM transactions
		WHERE tx_hash IN (%s)
	`, strings.Join(placeholders, ","))

	rows, err := p.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, apperr.NewStorageError(apperr.StoreQueryError, "query transactions", err).WithOperation("GetTransactionsByHash")
	}
	defer rows.Close()

	var transactions []types.UnifiedTransaction
	for rows.Next() {
		t, err := p.scanTransaction(rows)
		if err != nil {
			return nil, apperr.NewStorageError(apperr.StoreQueryError, "scan transaction", err).WithOperation("GetTransactionsByHash")
		}
		transactions = append(transactions, t)
	}

	return transactions, nil
}

func (p *PgSQLStore) GetStorageStats(ctx context.Context) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	var txCount int64
	if err := p.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM transactions").Scan(&txCount); err != nil {
		return nil, apperr.NewStorageError(apperr.StoreQueryError, "query transaction count", err).WithOperation("GetStorageStats")
	}
	stats["total_transactions"] = txCount

	var dexTxCount int64
	if err := p.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM dex_transactions").Scan(&dexTxCount); err != nil {
		log.Warnf("query dex_transactions count failed: %v", err)
	}
	stats["total_dex_transactions"] = dexTxCount

	chainStats := make(map[string]map[string]int64)
	rows, err := p.db.QueryContext(ctx, `
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

// GetAllPoolTokens returns addr -> {token0, token1} for every pool with
// known (non-NULL, non-empty) token addresses. Used at startup to warm up
// the in-memory pool cache so a process restart doesn't re-trigger eth_call
// lookups for pools already resolved before the restart.
func (p *PgSQLStore) GetAllPoolTokens(ctx context.Context) (map[string][2]string, error) {
	rows, err := p.db.QueryContext(ctx, `
		SELECT addr, token0, token1 FROM dex_pools
		WHERE token0 IS NOT NULL AND token0 != ''
		  AND token1 IS NOT NULL AND token1 != ''
	`)
	if err != nil {
		return nil, fmt.Errorf("query pool tokens: %w", err)
	}
	defer rows.Close()

	out := make(map[string][2]string)
	for rows.Next() {
		var addr, token0, token1 string
		if err := rows.Scan(&addr, &token0, &token1); err != nil {
			continue
		}
		out[addr] = [2]string{token0, token1}
	}
	return out, rows.Err()
}

// GetAllTokenMeta returns addr -> decimals for every token in
// dex_tokens. Used at startup to warm up the in-memory token cache so a
// process restart doesn't re-derive decimals for tokens already resolved
// in a prior run.
func (p *PgSQLStore) GetAllTokenMeta(ctx context.Context) (map[string]model.Token, error) {
	rows, err := p.db.QueryContext(ctx, `SELECT addr, name, symbol, decimals FROM dex_tokens`)
	if err != nil {
		return nil, fmt.Errorf("query token meta: %w", err)
	}
	defer rows.Close()

	out := make(map[string]model.Token)
	for rows.Next() {
		var addr, name, symbol string
		var decimals int
		if err := rows.Scan(&addr, &name, &symbol, &decimals); err != nil {
			continue
		}
		out[addr] = model.Token{Addr: addr, Name: name, Symbol: symbol, Decimals: decimals}
	}
	return out, rows.Err()
}

func (p *PgSQLStore) HealthCheck(ctx context.Context) error {
	return p.db.PingContext(ctx)
}

func (p *PgSQLStore) Close() error {
	return p.db.Close()
}

func (p *PgSQLStore) scanTransaction(rows *sql.Rows) (types.UnifiedTransaction, error) {
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
