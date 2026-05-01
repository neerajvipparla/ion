# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Test (race detector + coverage, all packages)
make test
# or directly:
go test -v -race -cover ./...

# Run a single test
go test -v -run TestName ./...

# Lint (golangci-lint must be installed)
make lint

# Format
make fmt

# Dependencies
make deps
```

## Architecture

Ion is a Go observability library. The module path is `github.com/JupiterMetaLabs/ion`.

### Public API (stable, `internal/*` has no guarantees)

| File | Role |
|------|------|
| `ion.go` | `*Ion` struct — root observability instance; `New()`, `Child()`, `Named()`, `With()`, `Shutdown()` |
| `logger.go` | `Logger` interface |
| `logger_impl.go` | `zapLogger` struct — internal Zap wrapper, `prepareFields()`, field conversion |
| `config.go` | Type aliases into `internal/config`; `Default()`, `Development()` builders |
| `fields.go` | `Field` struct and typed constructors (`String`, `Int64`, `Err`, etc.) |
| `context.go` | Context helpers (`WithRequestID`, `extractContextZapFields`, etc.) |
| `tracer.go` | `Tracer`/`Span` interfaces; `noopTracer` |
| `attrs.go` | `Attr` type alias, OTEL status constants |
| `fields/blockchain.go` | Domain-specific field constructors (`TxHash`, `BlockHeight`, etc.) |
| `middleware/ionhttp/` | HTTP middleware for context propagation |
| `middleware/iongrpc/` | gRPC stats handler for context propagation |

### Internal packages

| Package | Role |
|---------|------|
| `internal/config/config.go` | All config structs (`Config`, `OTELConfig`, `FileConfig`, …), `Validate()`, `Default()`, `Development()`, `NewFileWriter()` |
| `internal/core/logger_factory.go` | `NewZapLogger()` — assembles all Zap cores (console, file, OTEL) into a `zapcore.Tee` |
| `internal/core/otel.go` | `SetupLogProvider()`, `SetupTracerProvider()`, OTLP exporter construction, endpoint/auth helpers |
| `internal/core/meter.go` | `SetupMeterProvider()`, OTLP metrics exporter construction |
| `internal/core/filter.go` | `filteringCore` — strips internal sentinel keys from log output |
| `internal/core/enforcer.go` | `levelEnforcer` — overrides a core's level check (needed for the OTEL zap bridge which defaults to Info) |
| `internal/core/constants.go` | `SentinelKey = "__ion_ctx__"`, `SystemFieldPrefix = "__ion_"` |

### Log pipeline (critical to understand before modifying)

```
ion.Info(ctx, msg, fields...)
  └─ zapLogger.prepareFields(ctx, fields)
       ├─ toZapFields(fields)           // Field → zap.Field, zero-alloc for primitives
       └─ extractContextZapFields(ctx)  // trace_id, span_id, request_id, user_id
            + zap.Reflect(SentinelKey, ctx)  // carries raw ctx into the core for OTEL bridge
  └─ zap.Logger.Info(msg, zapFields...)
       └─ zapcore.Tee [
            filteringCore(consoleCore)      // strips SentinelKey before writing to stdout/stderr
            filteringCore(fileCore)         // strips SentinelKey before writing to file
            filteringCore(levelEnforcer(otelzapCore))  // OTEL bridge uses SentinelKey to
         ]                                              //  extract TraceID/SpanID before it's stripped
```

**`SentinelKey` (`"__ion_ctx__"`)** is the mechanism for passing `context.Context` through Zap's field system so the `otelzap` bridge can call `trace.SpanContextFromContext()` inside the core, while `filteringCore` prevents the raw context from leaking into console/file output.

### Adding a new log sink (the pattern to follow for ClickHouse)

All sinks are assembled in `internal/core/logger_factory.go` inside `NewZapLogger()`. The `cores` slice is built and passed to `zapcore.NewTee`. Steps:

1. Add config struct fields to `internal/config/config.go` (follow `FileConfig` pattern).
2. Alias the new type in `config.go` (public package), add a fluent builder on `Config`.
3. Create `internal/core/<sink>.go` implementing or wrapping `zapcore.Core`.
4. In `NewZapLogger()`: build the core if enabled, wrap it in `NewFilteringCore(core, SentinelKey)`, append to `cores`.
5. If the sink needs graceful shutdown (connections, flushers), add it to `ZapFactoryResult` and call shutdown in `zapLogger.Shutdown()`.
6. Wire any new config-level minimum-level calculation into the `minLevel` block in `NewZapLogger()`.

`filteringCore` **must** wrap every new core to strip the sentinel; otherwise raw `context.Context` values appear in output.

### `*Ion` vs `Logger` vs `zapLogger`

- `zapLogger` — concrete, unexported. Owns the `*zap.Logger`, `zap.AtomicLevel`, and `*core.LogProvider`. Implements `Logger`.
- `*Ion` — exported. Embeds `*zapLogger` (promoting all `Logger` methods). Also holds `tracerProvider` and `meterProvider`. The concrete type behind every `Logger` interface value returned by the public API.
- `Child()` returns `*Ion` directly (caller needs Tracer/Meter). `Named()`/`With()` return `Logger` (interface) — internally still `*Ion`.
- All children share the same `zap.AtomicLevel` pointer → `SetLevel()` on any instance propagates everywhere.
- Only the root `*Ion` from `New()` should be shut down. `Shutdown()` on a child tears down shared providers.

### `Critical()` — no-exit Fatal

`Critical()` calls `zap.Fatal()` but `New()` installs `zap.WithFatalHook(noExitHook{})`, which is a no-op hook. This emits a `FATAL`-level log entry and returns control to the caller — it never calls `os.Exit`.

### Config inheritance

`Tracing` and `Metrics` configs inherit `Endpoint`, `Protocol`, `Insecure`, `Username`, `Password`, `Headers`, `Timeout`, `BatchSize`, and `ExportInterval` from `OTEL` config when their own values are empty. This inheritance is applied in `ion.go` `New()` before calling the setup functions.

## Linting

golangci-lint v2 config is in `.golangci.yml`. Active linters: `govet`, `ineffassign`, `unused`, `nolintlint`, `staticcheck`, `errcheck`, `gosec`. Every `//nolint` directive must name the specific linter and include a reason comment. Import ordering enforced by `goimports` with local prefix `github.com/JupiterMetaLabs/ion`.
