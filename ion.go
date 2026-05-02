package ion

import (
	"context"
	"fmt"
	"log"
	"time"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/JupiterMetaLabs/ion/internal/clickhouse"
	"github.com/JupiterMetaLabs/ion/internal/core"
)

// Compile-time interface compliance check.
var _ Logger = (*Ion)(nil)

// Ion is the unified observability instance providing structured logging,
// distributed tracing, and metrics collection.
//
// Ion implements the [Logger] interface, so it can be used anywhere a Logger
// is expected. It also provides access to [Tracer] and [Meter] for complete
// observability.
//
// Child instances created via [Ion.Named], [Ion.With], or [Ion.Child] preserve
// full observability capabilities, including access to Tracer and Meter.
// The [Ion.Child] method is recommended for components that need tracing or
// metrics, as it returns *Ion directly without requiring a type assertion.
//
// Example:
//
//	app, warnings, err := ion.New(cfg)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer app.Shutdown(context.Background())
//
//	// Logging
//	app.Info(ctx, "message", ion.F("key", "value"))
//
//	// Scoped child with full observability
//	http := app.Child("http")
//	http.Info(ctx, "request received")
//	tracer := http.Tracer("http.handler")
//	ctx, span := tracer.Start(ctx, "HandleRequest")
//	defer span.End()
//
//	// Metrics
//	meter := http.Meter("http.metrics")
//	counter, _ := meter.Int64Counter("http.requests.total")
//	counter.Add(ctx, 1)
type Ion struct {
	*zapLogger // Embedded: promotes Debug, Info, Warn, Error, Critical, Sync, SetLevel, GetLevel.
	// Caller depth is unified: all log calls are 1 frame above zap, matching AddCallerSkip(1).
	serviceName    string
	version        string
	tracerProvider *core.TracerProvider
	tracingEnabled bool
	meterProvider  *core.MeterProvider
	metricsEnabled bool
	chCore         *clickhouse.Core
}

// Warning represents a non-fatal initialization issue.
// Ion returns warnings instead of failing when optional components
// (like OTEL or tracing) cannot be initialized.
type Warning struct {
	Component string // "otel", "tracing"
	Err       error
}

// Error implements the error interface, formatting the warning as "component: message".
func (w Warning) Error() string {
	return fmt.Sprintf("%s: %v", w.Component, w.Err)
}

// New creates a new Ion instance with the given configuration.
// This is the single entry point for creating ion observability.
//
// Returns:
//   - *Ion: Always returns a working Ion instance (may use fallbacks)
//   - []Warning: Non-fatal issues (e.g., OTEL connection failed, tracing disabled)
//   - error: Fatal configuration errors
func New(cfg Config) (*Ion, []Warning, error) {
	if err := cfg.Validate(); err != nil {
		return nil, nil, fmt.Errorf("invalid configuration: %w", err)
	}

	var warnings []Warning

	ion := &Ion{
		serviceName: cfg.ServiceName,
		version:     cfg.Version,
	}

	// 1. Setup Logger (Zap + OTEL Logs)
	zapRes, err := core.NewZapLogger(cfg)
	if err != nil {
		// Fatal error if we can't even init Zap (e.g. file error)
		return nil, nil, fmt.Errorf("failed to init logger: %w", err)
	}

	// Construct the logger wrapper
	ion.zapLogger = &zapLogger{
		zap:          zapRes.Logger,
		config:       cfg,
		atomicLvl:    zapRes.AtomicLevel,
		otelProvider: zapRes.OTELProvider,
	}

	// 2. Setup ClickHouse sink
	if cfg.ClickHouse.Enabled {
		chCore, chErr := buildClickHouseCore(cfg)
		if chErr != nil {
			warnings = append(warnings, Warning{
				Component: "clickhouse",
				Err:       fmt.Errorf("failed to init clickhouse: %w (clickhouse disabled)", chErr),
			})
		} else {
			ion.chCore = chCore
			// Inject the CH core into the zap tee without touching internal/core.
			// extractRow already drops the SentinelKey, but we wrap with filteringCore
			// for consistency with the other sinks.
			ion.zapLogger.zap = ion.zapLogger.zap.WithOptions(
				zap.WrapCore(func(existing zapcore.Core) zapcore.Core {
					return zapcore.NewTee(existing, core.NewFilteringCore(chCore, core.SentinelKey))
				}),
			)
		}
	}

	// 3. Setup Tracing (OTEL Traces)
	if cfg.Tracing.Enabled {
		// Use Tracing endpoint or fallback to OTEL endpoint
		if cfg.Tracing.Endpoint == "" {
			cfg.Tracing.Endpoint = cfg.OTEL.Endpoint
		}
		if cfg.Tracing.Protocol == "" {
			cfg.Tracing.Protocol = cfg.OTEL.Protocol
		}
		if !cfg.Tracing.Insecure && cfg.OTEL.Insecure {
			cfg.Tracing.Insecure = true // Inherit insecure if not explicitly set
		}
		if cfg.Tracing.Username == "" {
			cfg.Tracing.Username = cfg.OTEL.Username
		}
		if cfg.Tracing.Password == "" {
			cfg.Tracing.Password = cfg.OTEL.Password
		}
		if cfg.Tracing.Timeout == 0 {
			cfg.Tracing.Timeout = cfg.OTEL.Timeout
		}
		if cfg.Tracing.BatchSize == 0 {
			cfg.Tracing.BatchSize = cfg.OTEL.BatchSize
		}
		if cfg.Tracing.ExportInterval == 0 {
			cfg.Tracing.ExportInterval = cfg.OTEL.ExportInterval
		}
		if cfg.Tracing.Headers == nil && len(cfg.OTEL.Headers) > 0 {
			// Deep copy headers to avoid map reference issues
			cfg.Tracing.Headers = make(map[string]string, len(cfg.OTEL.Headers))
			for k, v := range cfg.OTEL.Headers {
				cfg.Tracing.Headers[k] = v
			}
		}

		tp, err := core.SetupTracerProvider(cfg.Tracing, cfg.ServiceName, cfg.Version)
		if err != nil {
			warnings = append(warnings, Warning{
				Component: "tracing",
				Err:       fmt.Errorf("failed to init tracing: %w (tracing disabled)", err),
			})
		} else if tp != nil {
			ion.tracerProvider = tp
			ion.tracingEnabled = true
		}
	}

	// 4. Setup Metrics (OTEL Metrics)
	if cfg.Metrics.Enabled {
		// Use Metrics endpoint or fallback to OTEL endpoint
		if cfg.Metrics.Endpoint == "" {
			cfg.Metrics.Endpoint = cfg.OTEL.Endpoint
		}
		if cfg.Metrics.Protocol == "" {
			cfg.Metrics.Protocol = cfg.OTEL.Protocol
		}
		if !cfg.Metrics.Insecure && cfg.OTEL.Insecure {
			cfg.Metrics.Insecure = true
		}
		// Auth inheritance
		if cfg.Metrics.Username == "" {
			cfg.Metrics.Username = cfg.OTEL.Username
		}
		if cfg.Metrics.Password == "" {
			cfg.Metrics.Password = cfg.OTEL.Password
		}
		// Metadata inheritance
		if cfg.Metrics.Headers == nil && len(cfg.OTEL.Headers) > 0 {
			cfg.Metrics.Headers = make(map[string]string, len(cfg.OTEL.Headers))
			for k, v := range cfg.OTEL.Headers {
				cfg.Metrics.Headers[k] = v
			}
		}

		mp, err := core.SetupMeterProvider(cfg.Metrics, cfg.ServiceName, cfg.Version)
		if err != nil {
			warnings = append(warnings, Warning{
				Component: "metrics",
				Err:       fmt.Errorf("failed to init metrics: %w (metrics disabled)", err),
			})
		} else if mp != nil {
			ion.meterProvider = mp
			ion.metricsEnabled = true
		}
	}

	return ion, warnings, nil
}

// --- Logger interface implementation (Named/With shadow promoted methods) ---

// Named returns a child Ion instance with a named sub-logger.
// The name appears in logs as the "logger" field (e.g., "http", "grpc").
//
// Unlike calling Named on a bare Logger, the returned value preserves access
// to Tracer, Meter, and full Shutdown orchestration because the concrete type
// behind the Logger interface is *Ion.
//
// To get the *Ion directly without a type assertion, use [Ion.Child] instead.
func (i *Ion) Named(name string) Logger {
	return &Ion{
		zapLogger:      i.namedInternal(name),
		serviceName:    i.serviceName,
		version:        i.version,
		tracerProvider: i.tracerProvider,
		tracingEnabled: i.tracingEnabled,
		meterProvider:  i.meterProvider,
		metricsEnabled: i.metricsEnabled,
		chCore:         i.chCore,
	}
}

// With returns a child Ion instance with additional fields attached to every log entry.
//
// Unlike calling With on a bare Logger, the returned value preserves access
// to Tracer, Meter, and full Shutdown orchestration because the concrete type
// behind the Logger interface is *Ion.
//
// To get the *Ion directly without a type assertion, use [Ion.Child] instead.
func (i *Ion) With(fields ...Field) Logger {
	return &Ion{
		zapLogger:      i.withInternal(fields...),
		serviceName:    i.serviceName,
		version:        i.version,
		tracerProvider: i.tracerProvider,
		tracingEnabled: i.tracingEnabled,
		meterProvider:  i.meterProvider,
		metricsEnabled: i.metricsEnabled,
		chCore:         i.chCore,
	}
}

// Child returns a named child Ion instance, optionally with additional fields.
// This is the recommended way to create scoped observability for application components
// because the return type is *Ion, giving direct access to Tracer, Meter, and Shutdown
// without a type assertion.
//
// Example:
//
//	http := app.Child("http", ion.String("version", "v2"))
//	http.Info(ctx, "request received")
//	tracer := http.Tracer("http.handler")
//	meter := http.Meter("http.metrics")
//
// Child instances share the parent's tracer and meter providers. Calling Shutdown
// on a child will shut down shared providers, affecting the parent and all siblings.
// In most applications, only the root Ion instance should be shut down.
func (i *Ion) Child(name string, fields ...Field) *Ion {
	child := i.namedInternal(name)
	if len(fields) > 0 {
		child = child.withInternal(fields...)
	}
	return &Ion{
		zapLogger:      child,
		serviceName:    i.serviceName,
		version:        i.version,
		tracerProvider: i.tracerProvider,
		tracingEnabled: i.tracingEnabled,
		meterProvider:  i.meterProvider,
		metricsEnabled: i.metricsEnabled,
		chCore:         i.chCore,
	}
}

// --- Tracer access ---

var tracingDisabledLogged bool

// Tracer returns a named tracer for creating spans.
// If tracing is not enabled, returns a no-op tracer (logs warning once).
func (i *Ion) Tracer(name string) Tracer {
	if !i.tracingEnabled || i.tracerProvider == nil {
		if !tracingDisabledLogged {
			tracingDisabledLogged = true
			log.Println("[ion] Tracing disabled: Tracer() returning no-op. Enable via Config.Tracing.Enabled")
		}
		return noopTracer{}
	}
	// core.SetupTracerProvider sets the global OTEL provider,
	// so newOTELTracer(name) which calls otel.Tracer(name) works correctly.
	return newOTELTracer(name)
}

// --- Metrics access ---

// Meter returns a named meter for creating metric instruments (counters, histograms, etc.).
// If metrics are not enabled, returns a no-op meter that silently discards all recordings.
func (i *Ion) Meter(name string, opts ...metric.MeterOption) metric.Meter {
	if !i.metricsEnabled || i.meterProvider == nil {
		return newNoopMeter()
	}
	return i.meterProvider.Meter(name, opts...)
}

// newNoopMeter returns a no-op meter that satisfies the metric.Meter interface
// without recording any data. Used when metrics are disabled.
func newNoopMeter() metric.Meter {
	return noop.NewMeterProvider().Meter("noop")
}

// DroppedCount returns the total number of log entries dropped by the ClickHouse
// sink since startup. A non-zero value means the async write buffer filled up
// faster than ClickHouse could flush — tune BatchSize, FlushInterval, or
// ChannelBuffer to reduce back-pressure.
// Returns 0 if ClickHouse is not enabled.
func (i *Ion) DroppedCount() int64 {
	if i.chCore == nil {
		return 0
	}
	return i.chCore.DroppedCount()
}

// --- Lifecycle ---

// Shutdown gracefully shuts down all observability subsystems in order:
// tracing provider, metrics provider, then the logging backend (including OTEL log export).
//
// The provided context controls the shutdown deadline. Returns the first error
// encountered, but always attempts to shut down all subsystems.
//
// Important: Child instances created via [Ion.Child], [Ion.Named], or [Ion.With]
// share the parent's tracer and meter providers. Calling Shutdown on a child
// tears down shared providers, affecting the parent and all siblings.
// In most applications, only the root Ion instance should be shut down.
func (i *Ion) Shutdown(ctx context.Context) error {
	var firstErr error

	if i.tracerProvider != nil {
		if err := i.tracerProvider.Shutdown(ctx); err != nil {
			firstErr = err
		}
	}

	if i.meterProvider != nil {
		if err := i.meterProvider.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if i.chCore != nil {
		if err := i.chCore.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if i.zapLogger != nil {
		if err := i.zapLogger.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

// buildClickHouseCore constructs and opens a ClickHouse core from ion Config.
// Returns an error (treated as warning by the caller) if the core cannot be started.
func buildClickHouseCore(cfg Config) (*clickhouse.Core, error) {
	// Inherit global level when the ClickHouse-specific level is not set.
	chCfg := cfg.ClickHouse
	if chCfg.Level == "" {
		chCfg.Level = cfg.Level
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	chCore, err := clickhouse.New(ctx, chCfg)
	if err != nil {
		return nil, err
	}
	if err := chCore.Open(ctx); err != nil {
		return nil, err
	}
	return chCore, nil
}
