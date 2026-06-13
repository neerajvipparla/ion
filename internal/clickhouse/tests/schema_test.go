package tests

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/neerajvipparla/ion/internal/clickhouse"
	"github.com/neerajvipparla/ion/internal/clickhouse/config"

	chdriver "github.com/ClickHouse/clickhouse-go/v2"
)

// liveConn opens a real ClickHouse connection using CLICKHOUSE_TEST_DSN.
// Skips the test if the env var is not set.
func liveConn(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("CLICKHOUSE_TEST_DSN")
	if dsn == "" {
		t.Skip("CLICKHOUSE_TEST_DSN not set — skipping integration test")
	}
	opts, err := chdriver.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("ParseDSN: %v", err)
	}
	db := chdriver.OpenDB(opts)
	if err := db.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	return db
}

func TestEnsureSchema_CreatesTable(t *testing.T) {
	dsn := os.Getenv("CLICKHOUSE_TEST_DSN")
	if dsn == "" {
		t.Skip("CLICKHOUSE_TEST_DSN not set — skipping integration test")
	}

	table := "ion_test_schema"
	if err := clickhouse.RunEnsureSchema(context.Background(), config.Config{DSN: dsn}, table); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	db := liveConn(t)
	defer db.Close()

	// Verify table exists and has the expected columns
	rows, err := db.QueryContext(context.Background(),
		fmt.Sprintf("DESCRIBE TABLE %s", table))
	if err != nil {
		t.Fatalf("DESCRIBE TABLE: %v", err)
	}
	defer rows.Close()

	cols := map[string]string{}
	for rows.Next() {
		var name, typ, defaultType, defaultExpr, comment, codecExpr, ttlExpr string
		if err := rows.Scan(&name, &typ, &defaultType, &defaultExpr, &comment, &codecExpr, &ttlExpr); err != nil {
			t.Fatalf("scan: %v", err)
		}
		cols[name] = typ
	}

	required := map[string]string{
		"timestamp":   "DateTime64(9, 'UTC')",
		"level":       "LowCardinality(String)",
		"service":     "LowCardinality(String)",
		"version":     "LowCardinality(String)",
		"logger":      "String",
		"message":     "String",
		"trace_id":    "String",
		"span_id":     "String",
		"request_id":  "String",
		"user_id":     "String",
		"caller":      "String",
		"str_fields":  "Map(String, String)",
		"int_fields":  "Map(String, Int64)",
		"flt_fields":  "Map(String, Float64)",
		"bool_fields": "Map(String, UInt8)",
		"extra":       "String",
	}

	for col, wantType := range required {
		gotType, ok := cols[col]
		if !ok {
			t.Errorf("column %q missing from table", col)
			continue
		}
		if !strings.EqualFold(gotType, wantType) {
			t.Errorf("column %q: got type %q, want %q", col, gotType, wantType)
		}
	}

	// Cleanup
	_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP TABLE IF EXISTS %s", table))
}

func TestEnsureSchema_Idempotent(t *testing.T) {
	dsn := os.Getenv("CLICKHOUSE_TEST_DSN")
	if dsn == "" {
		t.Skip("CLICKHOUSE_TEST_DSN not set — skipping integration test")
	}

	table := "ion_test_schema_idem"
	cfg := config.Config{DSN: dsn}

	// Running twice must not error — CREATE TABLE IF NOT EXISTS guarantees this
	for i := range 2 {
		if err := clickhouse.RunEnsureSchema(context.Background(), cfg, table); err != nil {
			t.Fatalf("EnsureSchema run %d: %v", i+1, err)
		}
	}

	db := liveConn(t)
	defer db.Close()
	_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP TABLE IF EXISTS %s", table))
}
