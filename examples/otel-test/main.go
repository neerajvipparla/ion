// Package main tests ion trace correlation with OTEL and Jaeger.
//
// Run with:
//
//	docker compose up -d
//	go run examples/otel-test/main.go
//	# View traces at http://localhost:16686
//	# View logs in docker compose logs otel-collector
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/neerajvipparla/ion"
)

func main() {
	ctx := context.Background()

	// Configure with OTEL and tracing enabled
	cfg := ion.Config{
		ServiceName: "ion-otel-test",
		Version:     "1.0.0",
		Level:       "debug",
		Development: true,
		Console: ion.ConsoleConfig{
			Enabled: true,
			Format:  "pretty",
			Color:   true,
		},
		OTEL: ion.OTELConfig{
			Enabled:  true,
			Endpoint: os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
			Protocol: "grpc",
			Insecure: true, // Default to insecure for local testing usually, user can override via scheme in endpoint or code change if needed
			Username: os.Getenv("OTEL_EXPORTER_OTLP_HEADERS_USERNAME"),
			Password: os.Getenv("OTEL_EXPORTER_OTLP_HEADERS_PASSWORD"),
		},
		Tracing: ion.TracingConfig{
			Enabled: true,
			// Fallback to OTEL endpoint if not set, but explicit here for clarity if needed
			Endpoint: os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
			Protocol: "grpc",
			Sampler:  "always",
		},
	}

	fmt.Printf("Configuring Ion with:\n")
	fmt.Printf("  OTEL Endpoint: %s\n", cfg.OTEL.Endpoint)
	fmt.Printf("  OTEL Protocol: %s\n", cfg.OTEL.Protocol)
	fmt.Printf("  Service Name: %s\n", cfg.ServiceName)
	fmt.Println("---------------------------------------------------")

	app, warnings, err := ion.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create ion: %v", err)
	}
	for _, w := range warnings {
		log.Printf("ion warning: %v", w)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := app.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown error: %v", err)
		}
	}()

	// 1. Orphan Logs (No Context)
	fmt.Println("[Scenario 1] Orphan Logs (Background/Startup)")
	app.Info(ctx, "application starting", ion.String("env", "staging"))
	app.Debug(ctx, "loading configuration", ion.String("path", "/etc/config.yaml"))
	time.Sleep(100 * time.Millisecond)

	// 2. HTTP Request Simulation (API -> DB -> Cache)
	fmt.Println("\n[Scenario 2] HTTP Request: Payment Processing")
	processPayment(ctx, app, "user_123", 99.99)

	// 3. Error Scenario
	fmt.Println("\n[Scenario 3] API Error Flow")
	simulateAPIError(ctx, app)

	// 4. Concurrent Batch Processing
	fmt.Println("\n[Scenario 4] Concurrent Background Jobs")
	runConcurrentJobs(ctx, app)

	fmt.Println("\n========================================")
	fmt.Println("TEST COMPLETE! Check Grafana/Jaeger.")
	fmt.Println("========================================")

	// Give OTEL time to flush
	time.Sleep(2 * time.Second)
}

func processPayment(ctx context.Context, app *ion.Ion, userID string, amount float64) {
	app.Info(ctx, "http_request_received", ion.String("method", "POST"), ion.String("path", "/api/pay"))

	tracer := app.Tracer("payment-service")
	ctx, span := tracer.Start(ctx, "ProcessPayment")
	defer span.End()

	app.Info(ctx, "processing payment", ion.String("user_id", userID), ion.Float64("amount", amount))

	// Simulate DB Call
	func() {
		ctx, span := tracer.Start(ctx, "Database:GetUser")
		defer span.End()
		app.Debug(ctx, "querying user balance", ion.String("db_host", "primary-db"))
		time.Sleep(50 * time.Millisecond)
	}()

	// Simulate External API Call (Bank)
	func() {
		ctx, span := tracer.Start(ctx, "ExternalAPI:ChargeCard")
		defer span.End()
		app.Info(ctx, "contacting payment provider", ion.String("provider", "stripe"))
		time.Sleep(150 * time.Millisecond)
		app.Info(ctx, "payment authorized", ion.String("tx_id", "ch_12345"))
	}()

	app.Info(ctx, "payment successful", ion.Int("status", 200))
}

func simulateAPIError(ctx context.Context, app *ion.Ion) {
	tracer := app.Tracer("api-service")
	ctx, span := tracer.Start(ctx, "GetUserProfile")
	defer span.End()

	app.Info(ctx, "fetching user profile", ion.String("user_id", "unknown_999"))

	// Simulate DB Error
	func() {
		ctx, span := tracer.Start(ctx, "Database:Find")
		defer span.End()
		// app.Error signature is (ctx, msg, error, fields...) or similar.
		// Assuming Error(ctx, error, msg, fields...) or Error(ctx, msg, fields...)
		// Let's check ion.go signature. Based on usage it seems Error(ctx, msg, fields...) but typicalzap/ion pattern might differ.
		// Error log usually takes a string message.
		app.Error(ctx, "database connection failed", fmt.Errorf("connection timeout"), ion.String("error", "connection_timeout"), ion.Int("retries", 3))
		span.RecordError(fmt.Errorf("db connection timeout"))
	}()

	app.Error(ctx, "request failed", fmt.Errorf("internal error"), ion.Int("status", 500), ion.String("code", "INTERNAL_ERROR"))
}

func runConcurrentJobs(ctx context.Context, app *ion.Ion) {
	tracer := app.Tracer("worker-pool")
	ctx, span := tracer.Start(ctx, "BatchProcessor")
	defer span.End()

	app.Info(ctx, "starting batch job", ion.Int("job_count", 3))

	done := make(chan bool)
	for i := 0; i < 3; i++ {
		go func(id int) {
			ctx, span := tracer.Start(ctx, fmt.Sprintf("Job-%d", id))
			defer span.End()

			app.Info(ctx, "job started", ion.Int("worker_id", id))
			time.Sleep(time.Duration(100+id*50) * time.Millisecond)
			app.Info(ctx, "job completed", ion.Int("worker_id", id))
			done <- true
		}(i)
	}

	for i := 0; i < 3; i++ {
		<-done
	}
	app.Info(ctx, "all jobs finished")
}
