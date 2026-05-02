package tests

import (
	"strings"
	"testing"

	"github.com/JupiterMetaLabs/ion/internal/clickhouse"
)

// --- buildDDL ---

func TestBuildDDL_ContainsAllColumns(t *testing.T) {
	ddl := clickhouse.BuildDDL("ion_logs")

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
	ddl := clickhouse.BuildDDL("my_custom_table")

	if !strings.Contains(ddl, "my_custom_table") {
		t.Error("DDL does not contain the provided table name")
	}
	if strings.Contains(ddl, "ion_logs") {
		t.Error("DDL contains hardcoded table name instead of the provided one")
	}
}

func TestBuildDDL_IfNotExists(t *testing.T) {
	ddl := clickhouse.BuildDDL("ion_logs")
	if !strings.Contains(ddl, "IF NOT EXISTS") {
		t.Error("DDL missing IF NOT EXISTS — not safe to re-run")
	}
}

func TestBuildDDL_LowCardinalityOnRepetitiveColumns(t *testing.T) {
	ddl := clickhouse.BuildDDL("ion_logs")
	for _, col := range []string{"level", "service", "version"} {
		// The column definition must use LowCardinality, not plain String.
		if !strings.Contains(ddl, "LowCardinality(String)") {
			t.Errorf("DDL: %q should use LowCardinality(String)", col)
		}
	}
}

func TestBuildDDL_TypedMapColumns(t *testing.T) {
	ddl := clickhouse.BuildDDL("ion_logs")

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
	ddl := clickhouse.BuildDDL("ion_logs")
	if !strings.Contains(ddl, "MergeTree()") {
		t.Error("DDL missing MergeTree() engine")
	}
}

func TestBuildDDL_PartitionByMonth(t *testing.T) {
	ddl := clickhouse.BuildDDL("ion_logs")
	if !strings.Contains(ddl, "PARTITION BY toYYYYMM(timestamp)") {
		t.Error("DDL missing PARTITION BY toYYYYMM(timestamp)")
	}
}

func TestBuildDDL_OrderBy(t *testing.T) {
	ddl := clickhouse.BuildDDL("ion_logs")
	if !strings.Contains(ddl, "ORDER BY (service, level, timestamp)") {
		t.Error("DDL missing ORDER BY (service, level, timestamp)")
	}
}

func TestBuildDDL_TTL(t *testing.T) {
	ddl := clickhouse.BuildDDL("ion_logs")
	if !strings.Contains(ddl, "TTL") {
		t.Error("DDL missing TTL clause")
	}
}

func TestBuildDDL_NanosecondTimestamp(t *testing.T) {
	ddl := clickhouse.BuildDDL("ion_logs")
	if !strings.Contains(ddl, "DateTime64(9") {
		t.Error("DDL: timestamp column must use DateTime64(9,...) for nanosecond precision")
	}
}


