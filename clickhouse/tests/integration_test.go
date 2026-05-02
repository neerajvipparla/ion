package tests

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/JupiterMetaLabs/ion/clickhouse"
	"github.com/JupiterMetaLabs/ion/clickhouse/config"
)

// integrationDSN returns the test DSN or skips the test.
func integrationDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("CLICKHOUSE_TEST_DSN")
	if dsn == "" {
		t.Skip("CLICKHOUSE_TEST_DSN not set — skipping integration test")
	}
	return dsn
}

// openCore creates a Core with AutoSchema=true against a unique test table,
// registers cleanup to drop the table, and returns the core + table name.
func openCore(t *testing.T, dsn string) (*clickhouse.Core, string) {
	t.Helper()
	table := fmt.Sprintf("ion_integration_%d", time.Now().UnixNano())

	cfg := config.Config{
		DSN:           dsn,
		Table:         table,
		AutoSchema:    true,
		BatchSize:     100,
		FlushInterval: 500 * time.Millisecond,
	}

	ctx := context.Background()
	core, err := clickhouse.New(ctx, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := core.Open(ctx); err != nil {
		t.Fatalf("Open: %v", err)
	}

	t.Cleanup(func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = core.Shutdown(shutCtx)

		db := liveConn(t)
		defer db.Close()
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP TABLE IF EXISTS %s", table))
	})

	return core, table
}

// queryRows fetches all rows from the given table ordered by timestamp.
func queryRows(t *testing.T, db *sql.DB, table string) *sql.Rows {
	t.Helper()
	rows, err := db.QueryContext(context.Background(),
		fmt.Sprintf("SELECT timestamp, level, service, version, logger, message, trace_id, span_id, request_id, user_id, caller, str_fields, int_fields, flt_fields, bool_fields, extra FROM %s ORDER BY timestamp", table))
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	return rows
}

// --- Tests ---

func TestIntegration_SingleRow_AllFixedColumns(t *testing.T) {
	dsn := integrationDSN(t)
	core, table := openCore(t, dsn)
	db := liveConn(t)
	defer db.Close()

	ts := time.Now().UTC().Truncate(time.Millisecond)
	entry := zapcore.Entry{
		Level:   zapcore.InfoLevel,
		Time:    ts,
		Message: "hello integration",
	}
	fields := []zapcore.Field{
		zap.String("service", "test-svc"),
		zap.String("version", "v1.2.3"),
		zap.String("trace_id", "trace-abc"),
		zap.String("span_id", "span-xyz"),
		zap.String("request_id", "req-001"),
		zap.String("user_id", "user-42"),
	}
	if err := core.Write(entry, fields); err != nil {
		t.Fatalf("Write: %v", err)
	}
	core.Sync() //nolint:errcheck

	// Poll briefly for the async flush
	var count int
	for range 20 {
		_ = db.QueryRowContext(context.Background(),
			fmt.Sprintf("SELECT count() FROM %s", table)).Scan(&count)
		if count > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if count == 0 {
		t.Fatal("no rows found after flush")
	}

	rows := queryRows(t, db, table)
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("expected at least one row")
	}

	var (
		gotTimestamp                                               time.Time
		gotLevel, gotService, gotVersion, gotLogger, gotMessage    string
		gotTraceID, gotSpanID, gotRequestID, gotUserID, gotCaller string
		gotStrFields                                               map[string]string
		gotIntFields                                               map[string]int64
		gotFltFields                                               map[string]float64
		gotBoolFields                                              map[string]uint8
		gotExtra                                                   string
	)
	if err := rows.Scan(
		&gotTimestamp, &gotLevel, &gotService, &gotVersion, &gotLogger, &gotMessage,
		&gotTraceID, &gotSpanID, &gotRequestID, &gotUserID, &gotCaller,
		&gotStrFields, &gotIntFields, &gotFltFields, &gotBoolFields, &gotExtra,
	); err != nil {
		t.Fatalf("scan: %v", err)
	}

	if gotLevel != "info" {
		t.Errorf("level: got %q, want %q", gotLevel, "info")
	}
	if gotMessage != "hello integration" {
		t.Errorf("message: got %q, want %q", gotMessage, "hello integration")
	}
	if gotService != "test-svc" {
		t.Errorf("service: got %q, want %q", gotService, "test-svc")
	}
	if gotVersion != "v1.2.3" {
		t.Errorf("version: got %q, want %q", gotVersion, "v1.2.3")
	}
	if gotTraceID != "trace-abc" {
		t.Errorf("trace_id: got %q, want %q", gotTraceID, "trace-abc")
	}
	if gotSpanID != "span-xyz" {
		t.Errorf("span_id: got %q, want %q", gotSpanID, "span-xyz")
	}
	if gotRequestID != "req-001" {
		t.Errorf("request_id: got %q, want %q", gotRequestID, "req-001")
	}
	if gotUserID != "user-42" {
		t.Errorf("user_id: got %q, want %q", gotUserID, "user-42")
	}
}

func TestIntegration_TypedMapColumns(t *testing.T) {
	dsn := integrationDSN(t)
	core, table := openCore(t, dsn)
	db := liveConn(t)
	defer db.Close()

	entry := zapcore.Entry{Level: zapcore.WarnLevel, Time: time.Now(), Message: "typed fields"}
	fields := []zapcore.Field{
		zap.String("env", "staging"),
		zap.Int64("latency_us", 1234),
		zap.Float64("score", 0.95),
		zap.Bool("ok", true),
	}
	if err := core.Write(entry, fields); err != nil {
		t.Fatalf("Write: %v", err)
	}
	core.Sync() //nolint:errcheck

	var count int
	for range 20 {
		_ = db.QueryRowContext(context.Background(),
			fmt.Sprintf("SELECT count() FROM %s", table)).Scan(&count)
		if count > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if count == 0 {
		t.Fatal("no rows after flush")
	}

	rows := queryRows(t, db, table)
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("expected a row")
	}

	var (
		ts, level, svc, ver, logger, msg, tid, sid, rid, uid, caller string
		strF                                                           map[string]string
		intF                                                           map[string]int64
		fltF                                                           map[string]float64
		boolF                                                          map[string]uint8
		extra                                                          string
	)
	if err := rows.Scan(&ts, &level, &svc, &ver, &logger, &msg, &tid, &sid, &rid, &uid, &caller,
		&strF, &intF, &fltF, &boolF, &extra); err != nil {
		t.Fatalf("scan: %v", err)
	}

	if strF["env"] != "staging" {
		t.Errorf("str_fields[env]: got %q, want %q", strF["env"], "staging")
	}
	if intF["latency_us"] != 1234 {
		t.Errorf("int_fields[latency_us]: got %d, want 1234", intF["latency_us"])
	}
	if fltF["score"] != 0.95 {
		t.Errorf("flt_fields[score]: got %v, want 0.95", fltF["score"])
	}
	if boolF["ok"] != 1 {
		t.Errorf("bool_fields[ok]: got %d, want 1", boolF["ok"])
	}
}

func TestIntegration_ErrorFieldGoesToExtra(t *testing.T) {
	dsn := integrationDSN(t)
	core, table := openCore(t, dsn)
	db := liveConn(t)
	defer db.Close()

	entry := zapcore.Entry{Level: zapcore.ErrorLevel, Time: time.Now(), Message: "something failed"}
	fields := []zapcore.Field{
		zap.NamedError("db_err", fmt.Errorf("connection refused")),
	}
	if err := core.Write(entry, fields); err != nil {
		t.Fatalf("Write: %v", err)
	}
	core.Sync() //nolint:errcheck

	var count int
	for range 20 {
		_ = db.QueryRowContext(context.Background(),
			fmt.Sprintf("SELECT count() FROM %s", table)).Scan(&count)
		if count > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if count == 0 {
		t.Fatal("no rows after flush")
	}

	var extra string
	if err := db.QueryRowContext(context.Background(),
		fmt.Sprintf("SELECT extra FROM %s LIMIT 1", table)).Scan(&extra); err != nil {
		t.Fatalf("query extra: %v", err)
	}
	if extra == "" {
		t.Fatal("extra is empty — error field was not written")
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(extra), &parsed); err != nil {
		t.Fatalf("extra is not valid JSON: %v — value: %q", err, extra)
	}
	if _, ok := parsed["db_err"]; !ok {
		t.Errorf("extra JSON missing key %q, got: %s", "db_err", extra)
	}
}

func TestIntegration_BatchOf100Rows(t *testing.T) {
	dsn := integrationDSN(t)
	core, table := openCore(t, dsn)
	db := liveConn(t)
	defer db.Close()

	const n = 100
	for i := range n {
		entry := zapcore.Entry{
			Level:   zapcore.InfoLevel,
			Time:    time.Now(),
			Message: fmt.Sprintf("row %d", i),
		}
		if err := core.Write(entry, []zapcore.Field{zap.Int64("i", int64(i))}); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}

	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := core.Shutdown(shutCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	var count int
	if err := db.QueryRowContext(context.Background(),
		fmt.Sprintf("SELECT count() FROM %s", table)).Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != n {
		t.Errorf("row count: got %d, want %d. DroppedCount=%d", count, n, core.DroppedCount())
	}
	if core.DroppedCount() != 0 {
		t.Errorf("DroppedCount: got %d, want 0", core.DroppedCount())
	}
}

func TestIntegration_WithFields_PresetFieldsInEveryRow(t *testing.T) {
	dsn := integrationDSN(t)
	core, table := openCore(t, dsn)
	db := liveConn(t)
	defer db.Close()

	// With() returns a child core that stamps every row with preset fields.
	child := core.With([]zapcore.Field{zap.String("service", "preset-svc")})

	for range 3 {
		entry := zapcore.Entry{Level: zapcore.InfoLevel, Time: time.Now(), Message: "msg"}
		if err := child.Write(entry, nil); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := core.Shutdown(shutCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	rows, err := db.QueryContext(context.Background(),
		fmt.Sprintf("SELECT service FROM %s", table))
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	n := 0
	for rows.Next() {
		var svc string
		if err := rows.Scan(&svc); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if svc != "preset-svc" {
			t.Errorf("row %d service: got %q, want %q", n, svc, "preset-svc")
		}
		n++
	}
	if n != 3 {
		t.Errorf("row count: got %d, want 3", n)
	}
}

func TestIntegration_Shutdown_FlushesRemainingRows(t *testing.T) {
	dsn := integrationDSN(t)

	// Tiny FlushInterval so the ticker doesn't fire before Shutdown.
	cfg := config.Config{
		DSN:           dsn,
		Table:         fmt.Sprintf("ion_integration_%d", time.Now().UnixNano()),
		AutoSchema:    true,
		BatchSize:     1000,         // large — won't flush on batch size
		FlushInterval: time.Hour,    // huge — ticker won't fire
	}

	ctx := context.Background()
	core, err := clickhouse.New(ctx, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := core.Open(ctx); err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		db := liveConn(t)
		defer db.Close()
		_, _ = db.ExecContext(context.Background(),
			fmt.Sprintf("DROP TABLE IF EXISTS %s", cfg.Table))
	})

	const n = 10
	for i := range n {
		entry := zapcore.Entry{Level: zapcore.InfoLevel, Time: time.Now(), Message: "pre-shutdown"}
		_ = core.Write(entry, []zapcore.Field{zap.Int64("i", int64(i))})
	}

	// Shutdown must drain and flush before returning.
	shutCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := core.Shutdown(shutCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	db := liveConn(t)
	defer db.Close()

	var count int
	if err := db.QueryRowContext(context.Background(),
		fmt.Sprintf("SELECT count() FROM %s", cfg.Table)).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != n {
		t.Errorf("post-shutdown count: got %d, want %d", count, n)
	}
}
