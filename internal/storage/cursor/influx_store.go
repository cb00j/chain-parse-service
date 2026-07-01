package cursor

import (
	"context"
	"fmt"
	"time"

	"unified-tx-parser/internal/types"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
)

// cursorMeasurement is the InfluxDB measurement used for all cursors,
// mirroring the relational sync_cursors table: one point per (source,
// chain_type, protocol, cursor_key), with cursor_value as the only field.
// Tags carry the identifying dimensions (so Flux can filter/group on them
// cheaply); the value itself is a field since it changes on every sync run.
const cursorMeasurement = "sync_cursors"

// InfluxCursorStore implements types.CursorStore against InfluxDB. Unlike
// DBCursorStore, there's no native upsert — every SetCursor call writes a
// new point, and GetCursor relies on last() to resolve "the current value"
// for a given tag combination. This is the same "field-store, resolve on
// read via last()" approach the rest of the InfluxDB backend already uses
// (see GetAllTokenMeta), so it doesn't introduce a new pattern.
type InfluxCursorStore struct {
	writeAPI api.WriteAPI
	queryAPI api.QueryAPI
	bucket   string
}

// NewInfluxCursorStore creates an InfluxDB-backed cursor store. It takes an
// already-constructed influxdb2.Client rather than opening its own — the
// client is cheap to share (it's just an HTTP client wrapper) and the
// caller (CreateCursorStore) already needs one for the health check, so
// there's no reason to open a second connection the way DBCursorStore
// opens its own *sql.DB (there, a fresh *sql.DB is the norm; here, a fresh
// influxdb2.Client per store would just be redundant HTTP client setup).
func NewInfluxCursorStore(client influxdb2.Client, org, bucket string) *InfluxCursorStore {
	return &InfluxCursorStore{
		writeAPI: client.WriteAPI(org, bucket),
		queryAPI: client.QueryAPI(org),
		bucket:   bucket,
	}
}

// GetCursor implements types.CursorStore.
func (s *InfluxCursorStore) GetCursor(ctx context.Context, source, chainType, protocol, cursorKey string) (string, bool, error) {
	query := fmt.Sprintf(`
		from(bucket: %q)
		|> range(start: 0)
		|> filter(fn: (r) => r._measurement == %q)
		|> filter(fn: (r) => r.source == %q and r.chain_type == %q and r.protocol == %q and r.cursor_key == %q)
		|> filter(fn: (r) => r._field == "cursor_value")
		|> last()
	`, s.bucket, cursorMeasurement, source, chainType, protocol, cursorKey)

	result, err := s.queryAPI.Query(ctx, query)
	if err != nil {
		return "", false, fmt.Errorf("query cursor (%s/%s/%s/%s): %w", source, chainType, protocol, cursorKey, err)
	}
	defer result.Close()

	var value string
	found := false
	if result.Next() {
		if v, ok := result.Record().Value().(string); ok {
			value = v
			found = true
		}
	}
	if result.Err() != nil {
		return "", false, fmt.Errorf("read cursor (%s/%s/%s/%s): %w", source, chainType, protocol, cursorKey, result.Err())
	}

	return value, found, nil
}

// SetCursor implements types.CursorStore. Writes are async (api.WriteAPI)
// like the rest of this backend, with an explicit Flush so a cursor write
// is durable before SetCursor returns — a sync job advancing its cursor is
// exactly the kind of write that shouldn't sit in a buffer and get lost on
// a crash between "storage.StoreDexData succeeded" and "process exits."
func (s *InfluxCursorStore) SetCursor(ctx context.Context, source, chainType, protocol, cursorKey, value string) error {
	point := influxdb2.NewPoint(
		cursorMeasurement,
		map[string]string{
			"source":     source,
			"chain_type": chainType,
			"protocol":   protocol,
			"cursor_key": cursorKey,
		},
		map[string]interface{}{
			"cursor_value": value,
		},
		time.Now(),
	)

	s.writeAPI.WritePoint(point)
	s.writeAPI.Flush()

	// api.WriteAPI is fire-and-forget: WritePoint/Flush don't themselves
	// return a per-write error, errors surface asynchronously on the
	// client's error channel. That's an existing limitation of this
	// backend generally (see StoreTransactions/StoreBlocks above, which
	// have the same shape), not something specific to the cursor store.
	return nil
}

// Close flushes any buffered points. Callers that construct an
// InfluxCursorStore sharing a client with the main storage engine don't
// need to call this separately (the engine's own Close() will flush), but
// it's provided for standalone use (e.g. a future dedicated sync-job
// process that doesn't also run the main StorageEngine).
func (s *InfluxCursorStore) Close() {
	s.writeAPI.Flush()
}

var _ types.CursorStore = (*InfluxCursorStore)(nil)
