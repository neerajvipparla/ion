# ClickHouse Package — Testing Guide

## Unit Tests (no ClickHouse required)

Run the full clickhouse package test suite with race detection:

```bash
go test ./clickhouse/... -race
```

Run a specific test group:

```bash
# Row extraction logic
go test ./clickhouse/... -race -run TestExtractRow

# Schema DDL correctness
go test ./clickhouse/... -race -run TestBuildDDL

# Batch writer (flush on size, flush on ticker, drops, sync, shutdown)
go test ./clickhouse/... -race -run TestBatchWriter

# Core zapcore.Core interface (Enabled, Check, Write, With, Sync, DroppedCount)
go test ./clickhouse/... -race -run TestCore

# Config validation and defaults
go test ./clickhouse/... -race -run TestValidate
go test ./clickhouse/... -race -run TestDefaults
```

Run a single named test:

```bash
go test -v -run TestCore_With_PresetFieldsPropagated ./clickhouse/...
```

---

## Integration Tests (live ClickHouse required)

Set the DSN environment variable before running. HTTP on port 8123 is recommended:

```bash
export CLICKHOUSE_TEST_DSN="http://default:PASSWORD@HOST:8123/default"
```

Then run the full suite (unit + integration):

```bash
CLICKHOUSE_TEST_DSN="http://default:PASSWORD@HOST:8123/default" go test ./clickhouse/... -race -v
```

Integration tests that run with the DSN set:

```bash
# Verify New() connects and exposes config
CLICKHOUSE_TEST_DSN="http://default:PASSWORD@HOST:8123/default" \
  go test ./clickhouse/tests/... -race -v -run TestNew_ConnectsAndExposesConfig

# Create table and verify all 16 columns exist
CLICKHOUSE_TEST_DSN="http://default:PASSWORD@HOST:8123/default" \
  go test ./clickhouse/tests/... -race -v -run TestEnsureSchema_CreatesTable

# Verify CREATE TABLE IF NOT EXISTS is idempotent (run twice, no error)
CLICKHOUSE_TEST_DSN="http://default:PASSWORD@HOST:8123/default" \
  go test ./clickhouse/tests/... -race -v -run TestEnsureSchema_Idempotent

# Ping tests
CLICKHOUSE_TEST_DSN="http://default:PASSWORD@HOST:8123/default" \
  go test ./clickhouse/tests/... -race -v -run TestPing
```

---

## Create the Production Table

Run this once to create `ion_logs` on your ClickHouse instance. Safe to re-run — uses `IF NOT EXISTS`:

```bash
cat > /tmp/create_ion_logs.go << 'EOF'
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/JupiterMetaLabs/ion/clickhouse"
    "github.com/JupiterMetaLabs/ion/clickhouse/config"
)

func main() {
    cfg := config.Config{DSN: "http://default:PASSWORD@HOST:8123/default"}
    if err := clickhouse.RunEnsureSchema(context.Background(), cfg, "ion_logs"); err != nil {
        log.Fatalf("EnsureSchema: %v", err)
    }
    fmt.Println("ion_logs table created (or already exists)")
}
EOF
go run /tmp/create_ion_logs.go
```

---

## Verify the Table on ClickHouse

Ping the HTTP endpoint:

```bash
curl "http://default:PASSWORD@HOST:8123/ping"
# Expected: Ok.
```

Check the table exists:

```bash
curl "http://default:PASSWORD@HOST:8123/" \
  --data "SELECT name, engine FROM system.tables WHERE database = 'default' AND name = 'ion_logs' FORMAT Pretty"
```

Inspect all column definitions:

```bash
curl "http://default:PASSWORD@HOST:8123/" \
  --data "DESCRIBE TABLE ion_logs FORMAT Pretty"
```

Check row count after writing logs:

```bash
curl "http://default:PASSWORD@HOST:8123/" \
  --data "SELECT count() FROM ion_logs FORMAT Pretty"
```

---

## Test File Map

| File | Package | What it tests | Needs ClickHouse |
|------|---------|--------------|-----------------|
| `clickhouse/row_test.go` | white-box | `extractRow()` field routing | No |
| `clickhouse/batch_test.go` | white-box | `batchWriter` flush/drop/sync/shutdown | No |
| `clickhouse/core_test.go` | white-box | `Core` zapcore interface | No |
| `clickhouse/schema_test.go` | white-box | `buildDDL`, `ensureSchema` with mock | No |
| `clickhouse/tests/config_test.go` | black-box | `Config.Validate()`, `WithDefaults()`, `New()` | Only `TestNew_ConnectsAndExposesConfig` |
| `clickhouse/tests/ping_test.go` | black-box | `Config.Ping()` error paths | No |
| `clickhouse/tests/schema_ddl_test.go` | black-box | `BuildDDL` output | No |
| `clickhouse/tests/schema_test.go` | black-box | `RunEnsureSchema` creates real table | Yes |
