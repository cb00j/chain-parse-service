package cursor

import (
	"context"
	"database/sql"
	"fmt"
	"unified-tx-parser/internal/types"
)

// Dialect selects the SQL placeholder style. Mirrors the same small,
// package-local enum used by internal/storage/progress/db — kept local
// rather than shared because each package's SQL is otherwise independent
// and a shared dialect type would just be indirection without payoff.
type Dialect string

const (
	DialectMySQL    Dialect = "mysql"
	DialectPostgres Dialect = "postgres"
)

// DBCursorStore implements types.CursorStore using a relational database.
// It is deliberately schema-thin: one row per (source, chain_type,
// protocol, cursor_key), value stored as a string so any sync job — not
// just The Graph — can reuse the same table without a migration.
type DBCursorStore struct {
	db      *sql.DB
	dialect Dialect
}

// NewDBCursorStore creates a DB-backed cursor store.
func NewDBCursorStore(db *sql.DB, dialect Dialect) *DBCursorStore {
	return &DBCursorStore{db: db, dialect: dialect}
}

// ph returns the positional placeholder for position n (1-based).
func (d *DBCursorStore) ph(n int) string {
	if d.dialect == DialectPostgres {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}

// GetCursor implements types.CursorStore.
func (d *DBCursorStore) GetCursor(ctx context.Context, source, chainType, protocol, cursorKey string) (string, bool, error) {
	query := fmt.Sprintf(
		`SELECT cursor_value FROM sync_cursors WHERE source = %s AND chain_type = %s AND protocol = %s AND cursor_key = %s`,
		d.ph(1), d.ph(2), d.ph(3), d.ph(4),
	)

	var value string
	err := d.db.QueryRowContext(ctx, query, source, chainType, protocol, cursorKey).Scan(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, fmt.Errorf("get cursor (%s/%s/%s/%s): %w", source, chainType, protocol, cursorKey, err)
	}

	return value, true, nil
}

// SetCursor implements types.CursorStore.
func (d *DBCursorStore) SetCursor(ctx context.Context, source, chainType, protocol, cursorKey, value string) error {
	query := d.upsertSQL()

	_, err := d.db.ExecContext(ctx, query, source, chainType, protocol, cursorKey, value)
	if err != nil {
		return fmt.Errorf("set cursor (%s/%s/%s/%s): %w", source, chainType, protocol, cursorKey, err)
	}

	return nil
}

func (d *DBCursorStore) upsertSQL() string {
	if d.dialect == DialectPostgres {
		return `INSERT INTO sync_cursors (source, chain_type, protocol, cursor_key, cursor_value)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (source, chain_type, protocol, cursor_key) DO UPDATE SET
				cursor_value = EXCLUDED.cursor_value,
				updated_at   = NOW()`
	}
	return `INSERT INTO sync_cursors (source, chain_type, protocol, cursor_key, cursor_value)
		VALUES (?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			cursor_value = VALUES(cursor_value),
			updated_at   = CURRENT_TIMESTAMP`
}

// noopCursorStore is a do-nothing fallback for storage backends without a
// relational database (e.g. storage.type=influxdb), mirroring
// noopProgressTracker in internal/app/storage.go. GetCursor always reports
// "no cursor saved," which is safe: the caller (thegraph.Syncer) falls back
// to TheGraphConfig.InitialSince and simply re-fetches from that point on
// every run — correct but not incremental.
type noopCursorStore struct{}

// NewNoopCursorStore creates a CursorStore that persists nothing.
func NewNoopCursorStore() types.CursorStore { return &noopCursorStore{} }

func (n *noopCursorStore) GetCursor(ctx context.Context, source, chainType, protocol, cursorKey string) (string, bool, error) {
	return "", false, nil
}

func (n *noopCursorStore) SetCursor(ctx context.Context, source, chainType, protocol, cursorKey, value string) error {
	return nil
}

var _ types.CursorStore = (*DBCursorStore)(nil)
var _ types.CursorStore = (*noopCursorStore)(nil)
