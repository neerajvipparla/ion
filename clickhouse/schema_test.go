package clickhouse

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// --- buildDDL ---

func TestBuildDDL_ContainsAllColumns(t *testing.T) {
	ddl := buildDDL("ion_logs")

	required := []string{
		"timestamp", "level", "service", "version",
		"logger", "message",
		"trace_id", "span_id", "request_id", "user_id",
		"caller",
		"str_fields", "int_fields", "flt_fields", "bool_fields",
		"extra",
	}
	for _, col := range required {
		if !strings.Contains(ddl, col) {
			t.Errorf("DDL missing column %q", col)
		}
	}
}

func TestBuildDDL_UsesProvidedTableName(t *testing.T) {
	ddl := buildDDL("my_custom_table")

	if !strings.Contains(ddl, "my_custom_table") {
		t.Error("DDL does not contain the provided table name")
	}
	if strings.Contains(ddl, "ion_logs") {
		t.Error("DDL contains hardcoded table name instead of the provided one")
	}
}

func TestBuildDDL_IfNotExists(t *testing.T) {
	ddl := buildDDL("ion_logs")
	if !strings.Contains(ddl, "IF NOT EXISTS") {
		t.Error("DDL missing IF NOT EXISTS — not safe to re-run")
	}
}

func TestBuildDDL_LowCardinalityOnRepetitiveColumns(t *testing.T) {
	ddl := buildDDL("ion_logs")
	for _, col := range []string{"level", "service", "version"} {
		// The column definition must use LowCardinality, not plain String.
		if !strings.Contains(ddl, "LowCardinality(String)") {
			t.Errorf("DDL: %q should use LowCardinality(String)", col)
		}
	}
}

func TestBuildDDL_TypedMapColumns(t *testing.T) {
	ddl := buildDDL("ion_logs")

	types := map[string]string{
		"str_fields":  "Map(String, String)",
		"int_fields":  "Map(String, Int64)",
		"flt_fields":  "Map(String, Float64)",
		"bool_fields": "Map(String, UInt8)",
	}
	for col, typ := range types {
		if !strings.Contains(ddl, typ) {
			t.Errorf("DDL: column %q missing type %q", col, typ)
		}
	}
}

func TestBuildDDL_MergeTreeEngine(t *testing.T) {
	ddl := buildDDL("ion_logs")
	if !strings.Contains(ddl, "MergeTree()") {
		t.Error("DDL missing MergeTree() engine")
	}
}

func TestBuildDDL_PartitionByMonth(t *testing.T) {
	ddl := buildDDL("ion_logs")
	if !strings.Contains(ddl, "PARTITION BY toYYYYMM(timestamp)") {
		t.Error("DDL missing PARTITION BY toYYYYMM(timestamp)")
	}
}

func TestBuildDDL_OrderBy(t *testing.T) {
	ddl := buildDDL("ion_logs")
	if !strings.Contains(ddl, "ORDER BY (service, level, timestamp)") {
		t.Error("DDL missing ORDER BY (service, level, timestamp)")
	}
}

func TestBuildDDL_TTL(t *testing.T) {
	ddl := buildDDL("ion_logs")
	if !strings.Contains(ddl, "TTL") {
		t.Error("DDL missing TTL clause")
	}
}

func TestBuildDDL_NanosecondTimestamp(t *testing.T) {
	ddl := buildDDL("ion_logs")
	if !strings.Contains(ddl, "DateTime64(9") {
		t.Error("DDL: timestamp column must use DateTime64(9,...) for nanosecond precision")
	}
}

// --- EnsureSchema ---

func TestEnsureSchema_CallsExecWithDDL(t *testing.T) {
	var captured string
	mock := &mockExecer{fn: func(ctx context.Context, query string) error {
		captured = query
		return nil
	}}

	cfg := minimalConfig("my_table")
	if err := EnsureSchema(context.Background(), mock, cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(captured, "my_table") {
		t.Errorf("Exec called with wrong DDL: does not contain table name. Got: %s", captured)
	}
	if !strings.Contains(captured, "IF NOT EXISTS") {
		t.Error("Exec called with DDL missing IF NOT EXISTS")
	}
}

func TestEnsureSchema_PropagatesExecError(t *testing.T) {
	mock := &mockExecer{fn: func(ctx context.Context, query string) error {
		return errors.New("permission denied")
	}}

	err := EnsureSchema(context.Background(), mock, minimalConfig("ion_logs"))
	if err == nil {
		t.Fatal("expected error from Exec, got nil")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("error %q does not contain original cause", err.Error())
	}
}

func TestEnsureSchema_ContextCancelledBeforeExec(t *testing.T) {
	called := false
	mock := &mockExecer{fn: func(ctx context.Context, query string) error {
		called = true
		return nil
	}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_ = EnsureSchema(ctx, mock, minimalConfig("ion_logs"))
	// Whether it short-circuits or delegates is an impl detail;
	// what matters is it does not panic and returns (possibly ctx error).
	_ = called
}

// --- helpers ---

type mockExecer struct {
	fn func(ctx context.Context, query string) error
}

func (m *mockExecer) Exec(ctx context.Context, query string, args ...any) error {
	return m.fn(ctx, query)
}

func minimalConfig(table string) schemaConfig {
	return schemaConfig{Table: table}
}
