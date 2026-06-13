package core

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"google.golang.org/grpc"
	insecurecreds "google.golang.org/grpc/credentials/insecure"

	"github.com/neerajvipparla/ion/internal/config"
)

// MeterProvider wraps the OTEL MeterProvider.
type MeterProvider struct {
	provider *sdkmetric.MeterProvider
}

// Meter returns a named meter.
func (mp *MeterProvider) Meter(name string, opts ...metric.MeterOption) metric.Meter {
	if mp == nil || mp.provider == nil {
		return noop.NewMeterProvider().Meter(name, opts...)
	}
	return mp.provider.Meter(name, opts...)
}

// Shutdown shuts down the meter provider.
func (mp *MeterProvider) Shutdown(ctx context.Context) error {
	if mp == nil || mp.provider == nil {
		return nil
	}
	return mp.provider.Shutdown(ctx)
}

// SetupMeterProvider initializes OpenTelemetry metrics.
func SetupMeterProvider(cfg config.MetricsConfig, serviceName, version string) (*MeterProvider, error) {
	if !cfg.Enabled || (cfg.Endpoint == "" && cfg.Protocol == "") {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := resource.New(ctx,
		resource.WithHost(),
		resource.WithOS(),
		resource.WithProcess(),
		resource.WithAttributes(semconv.ServiceName(serviceName), semconv.ServiceVersion(version)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTEL resource: %w", err)
	}

	// Inject Auth header logic: Preferred Headers > Basic Auth fallback
	headers := injectAuth(cfg.Headers, cfg.Username, cfg.Password, cfg.Protocol)

	// Parse/Sanitize endpoint
	endpoint, insecure, err := processEndpoint(cfg.Endpoint, cfg.Insecure)
	if err != nil {
		return nil, fmt.Errorf("invalid metrics endpoint: %w", err)
	}

	// Exporter
	var exporter sdkmetric.Exporter
	switch cfg.Protocol {
	case "http":
		opts := []otlpmetrichttp.Option{
			otlpmetrichttp.WithEndpoint(endpoint),
		}
		if insecure {
			opts = append(opts, otlpmetrichttp.WithInsecure())
		}
		if len(headers) > 0 {
			opts = append(opts, otlpmetrichttp.WithHeaders(headers))
		}
		if cfg.Timeout > 0 {
			opts = append(opts, otlpmetrichttp.WithTimeout(cfg.Timeout))
		}
		exporter, err = otlpmetrichttp.New(ctx, opts...)
	default:
		// Default to gRPC
		opts := []otlpmetricgrpc.Option{
			otlpmetricgrpc.WithEndpoint(endpoint),
		}
		if insecure {
			opts = append(opts, otlpmetricgrpc.WithInsecure())
			opts = append(opts, otlpmetricgrpc.WithDialOption(grpc.WithTransportCredentials(insecurecreds.NewCredentials())))
		}
		if len(headers) > 0 {
			opts = append(opts, otlpmetricgrpc.WithHeaders(headers))
		}
		if cfg.Timeout > 0 {
			opts = append(opts, otlpmetricgrpc.WithTimeout(cfg.Timeout))
		}
		exporter, err = otlpmetricgrpc.New(ctx, opts...)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create metric exporter: %w", err)
	}

	// Reader
	interval := cfg.Interval
	if interval <= 0 {
		interval = 15 * time.Second
	}

	// Default to Cumulative temporality (default OTel behavior)
	reader := sdkmetric.NewPeriodicReader(
		exporter,
		sdkmetric.WithInterval(interval),
	)

	// Provider
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(reader),
	)

	// Set global provider
	otel.SetMeterProvider(mp)

	return &MeterProvider{provider: mp}, nil
}
