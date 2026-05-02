package ion

import (
	clickhouseconfig "github.com/JupiterMetaLabs/ion/clickhouse/config"
	"github.com/JupiterMetaLabs/ion/internal/config"
)

// Config holds the complete logger configuration.
// It is an alias to internal/config.Config to allow sharing with internal packages.
type Config = config.Config

// ConsoleConfig configures console output.
type ConsoleConfig = config.ConsoleConfig

// FileConfig configures file output.
type FileConfig = config.FileConfig

// OTELConfig configures OTEL log export.
type OTELConfig = config.OTELConfig

// TracingConfig configures distributed tracing.
type TracingConfig = config.TracingConfig

// MetricsConfig configures OpenTelemetry metrics export.
type MetricsConfig = config.MetricsConfig

// ClickHouseConfig configures the ClickHouse log sink.
type ClickHouseConfig = clickhouseconfig.Config

// Default returns a Config with sensible production defaults.
func Default() Config {
	return config.Default()
}

// Development returns a Config optimized for development.
func Development() Config {
	return config.Development()
}
