package core

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.32.0"
	"google.golang.org/grpc"
	insecurecreds "google.golang.org/grpc/credentials/insecure"

	"github.com/JupiterMetaLabs/ion/internal/config"
)

// LogProvider manages the OpenTelemetry log provider.
type LogProvider struct {
	loggerProvider *sdklog.LoggerProvider
}

// LoggerProvider returns the underlying sdklog.LoggerProvider
func (p *LogProvider) LoggerProvider() *sdklog.LoggerProvider {
	if p == nil {
		return nil
	}
	return p.loggerProvider
}

// Shutdown shuts down the log provider.
func (p *LogProvider) Shutdown(ctx context.Context) error {
	if p == nil || p.loggerProvider == nil {
		return nil
	}
	return p.loggerProvider.Shutdown(ctx)
}

// TracerProvider wraps the OTEL TracerProvider.
type TracerProvider struct {
	provider *sdktrace.TracerProvider
}

// Shutdown shuts down the tracer provider.
func (tp *TracerProvider) Shutdown(ctx context.Context) error {
	if tp == nil || tp.provider == nil {
		return nil
	}
	return tp.provider.Shutdown(ctx)
}

// SetupLogProvider initializes OpenTelemetry logging.
func SetupLogProvider(cfg config.OTELConfig, serviceName, version string) (*LogProvider, error) {
	if !cfg.Enabled || cfg.Endpoint == "" {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Resources
	attrs := []attribute.KeyValue{
		semconv.ServiceName(serviceName),
		semconv.ServiceVersion(version),
	}
	for k, v := range cfg.Attributes {
		attrs = append(attrs, attribute.String(k, v))
	}

	res, err := resource.New(ctx,
		resource.WithHost(),
		resource.WithOS(),
		resource.WithProcess(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(attrs...),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTEL resource: %w", err)
	}

	// Exporter
	// Parse/Sanitize endpoint
	endpoint, insecure, err := processEndpoint(cfg.Endpoint, cfg.Insecure)
	if err != nil {
		return nil, fmt.Errorf("invalid OTEL endpoint: %w", err)
	}
	// Inject Auth header logic: Preferred Headers > Basic Auth fallback
	cfg.Headers = injectAuth(cfg.Headers, cfg.Username, cfg.Password, cfg.Protocol)

	var exporter sdklog.Exporter
	switch cfg.Protocol {
	case "http":
		exporter, err = createHTTPLogExporter(ctx, endpoint, insecure, cfg)
	default:
		exporter, err = createGRPCLogExporter(ctx, endpoint, insecure, cfg)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create OTEL log exporter: %w", err)
	}

	// Processor
	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = 512
	}
	// A robust queue size buffers telemetry against momentary network latency or throughput spikes.
	// We use 4x the batch size or the OTEL standard 2048, whichever is larger, to prevent premature drops.
	queueSize := batchSize * 4
	if queueSize < 2048 {
		queueSize = 2048
	}

	exportInterval := cfg.ExportInterval
	if exportInterval <= 0 {
		exportInterval = 5 * time.Second
	}

	processor := sdklog.NewBatchProcessor(
		exporter,
		sdklog.WithMaxQueueSize(queueSize),
		sdklog.WithExportMaxBatchSize(batchSize),
		sdklog.WithExportInterval(exportInterval),
	)

	provider := sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(processor),
	)

	// Set global logger provider (optional, but good for libs using global API)
	global.SetLoggerProvider(provider)

	return &LogProvider{loggerProvider: provider}, nil
}

// SetupTracerProvider creates and configures the OTEL tracer provider.
func SetupTracerProvider(cfg config.TracingConfig, serviceName, version string) (*TracerProvider, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	// Inject Auth header logic: Preferred Headers > Basic Auth fallback
	cfg.Headers = injectAuth(cfg.Headers, cfg.Username, cfg.Password, cfg.Protocol)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Resource
	res, err := resource.New(ctx,
		resource.WithHost(),
		resource.WithOS(),
		resource.WithProcess(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(version),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Exporter
	// Parse/Sanitize endpoint
	endpoint, insecure, err := processEndpoint(cfg.Endpoint, cfg.Insecure)
	if err != nil {
		return nil, fmt.Errorf("invalid OTEL endpoint: %w", err)
	}

	var exporter sdktrace.SpanExporter
	switch cfg.Protocol {
	case "http":
		exporter, err = createHTTPTraceExporter(ctx, endpoint, insecure, cfg)
	default:
		exporter, err = createGRPCTraceExporter(ctx, endpoint, insecure, cfg)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	// Sampler
	sampler := parseSampler(cfg.Sampler)

	// Processor
	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = 512
	}
	// A robust queue size buffers telemetry against momentary network latency or throughput spikes.
	queueSize := batchSize * 4
	if queueSize < 2048 {
		queueSize = 2048
	}

	exportInterval := cfg.ExportInterval
	if exportInterval <= 0 {
		exportInterval = 5 * time.Second
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(exporter,
			sdktrace.WithMaxQueueSize(queueSize),
			sdktrace.WithMaxExportBatchSize(batchSize),
			sdktrace.WithBatchTimeout(exportInterval),
		),
		sdktrace.WithSampler(sampler),
	)

	// Set globals
	otel.SetTracerProvider(tp)

	props := []propagation.TextMapPropagator{
		propagation.TraceContext{},
		propagation.Baggage{},
	}
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(props...))

	return &TracerProvider{provider: tp}, nil
}

// --- Helpers ---

func createGRPCLogExporter(ctx context.Context, endpoint string, insecure bool, cfg config.OTELConfig) (sdklog.Exporter, error) {
	opts := []otlploggrpc.Option{
		otlploggrpc.WithEndpoint(endpoint),
	}
	if insecure {
		opts = append(opts, otlploggrpc.WithDialOption(grpc.WithTransportCredentials(insecurecreds.NewCredentials())))
		opts = append(opts, otlploggrpc.WithInsecure())
	}
	if cfg.Timeout > 0 {
		opts = append(opts, otlploggrpc.WithTimeout(cfg.Timeout))
	}
	if len(cfg.Headers) > 0 {
		opts = append(opts, otlploggrpc.WithHeaders(cfg.Headers))
	}
	return otlploggrpc.New(ctx, opts...)
}

func createHTTPLogExporter(ctx context.Context, endpoint string, insecure bool, cfg config.OTELConfig) (sdklog.Exporter, error) {
	opts := []otlploghttp.Option{
		otlploghttp.WithEndpoint(endpoint),
	}
	if insecure {
		opts = append(opts, otlploghttp.WithInsecure())
	}
	if cfg.Timeout > 0 {
		opts = append(opts, otlploghttp.WithTimeout(cfg.Timeout))
	}
	if len(cfg.Headers) > 0 {
		opts = append(opts, otlploghttp.WithHeaders(cfg.Headers))
	}
	return otlploghttp.New(ctx, opts...)
}

func createGRPCTraceExporter(ctx context.Context, endpoint string, insecure bool, cfg config.TracingConfig) (sdktrace.SpanExporter, error) {
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(endpoint),
	}
	if insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
		opts = append(opts, otlptracegrpc.WithDialOption(grpc.WithTransportCredentials(insecurecreds.NewCredentials())))
	}
	if len(cfg.Headers) > 0 {
		opts = append(opts, otlptracegrpc.WithHeaders(cfg.Headers))
	}
	if cfg.Timeout > 0 {
		opts = append(opts, otlptracegrpc.WithTimeout(cfg.Timeout))
	}
	return otlptracegrpc.New(ctx, opts...)
}

func createHTTPTraceExporter(ctx context.Context, endpoint string, insecure bool, cfg config.TracingConfig) (sdktrace.SpanExporter, error) {
	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(endpoint),
	}
	if insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	if len(cfg.Headers) > 0 {
		opts = append(opts, otlptracehttp.WithHeaders(cfg.Headers))
	}
	if cfg.Timeout > 0 {
		opts = append(opts, otlptracehttp.WithTimeout(cfg.Timeout))
	}
	return otlptracehttp.New(ctx, opts...)
}

func parseSampler(s string) sdktrace.Sampler {
	switch {
	case s == "" || s == "always":
		return sdktrace.AlwaysSample()
	case s == "never":
		return sdktrace.NeverSample()
	case strings.HasPrefix(s, "ratio:"):
		ratioStr := strings.TrimPrefix(s, "ratio:")
		ratio, err := strconv.ParseFloat(ratioStr, 64)
		if err != nil {
			return sdktrace.AlwaysSample()
		}
		return sdktrace.TraceIDRatioBased(ratio)
	default:
		return sdktrace.AlwaysSample()
	}
}

// processEndpoint parses the endpoint URL to determine the host:port and insecure setting.
// If the endpoint contains a scheme (http/https), it overrides the insecure config.
// Returns the sanitized endpoint (host:port), the final insecure flag, and any error.
func processEndpoint(endpoint string, configInsecure bool) (string, bool, error) {
	if endpoint == "" {
		return "", configInsecure, nil
	}

	// If no scheme, assume it's already host:port or similar, keep config setting.
	if !strings.Contains(endpoint, "://") {
		return endpoint, configInsecure, nil
	}

	u, err := url.Parse(endpoint)
	if err != nil {
		return "", false, fmt.Errorf("failed to parse endpoint url: %w", err)
	}

	host := u.Host
	// If host is empty but path is not, it might be a malformed URL or just path.
	// But url.Parse("example.com:4317") parses as path "example.com:4317" with empty host if no scheme.
	// We handled no-scheme above. So here we expect a scheme.

	var insecure bool
	switch u.Scheme {
	case "http":
		insecure = true
	case "https":
		insecure = false
	default:
		return "", false, fmt.Errorf("unsupported scheme %q (only http:// and https:// allowed)", u.Scheme)
	}

	// Ensure port if missing for http/https
	if u.Port() == "" {
		switch u.Scheme {
		case "http":
			host = host + ":80"
		case "https":
			host = host + ":443"
		}
	}

	return host, insecure, nil
}

// injectAuth ensures the provided headers map contains appropriate authentication credentials.
// It returns a newly allocated map to guarantee the original configuration remains immutable.
//
// Hierarchy:
// 1. Header-First: If the provided headers map already contains an "Authorization" (or "authorization") key, it is preserved.
// 2. Basic Auth Fallback: If no authorization header is present, it constructs a Basic Auth credential using the provided username and password.
//
// Protocol should be "http" or "grpc". gRPC requires the lowercase "authorization" key to comply with HTTP/2 and gRPC metadata semantics.
func injectAuth(headers map[string]string, username, password, protocol string) map[string]string {
	// Deep copy to ensure we do not mutate shared configuration maps across components
	out := make(map[string]string, len(headers)+1)
	for k, v := range headers {
		out[k] = v
	}

	key := "Authorization"
	altKey := "authorization"
	if protocol != "http" {
		key = "authorization"
		altKey = "Authorization"
	}

	// 1. Header-First: Check if authentication is already explicitly provided in the headers
	if _, hasPrimary := out[key]; hasPrimary {
		return out
	}
	if val, hasAlt := out[altKey]; hasAlt {
		// Normalize to the protocol's required case
		out[key] = val
		delete(out, altKey)
		return out
	}

	// 2. Fallback: Generate Basic Auth if credentials are provided
	if username != "" && password != "" {
		auth := fmt.Sprintf("%s:%s", username, password)
		encodedAuth := base64.StdEncoding.EncodeToString([]byte(auth))
		out[key] = "Basic " + encodedAuth
	}

	return out
}
