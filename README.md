# Ion

**Ion** is an enterprise-grade observability client for Go services. It unifies **structured logging** (Zap), **distributed tracing**, and **metrics** (OpenTelemetry) into a single, cohesive API designed for high-throughput, long-running infrastructure.

> **Status:** v0.3 — Pre-release (API stable, targeting v1.0)
> **Target:** Microservices, Blockchain Nodes, Distributed Systems

---

## Guarantees & Design Invariants

Ion is built on strict operational guarantees. Operators can rely on these invariants in production:

1.  **No Process Termination**: Ion will **never** call `os.Exit`, `panic`, or `log.Fatal`. Even `Critical` level logs are strictly informational (mapped to FATAL severity) and guarantee control flow returns to the caller.
2.  **Thread Safety**: All public APIs on `Logger`, `Tracer`, and `Meter` are safe for concurrent use by multiple goroutines.
3.  **Non-Blocking Telemetry**: Trace and metrics export is asynchronous and decoupled from application logic. A slow OTEL collector will never block your business logic. Logs are synchronous to properly handle crash reporting, but rely on high-performance buffered writes.
4.  **Failure Isolation**: Telemetry backend failures (e.g., Collector down) are isolated. They may result in data loss (dropped spans) but will **never** crash the service.

## Non-Goals

To maintain focus and stability, Ion explicitly avoids:

*   **Alerting**: Ion emits signals; it does not manage thresholds or paging.
*   **Framework Magic**: Ion does not auto-inject into HTTP handlers without explicit middleware usage.

---

## Installation

```bash
go get github.com/JupiterMetaLabs/ion
```

Requires Go 1.24+.

---

## Quick Start

A minimal, correct example for a production service.

```go
package main

import (
    "context"
    "log"
    "time"

    "github.com/JupiterMetaLabs/ion"
)

func main() {
    ctx := context.Background()

    // 1. Initialize with Service Identity
    app, warnings, err := ion.New(ion.Default().WithService("payment-node"))
    if err != nil {
        log.Fatalf("Fatal: failed to init observability: %v", err)
    }
    for _, w := range warnings {
        log.Printf("Ion Startup Warning: %v", w)
    }

    // 2. Establish the Lifecycle Contract — flush before exit
    defer func() {
        shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        if err := app.Shutdown(shutdownCtx); err != nil {
            log.Printf("Shutdown data loss: %v", err)
        }
    }()

    // 3. Application Logic
    app.Info(ctx, "node started", ion.String("version", "1.0.0"))
    doWork(ctx, app)
}

func doWork(ctx context.Context, logger ion.Logger) {
    logger.Info(ctx, "processing block", ion.Uint64("height", 100))
}
```

---

## Scoped Observability

Create scoped children that retain full access to logging, tracing, and metrics. This is the recommended pattern for structuring observability in multi-component applications.

```go
app, _, _ := ion.New(cfg)

// Child() returns *Ion with full capabilities
http := app.Child("http")
http.Info(ctx, "request received")

// Full tracing access — no type assertion needed
tracer := http.Tracer("http.handler")
ctx, span := tracer.Start(ctx, "HandleRequest")
defer span.End()

// Full metrics access
meter := http.Meter("http.metrics")
counter, _ := meter.Int64Counter("http.requests.total")
counter.Add(ctx, 1)
```

`Named()` and `With()` also preserve observability (the concrete type behind the `Logger` interface is `*Ion`), but `Child()` returns `*Ion` directly — no type assertion required. Use `Child()` when your component needs tracing or metrics. Use `Named()`/`With()` when passing through the `Logger` interface (e.g., across package boundaries).

---

## Developer Reference

### Core Types

| Type | Description |
|------|-------------|
| `*Ion` | Root observability instance. Provides `Logger` + `Tracer()` + `Meter()` + `Shutdown()`. |
| `Logger` | Interface for structured logging. All methods require `context.Context`. |
| `Tracer` | Interface for creating spans. Obtained via `Ion.Tracer(name)`. |
| `Span` | Represents a unit of work. Call `End()` when done. |
| `Field` | Structured key-value pair for log entries. Zero-allocation for primitives. |
| `Config` | Complete configuration struct. Use `Default()` or `Development()` as starting points. |

### Creating Children

| Method | Returns | Use When |
|--------|---------|----------|
| `Child(name, fields...)` | `*Ion` | Component needs logging + tracing + metrics. **Recommended.** |
| `Named(name)` | `Logger` | Passing through `Logger` interface; only logging needed at the call site. |
| `With(fields...)` | `Logger` | Attaching permanent fields; only logging needed at the call site. |

All three preserve full observability. The difference is the return type — `Child()` gives you `*Ion` directly, while `Named()`/`With()` return `Logger` (backed by `*Ion` internally).

### Field Constructors

Ion provides typed field constructors for zero-allocation structured logging:

| Constructor | Type | Example |
|-------------|------|---------|
| `ion.String(key, val)` | `string` | `ion.String("user", "alice")` |
| `ion.Int(key, val)` | `int` | `ion.Int("port", 8080)` |
| `ion.Int64(key, val)` | `int64` | `ion.Int64("offset", 1024)` |
| `ion.Uint64(key, val)` | `uint64` | `ion.Uint64("block_height", 19500000)` |
| `ion.Float64(key, val)` | `float64` | `ion.Float64("latency_ms", 12.5)` |
| `ion.Bool(key, val)` | `bool` | `ion.Bool("success", true)` |
| `ion.Duration(key, val)` | `time.Duration` | `ion.Duration("elapsed", 50*time.Millisecond)` |
| `ion.Err(err)` | `error` | `ion.Err(err)` (key is always `"error"`) |
| `ion.F(key, val)` | `any` | `ion.F("data", myStruct)` — auto-detects type |

For blockchain-specific fields, see [`fields` package](#blockchain-fields).

### Context Helpers

Ion extracts trace correlation from `context.Context` automatically. You can also inject custom identifiers:

| Function | Description |
|----------|-------------|
| `ion.WithRequestID(ctx, id)` | Adds `request_id` to all logs from this context. |
| `ion.WithUserID(ctx, id)` | Adds `user_id` to all logs from this context. |
| `ion.WithTraceID(ctx, id)` | Manual trace ID for non-OTEL scenarios. |
| `ion.TraceIDFromContext(ctx)` | Extracts trace ID (OTEL span or manual). |
| `ion.RequestIDFromContext(ctx)` | Extracts request ID. |
| `ion.UserIDFromContext(ctx)` | Extracts user ID. |

### Log Levels

| Level | Method | Behavior |
|-------|--------|----------|
| `debug` | `Debug(ctx, msg, fields...)` | Verbose development info. Disabled in production by default. |
| `info` | `Info(ctx, msg, fields...)` | Operational state changes. Default minimum level. |
| `warn` | `Warn(ctx, msg, fields...)` | Recoverable issues. Routed to stderr when `ErrorsToStderr` is true. |
| `error` | `Error(ctx, msg, err, fields...)` | Actionable failures. Accepts an `error` parameter. |
| `fatal` | `Critical(ctx, msg, err, fields...)` | Highest severity. **Does NOT exit.** Safe for libraries. |

Levels can be changed at runtime via `SetLevel("debug")`. Changes propagate to all children sharing the same atomic level.

---

## API Overview

### 1. The Logger

Use the Logger for human-readable events, state changes, and errors. Always pass `context.Context` — even `context.Background()` — to maintain the API contract and enable future trace correlation.

```go
// INFO: Operational state changes
app.Info(ctx, "transaction processed",
    ion.String("tx_id", "0x123"),
    ion.Duration("latency", 50*time.Millisecond),
)

// ERROR: Actionable failures. Does not interrupt flow.
if err != nil {
    app.Error(ctx, "database connection failed", err, ion.String("db_host", "primary"))
}

// CRITICAL: Invariant violations (e.g. data corruption).
// GUARANTEE: Does NOT call os.Exit(). Safe to use in libraries.
app.Critical(ctx, "memory corruption detected", nil)
```

### 2. Child Loggers (Scopes)

Use `Child`, `Named`, and `With` to create context-aware sub-loggers. Prefer `Child()` when the component needs tracing or metrics.

```go
// Child: Full observability — logging, tracing, metrics
http := app.Child("http", ion.String("version", "v2"))
tracer := http.Tracer("http.handler")
meter := http.Meter("http.metrics")

// Named: Logger interface — good for cross-package boundaries
httpLog := app.Named("http")   // {"logger": "app.http", ...}
grpcLog := app.Named("grpc")   // {"logger": "app.grpc", ...}

// With: Permanent fields on all subsequent logs
userLogger := app.With(
    ion.Int("user_id", 42),
    ion.String("tenant", "acme-corp"),
)
userLogger.Info(ctx, "action taken")  // {"user_id": 42, "tenant": "acme-corp", ...}
```

### 3. The Tracer

Use the Tracer for latency measurement and causal chains. Every `Start` **must** have a corresponding `End()`.

```go
func ProcessOrder(ctx context.Context, orderID string) error {
    tracer := app.Tracer("order.processor")
    ctx, span := tracer.Start(ctx, "ProcessOrder")
    defer span.End()

    span.SetAttributes(attribute.String("order.id", orderID))

    if err := validate(ctx); err != nil {
        span.RecordError(err)
        span.SetStatus(ion.StatusError, "validation failed")
        return err
    }
    return nil
}
```

Ion provides `StatusOK`, `StatusError`, and `StatusUnset` constants so you don't need to import `go.opentelemetry.io/otel/codes`. For span attributes, `ion.Attr` is an alias for `attribute.KeyValue` — import `go.opentelemetry.io/otel/attribute` to create attribute values.

### 4. The Meter

Use Meter for operational metrics (counters, histograms, gauges).

```go
meter := app.Meter("http.metrics")
requestCounter, _ := meter.Int64Counter("http_requests_total")
latencyHist, _ := meter.Float64Histogram("http_request_duration_seconds")

requestCounter.Add(ctx, 1)
latencyHist.Record(ctx, 0.025) // 25ms
```

### 5. Blockchain Fields

The `fields` sub-package provides domain-specific constructors with consistent key naming:

```go
import "github.com/JupiterMetaLabs/ion/fields"

app.Info(ctx, "transaction routed",
    fields.TxHash("0xabc123..."),
    fields.ShardID(3),
    fields.Slot(150_000_000),
    fields.Epoch(350),
    fields.BlockHeight(19_500_000),
    fields.LatencyMs(12.5),
)
```

Categories: Transaction (`TxHash`, `TxType`, `Nonce`, `GasUsed`, ...), Block & Consensus (`BlockHeight`, `Slot`, `Epoch`, `Validator`, ...), Network (`ChainID`, `PeerID`, `NodeID`, ...), and Metrics (`Count`, `Size`, `LatencyMs`, ...).

---

## Configuration Reference

Ion uses a comprehensive configuration struct for behavior control. This maps 1:1 with `ion.Config`.

### Root Configuration (`ion.Config`)

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `Level` | `string` | `"info"` | Minimum log level (`debug`, `info`, `warn`, `error`, `fatal`). |
| `Development` | `bool` | `false` | Enables development mode (pretty output, caller location, stack traces). |
| `ServiceName` | `string` | `"unknown"` | Identity of the service (vital for trace attribution). |
| `Version` | `string` | `""` | Service version (e.g., commit hash or semver). |
| `Console` | `ConsoleConfig` | `Enabled: true` | Configuration for stdout/stderr. |
| `File` | `FileConfig` | `Enabled: false` | Configuration for file logging (with rotation). |
| `OTEL` | `OTELConfig` | `Enabled: false` | Configuration for remote OpenTelemetry logging. |
| `Tracing` | `TracingConfig` | `Enabled: false` | Configuration for distributed tracing. |
| `Metrics` | `MetricsConfig` | `Enabled: false` | Configuration for OpenTelemetry metrics. |

### Console Configuration (`ion.ConsoleConfig`)

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `Enabled` | `bool` | `true` | If false, stdout/stderr is silenced. |
| `Format` | `string` | `"json"` | `"json"`, `"pretty"`, or `"systemd"` (optimized for Journald). |
| `Color` | `bool` | `true` | Enables ANSI colors (only applies to `pretty` format). |
| `ErrorsToStderr` | `bool` | `true` | Writes `warn`/`error`/`fatal` to stderr, others to stdout. |
| `Level` | `string` | `""` | Optional override for console log level. Inherits global level if empty. |

### File Configuration (`ion.FileConfig`)

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `Enabled` | `bool` | `false` | Enables file writing. |
| `Path` | `string` | `""` | Absolute path to the log file (e.g., `/var/log/app.log`). |
| `MaxSizeMB` | `int` | `100` | Max size per file before rotation. |
| `MaxBackups` | `int` | `5` | Number of old files to keep. |
| `MaxAgeDays` | `int` | `7` | Max age of files to keep. |
| `Compress` | `bool` | `true` | Gzip old log files. |
| `Level` | `string` | `""` | Optional override for file log level. Inherits global level if empty. |

### OTEL Configuration (`ion.OTELConfig`)

Controls the OpenTelemetry **Logs** Exporter. Tracing and Metrics inherit `Endpoint`, `Protocol`, and auth fields from OTEL when their own values are empty.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `Enabled` | `bool` | `false` | Enables log export to Collector. |
| `Endpoint` | `string` | `""` | `host:port` or URL. URL schemes override `Insecure` setting. |
| `Protocol` | `string` | `"grpc"` | `"grpc"` (recommended) or `"http"`. |
| `Insecure` | `bool` | `false` | Disables TLS (dev only). Ignored if Endpoint starts with `https://`. |
| `Username` | `string` | `""` | Basic Auth username. (Fallback for VPC) |
| `Password` | `string` | `""` | Basic Auth password. (Fallback for VPC) |
| `BatchSize` | `int` | `512` | Max logs per export batch. |
| `ExportInterval` | `Duration` | `5s` | Flush interval. |
| `Level` | `string` | `""` | Optional override for OTEL log level. |

### Tracing Configuration (`ion.TracingConfig`)

Controls the OpenTelemetry **Trace** Provider. Empty fields inherit from `OTELConfig`.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `Enabled` | `bool` | `false` | Enables trace generation and export. |
| `Endpoint` | `string` | `""` | `host:port`. Inherits `OTEL.Endpoint` if empty. |
| `Sampler` | `string` | `"ratio:0.1"` | `"always"`, `"never"`, or `"ratio:0.X"`. Development mode uses `"always"`. |
| `Protocol` | `string` | `"grpc"` | Inherits `OTEL.Protocol` if empty. |
| `Username` | `string` | `""` | Inherits `OTEL.Username` if empty. |
| `Password` | `string` | `""` | Inherits `OTEL.Password` if empty. |

### Metrics Configuration (`ion.MetricsConfig`)

Controls the OpenTelemetry **Metrics** Provider (OTLP Push). Empty fields inherit from `OTELConfig`.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `Enabled` | `bool` | `false` | Enables metrics export. |
| `Endpoint` | `string` | `""` | `host:port`. Inherits `OTEL.Endpoint` if empty. |
| `Interval` | `Duration` | `15s` | Push interval. Development mode uses `5s`. |
| `Temporality` | `string` | `"cumulative"` | `"cumulative"` (Prometheus-compatible) or `"delta"`. |
| `Protocol` | `string` | `"grpc"` | Inherits `OTEL.Protocol` if empty. |
| `Username` | `string` | `""` | Inherits `OTEL.Username` if empty. |
| `Password` | `string` | `""` | Inherits `OTEL.Password` if empty. |

### Config Builders

For quick setup, use the fluent builder methods:

```go
// Production: Console + OTEL + Tracing on a shared collector
cfg := ion.Default().
    WithService("order-service").
    WithOTEL("otel-collector:4317").
    WithTracing("").  // inherits OTEL endpoint
    WithMetrics("")   // inherits OTEL endpoint

// Development: Pretty console, debug level, sample all traces
cfg := ion.Development().WithService("order-service")
```

For full control, initialize the struct directly. See [`examples/basic/main.go`](examples/basic/main.go) Example 6 for a complete production configuration.

---

## Initialization Recipes

### Console Only (CLI / Scripts)

```go
cfg := ion.Default()
cfg.Level = "info"
// cfg.Development = true  // Optional: enables caller info + stack traces
```

### Console + File (VM / Bare Metal)

```go
cfg := ion.Default()
cfg.File.Enabled = true
cfg.File.Path = "/var/log/app/app.log"
cfg.File.MaxSizeMB = 500
cfg.File.MaxBackups = 5
```

### OTEL Only (High-Traffic Sidecars)

```go
cfg := ion.Default()
cfg.Console.Enabled = false
cfg.OTEL.Enabled = true
cfg.OTEL.Endpoint = "localhost:4317"
cfg.OTEL.Protocol = "grpc"
```

### Systemd Native (Journald)

```go
cfg := ion.Default()
cfg.Console.Format = "systemd"
cfg.Console.ErrorsToStderr = true
```

### Full Configuration with Token Auth

```go
cfg := ion.Default()
cfg.OTEL.Enabled = true
cfg.OTEL.Endpoint = "otel.jmdt.io:443"
cfg.OTEL.Headers = map[string]string{
    "Authorization": "Bearer 89658fc8a43d1a39cde4c59d1e2772194a8e2b533d53f93ca29659f40972980f",
}
cfg.Tracing.Enabled = true
cfg.Metrics.Enabled = true
```

### Full Stack (Kubernetes)

```go
cfg := ion.Default()
cfg.Console.Enabled = true
cfg.OTEL.Enabled = true
cfg.OTEL.Endpoint = "otel-collector:4317"
cfg.Tracing.Enabled = true
cfg.Tracing.Sampler = "ratio:0.1"
cfg.Metrics.Enabled = true
```

---

## Tracing Guide

Distributed tracing requires discipline. For a complete step-by-step guide to span creation, error handling, background goroutines, and best practices, see the **[Tracing Quickstart](docs/TRACING_QUICKSTART.md)**.

---

## HTTP & gRPC Integration

Ion provides middleware and interceptors for automatic context propagation.

### HTTP Middleware

```go
import "github.com/JupiterMetaLabs/ion/middleware/ionhttp"

mux := http.NewServeMux()
handler := ionhttp.Handler(mux, "payment-api")
http.ListenAndServe(":8080", handler)
```

### gRPC Interceptors

```go
import "github.com/JupiterMetaLabs/ion/middleware/iongrpc"

// Server
s := grpc.NewServer(grpc.StatsHandler(iongrpc.ServerHandler()))

// Client
conn, _ := grpc.Dial(addr, grpc.WithStatsHandler(iongrpc.ClientHandler()))
```

---

## Examples

The [`examples/`](examples/) directory contains runnable demonstrations:

| Example | File | What It Shows |
|---------|------|---------------|
| Simple Usage | [`examples/basic/main.go`](examples/basic/main.go) (Example 1) | Minimal setup with `Development()` config. |
| Dependency Injection | [`examples/basic/main.go`](examples/basic/main.go) (Example 2) | `Child()` pattern for component observability. |
| Child Loggers | [`examples/basic/main.go`](examples/basic/main.go) (Example 3) | `Named()` and `With()` for scoped logging. |
| Metrics | [`examples/basic/main.go`](examples/basic/main.go) (Example 4) | Counter and histogram instrumentation. |
| Blockchain Fields | [`examples/basic/main.go`](examples/basic/main.go) (Example 5) | Domain-specific field helpers. |
| Production Setup | [`examples/basic/main.go`](examples/basic/main.go) (Example 6) | Full config with tracing, file rotation, graceful shutdown. |
| OTEL + Jaeger | [`examples/otel-test/main.go`](examples/otel-test/main.go) | End-to-end trace correlation with Docker Compose. |
| Benchmarks | [`examples/benchmark/main.go`](examples/benchmark/main.go) | Performance measurement suite. |

---

## Best Practices

**Context propagation.** Always pass `context.Context` to log methods. Using `context.Background()` breaks the trace chain — reserve it for `main()` and background worker roots.

**Shutdown is mandatory.** Failing to call `Shutdown()` guarantees data loss for buffered traces, metrics, and OTEL logs. Always `defer` shutdown in `main()` with a timeout context.

**Use typed fields.** Prefer `ion.String("key", val)` over `ion.F("key", val)`. Typed constructors are zero-allocation and catch type errors at compile time.

**Use static keys.** `ion.String(userInput, "value")` is a security risk and breaks log indexing. Keys must be compile-time constants.

**Use consistent key naming.** Stick to `snake_case` (e.g., `user_id`, not `userID` or `uid`). The `fields` package enforces this for blockchain-specific keys.

**Prefer `Child()` for components.** When a struct needs logging, tracing, and metrics, accept `*ion.Ion` and create children with `Child()`. Use the `Logger` interface at package boundaries where only logging is needed.

**Only shut down the root.** Child instances share providers with the parent. Calling `Shutdown()` on a child tears down shared resources. Shut down only the root `*Ion` returned by `New()`.

**Handle warnings.** `New()` returns warnings for non-fatal issues (e.g., OTEL connection failure). Log these at startup so operators know when telemetry is degraded.

---

## Operational Model

### How Ion Works

*   **Logs**: Emitted **synchronously** to configured cores (Console/File/OTEL). If your application crashes immediately after a log statement, the log is persisted (up to OS buffering).
*   **Traces**: Buffered and exported **asynchronously**. Spans are batched in memory and sent to the OTEL endpoint on a timer or size threshold.
*   **Metrics**: Pushed **asynchronously** via OTLP at the configured interval (default 15s).
*   **Correlation**: `trace_id` and `span_id` are extracted from `context.Context` at the moment of logging.

### Production Failure Modes

*   **OTEL Collector Down**: Exporter retries with exponential backoff. If buffers fill, new spans/logs are dropped. Application performance is preserved.
*   **Disk Full (File Logging)**: Lumberjack rotation attempts to write. If the syscall fails, the application continues but file logs are lost.
*   **High Load**: Tracing and metrics use bounded buffers. Under extreme load, excess data is dropped to prevent memory leaks.

---

## Further Reading

*   **[Observability Reference Specification](docs/OBSERVABILITY.md)** — Backend engineering invariants, tiered recommendations, OTel Collector configurations.
*   **[Enterprise User Guide](docs/USER_GUIDE.md)** — Advanced patterns, DI strategies, and blockchain-specific examples.
*   **[Tracing Quickstart](docs/TRACING_QUICKSTART.md)** — Step-by-step guide to setting up distributed tracing.

---

## Versioning

*   **Public API**: `ion.go`, `logger.go`, `config.go`, `fields.go`, `tracer.go`, `attrs.go`, `context.go`. Stable since v0.3.
*   **Internal**: `internal/*`. No stability guarantees.
*   **Behavior**: Log format changes or configuration defaults are considered breaking changes.

---

## License

[MIT](LICENSE) © 2025 JupiterMeta Labs

