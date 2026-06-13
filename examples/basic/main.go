package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/neerajvipparla/ion"
	"github.com/neerajvipparla/ion/fields"
)

// ============================================================================
// Example 1: Simple Usage
// Best for: Small apps, scripts, or quick prototypes.
// ============================================================================

func example1_SimpleUsage() {
	ctx := context.Background()

	// Create Ion instance - one entry point for everything
	// Use Development() to see caller and debug logs
	app, warnings, err := ion.New(ion.Development().WithService("example-app"))
	if err != nil {
		log.Fatalf("Failed to create ion: %v", err)
	}
	for _, w := range warnings {
		log.Printf("ion warning: %v", w)
	}
	defer func() { _ = app.Sync() }()

	// Use Ion directly for logging
	app.Info(ctx, "application started")
	app.Debug(ctx, "debug info", ion.String("key", "value"))
	app.Warn(ctx, "something might be wrong")
}

// ============================================================================
// Example 2: Dependency Injection Pattern
// Best for: Libraries, large apps, or teams that prefer explicit dependencies.
// ============================================================================

func example2_DependencyInjection() {
	ctx := context.Background()

	app, _, err := ion.New(ion.Default().WithService("payment-api"))
	if err != nil {
		log.Fatalf("Failed to create ion: %v", err)
	}
	defer func() { _ = app.Sync() }()

	// Pass a scoped child to components — preserves logging, tracing, and metrics
	server := NewServer(app.Child("server"))
	server.Start(ctx)
}

type Server struct {
	app *ion.Ion // Full observability — logging, tracing, and metrics
}

func NewServer(app *ion.Ion) *Server {
	return &Server{app: app}
}

func (s *Server) Start(ctx context.Context) {
	s.app.Info(ctx, "server listening", ion.Int("port", 8080))

	// Child has full access to tracing
	tracer := s.app.Tracer("server.handler")
	ctx, span := tracer.Start(ctx, "Start")
	defer span.End()

	s.app.Info(ctx, "server initialized")
}

// ============================================================================
// Example 3: Child Loggers (With and Named)
// Demonstrates how to scope loggers for specific contexts.
// ============================================================================

func example3_ChildLoggers() {
	ctx := context.Background()
	app, _, _ := ion.New(ion.Default())

	// Named: Adds a "logger" field to identify the component
	httpLog := app.Named("http")
	grpcLog := app.Named("grpc")

	// With: Adds permanent fields to all log entries from this child
	userLogger := app.With(
		ion.Int("user_id", 42),
		ion.String("tenant", "acme-corp"),
	)

	httpLog.Info(ctx, "request received") // {"logger": "http", ...}
	grpcLog.Info(ctx, "rpc called")       // {"logger": "grpc", ...}
	userLogger.Info(ctx, "action taken")  // {"user_id": 42, "tenant": "acme-corp", ...}
}

// ============================================================================
// Example 4: Metrics
// Demonstrates OpenTelemetry metrics instrumentation.
// ============================================================================

func example4_Metrics() {
	ctx := context.Background()

	cfg := ion.Default().WithService("metrics-demo")
	cfg.Metrics.Enabled = true
	cfg.Metrics.Endpoint = "localhost:4317" // OTel Collector
	cfg.Metrics.Protocol = "grpc"
	cfg.Metrics.Insecure = true

	app, _, err := ion.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create ion: %v", err)
	}
	defer func() { _ = app.Shutdown(ctx) }()

	// Get a named meter
	meter := app.Meter("example.metrics")

	// Create instruments
	requestCounter, _ := meter.Int64Counter("http_requests_total") // metric.WithDescription("Total HTTP requests"),

	latencyHist, _ := meter.Float64Histogram("http_request_duration_seconds") // metric.WithDescription("HTTP request latency"),

	// Record metrics
	requestCounter.Add(ctx, 1)
	latencyHist.Record(ctx, 0.025) // 25ms

	app.Info(ctx, "metrics recorded")
}

// ============================================================================
// Example 5: Blockchain Fields
// Demonstrates domain-specific field helpers.
// ============================================================================

func example5_BlockchainFields() {
	ctx := context.Background()
	app, _, _ := ion.New(ion.Default().WithService("mempool-router"))

	app.Info(ctx, "transaction routed",
		fields.TxHash("0xabc123..."),
		fields.ShardID(3),
		fields.Slot(150_000_000),
		fields.Epoch(350),
		fields.BlockHeight(19_500_000),
		fields.LatencyMs(12.5),
	)
}

// ============================================================================
// Example 6: Production Setup with Tracing
// The recommended pattern for real-world services.
// ============================================================================

func example6_ProductionSetup() {
	ctx := context.Background()

	cfg := ion.Config{
		Level:       "info",
		Development: false,
		ServiceName: "order-service",
		Version:     "v2.1.0",

		Console: ion.ConsoleConfig{
			Enabled:        true,
			Format:         "json",
			ErrorsToStderr: true,
		},

		File: ion.FileConfig{
			Enabled:    true,
			Path:       "/var/log/orders/app.log",
			MaxSizeMB:  100,
			MaxBackups: 5,
			Compress:   true,
		},

		OTEL: ion.OTELConfig{
			Enabled:  true,
			Endpoint: "otel-collector:4317",
			Protocol: "grpc",
			Attributes: map[string]string{
				"env":    "production",
				"region": "us-east-1",
			},
		},

		Tracing: ion.TracingConfig{
			Enabled: true,
			Sampler: "ratio:0.1", // Sample 10%
		},
	}

	app, warnings, err := ion.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create ion: %v", err)
	}
	for _, w := range warnings {
		log.Printf("ion warning: %v", w)
	}

	// CRITICAL: Graceful shutdown to flush all logs and traces
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := app.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "shutdown error: %v\n", err)
		}
	}()

	// Run your application
	runProductionApp(ctx, app)
}

func runProductionApp(ctx context.Context, app *ion.Ion) {
	// Child preserves tracing and metrics — single entry point for component observability
	svc := app.Child("main")
	tracer := svc.Tracer("order-service.main")

	// Create a span for the main operation
	ctx, span := tracer.Start(ctx, "ApplicationRun")
	defer span.End()

	svc.Info(ctx, "service started")

	// Simulate work
	time.Sleep(100 * time.Millisecond)

	// Simulate an error
	err := errors.New("database connection lost")
	svc.Error(ctx, "critical failure", err, ion.String("component", "db"))

	// Wait for signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		// Auto-exit for example purposes
		time.Sleep(200 * time.Millisecond)
		sigChan <- syscall.SIGTERM
	}()

	svc.Info(ctx, "waiting for shutdown signal...")
	<-sigChan
	svc.Info(ctx, "received shutdown signal, exiting...")
}

// ============================================================================
// Main: Run all examples
// ============================================================================

func main() {
	fmt.Println("=== Example 1: Simple Usage ===")
	example1_SimpleUsage()

	fmt.Println("\n=== Example 2: Dependency Injection ===")
	example2_DependencyInjection()

	fmt.Println("\n=== Example 3: Child Loggers ===")
	example3_ChildLoggers()

	fmt.Println("\n=== Example 4: Metrics ===")
	example4_Metrics()

	fmt.Println("\n=== Example 5: Blockchain Fields ===")
	example5_BlockchainFields()

	fmt.Println("\n=== Example 6: Production Setup ===")
	example6_ProductionSetup()
}
