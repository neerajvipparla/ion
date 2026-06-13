// Package main provides a comprehensive benchmark suite for ion logging library.
//
// Run with:
//
//	go run examples/benchmark/main.go
//
// Or run with Go's benchmark tool:
//
//	go test -bench=. -benchmem ./examples/benchmark/
package main

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/neerajvipparla/ion"
	"github.com/neerajvipparla/ion/fields"
)

func main() {
	fmt.Println("========================================")
	fmt.Println("ION LOGGING BENCHMARK SUITE")
	fmt.Println("========================================")
	fmt.Println()

	// Run all benchmarks
	runBenchmark("Info (no fields)", benchmarkInfoNoFields)
	runBenchmark("Info (3 fields)", benchmarkInfo3Fields)
	runBenchmark("Info (10 fields)", benchmarkInfo10Fields)
	runBenchmark("Info (blockchain fields)", benchmarkBlockchainFields)
	runBenchmark("Info (with trace context)", benchmarkWithTraceContext)
	runBenchmark("Debug (filtered - zero alloc)", benchmarkDebugFiltered)
	runBenchmark("Error (with error)", benchmarkError)
	runBenchmark("Child logger (With)", benchmarkWithChild)
	runBenchmark("Child logger (Named)", benchmarkNamedChild)
	runBenchmark("Field: String", benchmarkFieldString)
	runBenchmark("Field: Int", benchmarkFieldInt)
	runBenchmark("Field: F (polymorphic)", benchmarkFieldF)
	runBenchmark("Field: Uint64", benchmarkFieldUint64)
	runBenchmark("Pool: 3 fields (within cap)", benchmarkPool3Fields)
	runBenchmark("Pool: 17 fields (exceeds cap)", benchmarkPool17Fields)
	runBenchmark("Console output (JSON)", benchmarkConsoleJSON)
	runBenchmark("Console output (Pretty)", benchmarkConsolePretty)

	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("BENCHMARK COMPLETE")
	fmt.Println("========================================")
}

// runBenchmark runs a benchmark function and prints results.
func runBenchmark(name string, fn func(b *testing.B)) {
	// Force GC before benchmark
	runtime.GC()

	result := testing.Benchmark(fn)

	nsPerOp := float64(result.T.Nanoseconds()) / float64(result.N)
	allocsPerOp := result.AllocsPerOp()
	bytesPerOp := result.AllocedBytesPerOp()

	fmt.Printf("%-35s %10.0f ns/op  %3d allocs  %5d B/op\n",
		name, nsPerOp, allocsPerOp, bytesPerOp)
}

// --- Benchmark Functions ---

func setupLogger() ion.Logger {
	cfg := ion.Default()
	cfg.Console.Enabled = false
	logger, _, _ := ion.New(cfg)
	return logger
}

func benchmarkInfoNoFields(b *testing.B) {
	logger := setupLogger()
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info(ctx, "benchmark message")
	}
}

func benchmarkInfo3Fields(b *testing.B) {
	logger := setupLogger()
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info(ctx, "benchmark message",
			ion.String("key1", "value1"),
			ion.Int("key2", 42),
			ion.Bool("key3", true),
		)
	}
}

func benchmarkInfo10Fields(b *testing.B) {
	logger := setupLogger()
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info(ctx, "benchmark message",
			ion.String("key1", "value1"),
			ion.Int("key2", 42),
			ion.Bool("key3", true),
			ion.String("key4", "value4"),
			ion.Int("key5", 100),
			ion.Float64("key6", 3.14),
			ion.String("key7", "value7"),
			ion.Int("key8", 200),
			ion.String("key9", "value9"),
			ion.Bool("key10", false),
		)
	}
}

func benchmarkBlockchainFields(b *testing.B) {
	logger := setupLogger()
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info(ctx, "transaction processed",
			fields.TxHash("0xabc123def456..."),
			fields.BlockHeight(18_500_000),
			fields.Slot(250_000_000),
			fields.GasUsed(21000),
			fields.LatencyMs(42.5),
		)
	}
}

func benchmarkWithTraceContext(b *testing.B) {
	logger := setupLogger()

	// Set up no-op tracer
	otel.SetTracerProvider(noop.NewTracerProvider())
	tracer := otel.Tracer("benchmark")
	ctx, span := tracer.Start(context.Background(), "BenchmarkOp")
	defer span.End()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info(ctx, "message with trace",
			ion.String("key", "value"),
		)
	}
}

func benchmarkDebugFiltered(b *testing.B) {
	cfg := ion.Default()
	cfg.Level = "info" // Debug will be filtered
	cfg.Console.Enabled = false
	logger, _, _ := ion.New(cfg)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Debug(ctx, "filtered message")
	}
}

func benchmarkError(b *testing.B) {
	logger := setupLogger()
	ctx := context.Background()
	err := fmt.Errorf("benchmark error")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Error(ctx, "operation failed", err,
			ion.String("operation", "benchmark"),
		)
	}
}

func benchmarkWithChild(b *testing.B) {
	logger := setupLogger()
	child := logger.With(ion.String("component", "benchmark"))
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		child.Info(ctx, "message from child")
	}
}

func benchmarkNamedChild(b *testing.B) {
	logger := setupLogger()
	named := logger.Named("benchmark")
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		named.Info(ctx, "message from named")
	}
}

func benchmarkFieldString(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = ion.String("key", "value")
	}
}

func benchmarkFieldInt(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = ion.Int("key", 42)
	}
}

func benchmarkFieldF(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = ion.F("key", "value")
	}
}

func benchmarkFieldUint64(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = ion.Uint64("block_height", 18_500_000)
	}
}

func benchmarkPool3Fields(b *testing.B) {
	logger := setupLogger()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info(ctx, "message",
			ion.String("a", "1"),
			ion.String("b", "2"),
			ion.String("c", "3"),
		)
	}
}

func benchmarkPool17Fields(b *testing.B) {
	logger := setupLogger()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info(ctx, "message",
			ion.String("a", "1"), ion.String("b", "2"), ion.String("c", "3"),
			ion.String("d", "4"), ion.String("e", "5"), ion.String("f", "6"),
			ion.String("g", "7"), ion.String("h", "8"), ion.String("i", "9"),
			ion.String("j", "10"), ion.String("k", "11"), ion.String("l", "12"),
			ion.String("m", "13"), ion.String("n", "14"), ion.String("o", "15"),
			ion.String("p", "16"), ion.String("q", "17"),
		)
	}
}

func benchmarkConsoleJSON(b *testing.B) {
	cfg := ion.Default()
	cfg.Console.Enabled = true
	cfg.Console.Format = "json"
	logger, _, _ := ion.New(cfg)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info(ctx, "message",
			ion.String("key", "value"),
		)
	}
}

func benchmarkConsolePretty(b *testing.B) {
	cfg := ion.Default()
	cfg.Console.Enabled = true
	cfg.Console.Format = "pretty"
	logger, _, _ := ion.New(cfg)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info(ctx, "message",
			ion.String("key", "value"),
		)
	}
}

// --- Additional benchmarks for comparison ---

func init() {
	// Warm up the pool
	cfg := ion.Default()
	cfg.Console.Enabled = false
	logger, _, _ := ion.New(cfg)
	ctx := context.Background()
	for i := 0; i < 100; i++ {
		logger.Info(ctx, "warmup", ion.String("key", "value"))
	}
	time.Sleep(10 * time.Millisecond)
}
