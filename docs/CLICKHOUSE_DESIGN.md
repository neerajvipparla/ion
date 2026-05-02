# ClickHouse Log Sink — Design & Implementation Plan

**Date:** 2026-05-01
**Status:** Approved for implementation
**Module:** `github.com/JupiterMetaLabs/ion` (existing module, new package `clickhouse/`)
**Author context:** Ion is a Go observability library built on Zap + OpenTelemetry. This document describes a new `zapcore.Core` implementation that writes log entries directly to ClickHouse for fast time-series analytics queries.

---

## Context

Ion's log pipeline works by assembling a list of `zapcore.Core` instances inside `internal/core/logger_factory.go` and fanning them out with `zapcore.NewTee`. Existing sinks are: console (stdout/stderr), file (lumberjack), and OTEL (via otelzap bridge). Each sink is independent — adding or removing one does not affect the others.

ClickHouse is being added as a fourth sink. The reason: OTEL → Loki works for operational triage, but ClickHouse enables fast analytical queries over log data (e.g., "how many block validation errors per validator in the last 10 minutes?"). This is not possible efficiently with Loki.

Logs in Ion are semi-structured: every entry has a fixed set of well-known fields (`level`, `service`, `trace_id`, etc.) plus arbitrary typed fields attached at call time via `ion.String(...)`, `ion.Int64(...)`, `ion.F(...)`. The ClickHouse schema must store both without forcing callers to change anything.

**This package is built independently first.** It does not import or wire into `ion.New()` yet. It exposes a standard `zapcore.Core` that can be attached to any `*zap.Logger`. Once validated, connecting it to ion is a trivial change in `internal/core/logger_factory.go`.

---

## Package Structure

```
ion/                                        ← module root
  go.mod                                    ← gains one new dep: clickhouse-go/v2
  clickhouse/
    config.go                               ← Config struct + Validate()
    core.go                                 ← Core struct + zapcore.Core interface + New/Open/Shutdown
    batch.go                                ← batchWriter: channel, flush goroutine, drain
    row.go                                  ← logRow struct + extractRow() field extraction
    schema.go                               ← DDL const + EnsureSchema()
    core_test.go                            ← unit tests
    integration_test.go                     ← integration tests (env-gated via CLICKHOUSE_TEST_DSN)
```

The `clickhouse/` package is within the same module, so it is allowed to import `github.com/JupiterMetaLabs/ion/internal/core` for the sentinel key constant (`SentinelKey = "__ion_ctx__"`) and `SystemFieldPrefix = "__ion_"`.

---

## Architecture & Data Flow

```
Application goroutine
────────────────────────────────────────────────────────────────
  logger.Info(ctx, "msg", ion.String("k","v"), ion.Int64("n",42))
    │
    └─► zapLogger.zap.Info(...)
          │
          └─► zapcore.Tee  ─────► consoleCore (existing)
                │           ─────► fileCore    (existing)
                │           ─────► otelCore    (existing)
                │
                └─────────────► clickhouse.Core.Write(entry, fields)
                                   │
                                   ├─ extractRow(entry, allFields)   ← O(N) field scan
                                   │     maps zap field types →
                                   │     StrFields / IntFields / FltFields / BoolFields / Extra
                                   │
                                   └─ select {
                                        case batchWriter.ch <- row:   ← non-blocking send
                                        default:                       ← buffer full
                                          dropped.Add(1)
                                      }
                                      return nil                       ← always returns immediately

Background goroutine (one per Core, started in Open())
────────────────────────────────────────────────────────────────
  batchWriter.run():
    ticker := time.NewTicker(FlushInterval)
    batch  := []logRow{}

    for {
      select {
      case row := <-ch:
        batch = append(batch, row)
        if len(batch) >= BatchSize:
          flush(batch) → batch = nil

      case <-ticker.C:
        if len(batch) > 0:
          flush(batch) → batch = nil

      case <-stop:
        drain(ch) → append remaining to batch
        flush(batch)
        return
      }
    }

flush(batch):
  b, err := conn.PrepareBatch(ctx, "INSERT INTO <table>")
  for each row:
    b.AppendStruct(&row)
  b.Send()                  ← one TCP write, binary column-oriented protocol
  if err: print to stderr, drop batch, continue
```

**Key invariant:** `Write()` is always non-blocking. The only work done in the caller's goroutine is field extraction (O(N) scan) + a non-blocking channel send. ClickHouse I/O is entirely in the background goroutine.

---

## Component Specifications

### `config.go` — `Config`

```go
type Config struct {
    // Required
    DSN   string // "clickhouse://user:pass@host:9000/db" or HTTP variant

    // Optional with defaults
    Database        string        // default: "default"
    Table           string        // default: "ion_logs"
    Level           string        // minimum level to ship to CH; default: "info"
    BatchSize       int           // rows per flush; default: 1000
    FlushInterval   time.Duration // flush cadence; default: 5s
    ChannelBuffer   int           // async queue depth; default: 10000
    AutoSchema      bool          // run CREATE TABLE IF NOT EXISTS on Open(); default: true
    DialTimeout     time.Duration // default: 10s
    MaxOpenConns    int           // connection pool size; default: 5
    MaxIdleConns    int           // default: 5
    ConnMaxLifetime time.Duration // default: 1h
}
```

`Validate()` returns a single combined error for all invalid fields — same pattern as `internal/config.Config.Validate()` in the existing codebase.

`withDefaults()` is a private method that fills zero-value fields with the defaults above, called inside `New()`.

---

### `core.go` — `Core` (implements `zapcore.Core`)

```go
type Core struct {
    cfg          Config
    conn         driver.Conn      // clickhouse-go/v2 native connection
    level        zapcore.LevelEnabler
    writer       *batchWriter
    presetFields []zapcore.Field  // accumulated by With() calls
    dropped      atomic.Int64
}
```

**`zapcore.Core` interface — method-by-method:**

| Method | Behaviour |
|--------|-----------|
| `Enabled(lvl zapcore.Level) bool` | Delegates to `c.level.Enabled(lvl)`. Fast path: if the level is not enabled, Zap never calls `Check` or `Write`. |
| `With(fields []zapcore.Field) zapcore.Core` | Returns a **new** `*Core` with `presetFields = append(c.presetFields, fields...)`. Never mutates the receiver. The new Core shares the same `conn`, `writer`, and `dropped` counter — they all feed the same background flusher. |
| `Check(entry zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry` | Standard pattern: if `c.Enabled(entry.Level)`, call `ce.AddCore(entry, c)` and return. |
| `Write(entry zapcore.Entry, fields []zapcore.Field) error` | Merge `presetFields` + `fields`. Call `extractRow(entry, merged)`. Non-blocking send to `writer.ch`. Return `nil` always. |
| `Sync() error` | Call `writer.flushSync()` to flush the current in-memory batch immediately. Returns any ClickHouse error. |

**Public functions on `core.go`:**

```go
// New validates cfg, fills defaults, returns an uninitialised *Core.
// Does NOT connect to ClickHouse yet. Safe to call at program start.
func New(cfg Config) (*Core, error)

// Open connects to ClickHouse, optionally runs DDL (if AutoSchema=true),
// starts the background batch-flush goroutine.
// ctx controls the connection and DDL timeout.
func (c *Core) Open(ctx context.Context) error

// Shutdown signals the background goroutine to stop, drains the channel,
// flushes remaining rows, and closes the ClickHouse connection.
// Respects ctx deadline.
func (c *Core) Shutdown(ctx context.Context) error

// DroppedCount returns the total log entries dropped due to a full buffer.
// Use this to detect back-pressure. Expose via a metric in production.
func (c *Core) DroppedCount() int64
```

---

### `batch.go` — `batchWriter`

```go
type batchWriter struct {
    ch      chan logRow
    conn    driver.Conn
    cfg     Config
    wg      sync.WaitGroup
    stop    chan struct{}
    dropped *atomic.Int64  // pointer to Core.dropped
}
```

`run()` is the background goroutine body (described in the data flow section above).

`flush(rows []logRow)`:
1. `conn.PrepareBatch(ctx, "INSERT INTO <table>")`
2. For each row: `batch.AppendStruct(&row)`
3. `batch.Send()`
4. On error: `fmt.Fprintf(os.Stderr, "[ion/clickhouse] flush error: %v\n", err)` — batch is dropped, loop continues. This is consistent with ion's failure isolation design: a backend failure never crashes or stalls the service.

`flushSync()` — called by `Sync()`: drains what's currently in the channel into a local slice, then flushes. Used for orderly shutdown and testing.

`Shutdown(ctx context.Context) error`:
1. Close `stop` channel → signals `run()` to exit
2. `wg.Wait()` with `ctx` deadline
3. Return ctx error if deadline exceeded

---

### `row.go` — `logRow` + `extractRow()`

```go
type logRow struct {
    Timestamp  time.Time
    Level      string
    Service    string
    Version    string
    Logger     string
    Message    string
    TraceID    string
    SpanID     string
    RequestID  string
    UserID     string
    Caller     string
    StrFields  map[string]string
    IntFields  map[string]int64
    FltFields  map[string]float64
    BoolFields map[string]uint8
    Extra      string  // JSON-encoded bag for complex/unknown field types
}
```

`extractRow(entry zapcore.Entry, fields []zapcore.Field) logRow`:

Iterates `fields` once. For each `zapcore.Field`:

```
Key == core.SentinelKey ("__ion_ctx__")     → skip (internal context carrier)
strings.HasPrefix(key, core.SystemFieldPrefix) → skip (any other internal field)

Key == "trace_id"    → row.TraceID
Key == "span_id"     → row.SpanID
Key == "request_id"  → row.RequestID
Key == "user_id"     → row.UserID
Key == "service"     → row.Service (overrides entry default if caller sets it)
Key == "version"     → row.Version

zapcore.StringType                              → StrFields[key]
zapcore.Int64Type / Int32Type / Int16Type / Int8Type
                                                → IntFields[key]
zapcore.Uint64Type / Uint32Type / Uint16Type / Uint8Type
                                                → IntFields[key] (cast to int64)
zapcore.Float64Type / Float32Type               → FltFields[key]
zapcore.BoolType                                → BoolFields[key] (0 or 1)
zapcore.ErrorType                               → Extra["<key>"] = "<err.Error()>" (string)
zapcore.ReflectType / all others                → Extra["<key>"] = json.Marshal(field.Interface)
```

`Extra` is always a single flat JSON object where each key is the field's key name and each value is
the serialized representation of that field. Example with two overflow fields:

```json
{"db_error": "connection refused", "request": {"method": "GET", "path": "/api/v1"}}
```

`ErrorType` values are stored as plain strings (the `.Error()` string). `ReflectType` values are
stored as whatever `json.Marshal` produces for that Go value (nested objects are valid).
An empty `Extra` (no overflow fields) is stored as `""` — not `"{}"` — to save space and make
`WHERE extra != ''` a clean "has overflow fields" filter. Marshalling failures for a single field
drop that field's key from `Extra` silently; other fields in the same entry are unaffected.

Service and Version come from the Zap logger's initial fields (set via `zap.Fields(...)` in `buildZapOptions`). These arrive as `zapcore.StringType` fields with keys `"service"` and `"version"`. The extraction logic handles them as dedicated column writes.

---

### `schema.go` — DDL + `EnsureSchema()`

```sql
CREATE TABLE IF NOT EXISTS ion_logs
(
    timestamp   DateTime64(9, 'UTC'),
    level       LowCardinality(String),
    service     LowCardinality(String),
    version     LowCardinality(String),
    logger      String,
    message     String,
    trace_id    String,
    span_id     String,
    request_id  String,
    user_id     String,
    caller      String,
    str_fields  Map(String, String),
    int_fields  Map(String, Int64),
    flt_fields  Map(String, Float64),
    bool_fields Map(String, UInt8),
    extra       String
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (service, level, timestamp)
TTL timestamp + INTERVAL 30 DAY DELETE
SETTINGS index_granularity = 8192;
```

**Schema design rationale (for future LLMs reading this):**

- `LowCardinality(String)` on `level`, `service`, `version` — these fields repeat identically across millions of rows. ClickHouse applies dictionary encoding, reducing storage ~10x and enabling faster `WHERE level = 'error'` scans.
- `DateTime64(9, 'UTC')` — nanosecond precision matches Go's `time.Time`. Always UTC to avoid timezone ambiguity.
- `ORDER BY (service, level, timestamp)` — the primary sparse index is built on this key. This is optimised for the dominant access pattern: "give me ERROR logs for service X in the last hour." The `(service, level)` prefix eliminates rows before the timestamp range scan.
- `PARTITION BY toYYYYMM(timestamp)` — monthly partitions allow `ALTER TABLE DROP PARTITION '202501'` for cheap data expiry without scanning the whole table. Required for the TTL to work efficiently.
- `TTL timestamp + INTERVAL 30 DAY DELETE` — automatic deletion at the partition level. Teams managing their own schema can change this interval.
- `Map(String, T)` per type — preserves numeric types for `WHERE int_fields['block_height'] > 19000000` without `CAST`. A single `Map(String, String)` would require string-to-number coercion at query time and would break index pushdown.
- `extra String` — escape hatch for complex Go types (`structs`, `errors`, `interfaces`). Stored as JSON. Queryable with ClickHouse JSON extraction functions but not indexed. Acceptable because complex-type fields are rare in practice.

`EnsureSchema(ctx context.Context, conn driver.Conn, cfg Config) error` — executes the DDL above with the configured `cfg.Table` name substituted in. Called by `Open()` when `cfg.AutoSchema == true`.

---

## Error Handling Matrix

| Scenario | Where it happens | Behaviour |
|----------|-----------------|-----------|
| Invalid DSN / config | `New()` | Returns error immediately. Application decides. |
| ClickHouse unreachable at `Open()` | `Open()` | Returns error. Application can start without CH sink. |
| ClickHouse down during flush | `batchWriter.flush()` | Prints to stderr. Drops the batch. Continues. |
| Buffer full (`ChannelBuffer` exhausted) | `Write()` | Increments `dropped` counter. Returns `nil`. Never blocks. |
| `Shutdown()` context deadline exceeded | `Shutdown()` | Returns `ctx.Err()`. Remaining buffered rows are lost. |
| `With()` on a closed Core | `With()` | Returns new Core (With does not touch the connection). Safe. |
| Malformed row (json.Marshal fails) | `extractRow()` | The failing field's key is omitted from `extra`. Other fields still written. |

---

## Performance & Memory Notes

**Memory ceiling:**
- Each `logRow` has 4 map fields. An empty `logRow` is ~400 bytes on the heap (map headers).
- A `logRow` with 5 typical fields (2 strings, 2 ints, 1 float) is ~700–900 bytes.
- Default `ChannelBuffer = 10000` → peak heap from the channel: ~7–10 MB.
- Tuning: if memory is tight, reduce `ChannelBuffer`. If drops are observed, increase it or reduce `FlushInterval`.

**CPU cost per log entry (in caller goroutine):**
- One O(N) pass over fields (N = number of fields per log call, typically 2–8).
- Map insertions (amortised O(1)).
- `json.Marshal` only for `AnyType`/`ReflectType` fields — not called for typed primitives.
- No lock contention: `Write()` uses a channel send (lock-free in most cases with buffered channels).

**ClickHouse insert efficiency:**
- Native binary protocol: `clickhouse-go/v2` sends an entire batch as one binary-encoded payload. For 1,000 rows this is approximately one TCP write — not 1,000 round trips.
- ClickHouse is designed for bulk columnar inserts. Single-row inserts destroy performance. The batch writer pattern is **not optional** — it's the reason this architecture works.

**Signals to watch in production:**
| Signal | Implication | Tuning action |
|--------|-------------|---------------|
| `DroppedCount()` rising | Buffer filling faster than ClickHouse can flush | Decrease `FlushInterval` or increase `BatchSize` + `ChannelBuffer` |
| Flush errors in stderr | ClickHouse unreachable or auth failure | Fix connectivity; rows during downtime are lost |
| Memory growth | `ChannelBuffer` too large for average row size | Decrease `ChannelBuffer` |
| High CPU | `AnyType` fields being JSON-marshalled on hot path | Prefer typed constructors (`ion.String`, `ion.Int64`) over `ion.F` with structs |

---

## Usage (Standalone, Before Ion Wiring)

```go
package main

import (
    "context"
    "time"

    "github.com/JupiterMetaLabs/ion/clickhouse"
    "go.uber.org/zap"
    "go.uber.org/zap/zapcore"
)

func main() {
    ctx := context.Background()

    // 1. Build the ClickHouse core
    chCore, err := clickhouse.New(clickhouse.Config{
        DSN:           "clickhouse://localhost:9000/default",
        Table:         "ion_logs",
        BatchSize:     500,
        FlushInterval: 3 * time.Second,
    })
    if err != nil {
        panic(err)
    }
    if err := chCore.Open(ctx); err != nil {
        // ClickHouse unavailable — decide: fail hard or continue without it
        panic(err)
    }
    defer chCore.Shutdown(ctx)

    // 2. Attach to any *zap.Logger via zapcore.NewTee
    consoleCore := zapcore.NewCore(
        zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
        zapcore.Lock(os.Stdout),
        zapcore.InfoLevel,
    )
    logger := zap.New(zapcore.NewTee(consoleCore, chCore))

    // 3. Log normally — both cores receive the entry
    logger.Info("block validated",
        zap.String("validator", "0xabc"),
        zap.Int64("block_height", 19_500_000),
        zap.Float64("latency_ms", 12.5),
    )

    // At shutdown: chCore.Shutdown(ctx) flushes remaining rows
}
```

---

## Future: Connecting to `ion.New()`

When this package is validated, wiring it into ion requires only:

**`internal/config/config.go`** — add `ClickHouseConfig` struct (follows `FileConfig` pattern).

**`config.go`** (public package) — add type alias and `WithClickHouse(dsn string)` fluent builder.

**`internal/core/logger_factory.go`** — inside `NewZapLogger()`, after the file core block:
```go
if cfg.ClickHouse.Enabled {
    chCore, err := clickhouse.New(cfg.ClickHouse)
    // ... open, handle error as warning (like OTEL)
    cores = append(cores, clickhouse.NewFilteringWrapper(chCore, SentinelKey))
}
```

**`ion.go`** — add shutdown of the ClickHouse core in `Ion.Shutdown()` if it holds a closeable resource (or delegate via `zapLogger.Shutdown()` → `core.LogProvider` pattern).

No changes to `Logger` interface. No changes to `*Ion` struct public API.

---

## Files to Create

| File | Key responsibility |
|------|--------------------|
| `clickhouse/config.go` | `Config` struct, `Validate()`, `withDefaults()` |
| `clickhouse/core.go` | `Core` struct, `zapcore.Core` impl, `New()`, `Open()`, `Shutdown()`, `DroppedCount()` |
| `clickhouse/batch.go` | `batchWriter`: channel, `run()`, `flush()`, `flushSync()`, `Shutdown()` |
| `clickhouse/row.go` | `logRow` struct, `extractRow()`, field type mapping logic |
| `clickhouse/schema.go` | DDL const, `EnsureSchema()` |
| `clickhouse/core_test.go` | Unit tests for all components |
| `clickhouse/integration_test.go` | Integration tests, skipped without `CLICKHOUSE_TEST_DSN` env var |

**Dependency to add to `go.mod`:**
```
github.com/ClickHouse/clickhouse-go/v2
```

No other new dependencies.

---

## Verification Plan

1. `go build ./clickhouse/...` — confirms the package compiles cleanly with no ion wiring yet.
2. `go test -v -race ./clickhouse/...` — unit tests pass; integration tests skip cleanly without the env var.
3. `CLICKHOUSE_TEST_DSN="clickhouse://localhost:9000" go test -v -race ./clickhouse/...` — integration tests run against a real instance and verify rows appear in the table with correct column values.
4. Manual check: write 10,000 entries, call `Shutdown()`, query `SELECT count() FROM ion_logs` → must equal 10,000. `DroppedCount()` must be 0 under normal conditions.
5. Stress test: fill channel (set `ChannelBuffer=10`), verify `DroppedCount()` increments and `Write()` never blocks the goroutine.
