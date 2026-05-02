package config

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	clickhouse_errors "github.com/JupiterMetaLabs/ion/clickhouse/config/errors"
)

var validIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)?$`)

// Config holds all configuration for the ClickHouse log sink.
// Call New(cfg) to validate and apply defaults before use.
type Config struct {
	// Enabled controls whether the ClickHouse sink is active.
	// Default: false
	Enabled bool

	// DSN is the ClickHouse connection string. Required when Enabled.
	// Example: "http://user:pass@host:8123/db", "clickhouse://user:pass@host:9000/db"
	DSN string

	// Table is the ClickHouse table name for log entries.
	// Default: "ion_logs"
	Table string

	// Level is the minimum log level forwarded to ClickHouse.
	// Valid values: "debug", "info", "warn", "error", "fatal".
	// Default: "info"
	Level string

	// BatchSize is the maximum number of rows per flush.
	// Default: 1000
	BatchSize int

	// FlushInterval is how often the background flusher sends pending rows.
	// Default: 5s
	FlushInterval time.Duration

	// ChannelBuffer is the depth of the async write queue.
	// When full, incoming entries are dropped and DroppedCount increments.
	// Default: 10000
	ChannelBuffer int

	// AutoSchema runs CREATE TABLE IF NOT EXISTS on Open() when true.
	// Default: false — production teams are expected to manage DDL themselves.
	AutoSchema bool

	// DialTimeout is the ClickHouse connection timeout.
	// Default: 10s
	DialTimeout time.Duration

	// WriteTimeout is the per-flush ClickHouse write timeout.
	// Default: 30s
	WriteTimeout time.Duration

	// MaxOpenConns is the connection pool size.
	// Default: 5
	MaxOpenConns int

	// MaxIdleConns is the number of idle connections kept open.
	// Default: 5
	MaxIdleConns int

	// ConnMaxLifetime is how long a connection may be reused.
	// Default: 1h
	ConnMaxLifetime time.Duration
}

var validLevels = map[string]bool{
	"debug":   true,
	"info":    true,
	"warn":    true,
	"warning": true,
	"error":   true,
	"fatal":   true,
}

// withDefaults returns a copy of cfg with zero-value fields filled with defaults.
// User-supplied non-zero values are never overwritten.
func (cfg Config) WithDefaults() Config {
	if cfg.Table == "" {
		cfg.Table = "ion_logs"
	}
	if cfg.Level == "" {
		cfg.Level = "info"
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 1000
	}
	if cfg.FlushInterval == 0 {
		cfg.FlushInterval = 5 * time.Second
	}
	if cfg.ChannelBuffer == 0 {
		cfg.ChannelBuffer = 10000
	}
	if cfg.DialTimeout == 0 {
		cfg.DialTimeout = 10 * time.Second
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = 30 * time.Second
	}
	if cfg.MaxOpenConns == 0 {
		cfg.MaxOpenConns = 5
	}
	if cfg.MaxIdleConns == 0 {
		cfg.MaxIdleConns = 5
	}
	if cfg.ConnMaxLifetime == 0 {
		cfg.ConnMaxLifetime = time.Hour
	}
	return cfg
}

// validate checks the config for invalid values after defaults have been applied.
// Returns a combined error listing every violation found.
func (cfg Config) Validate() error {
	var errs []string

	if cfg.DSN == "" {
		errs = append(errs, clickhouse_errors.ErrInvalidDSN.Error())
	}
	if cfg.Level != "" && !validLevels[strings.ToLower(cfg.Level)] {
		errs = append(errs, clickhouse_errors.ErrInvalidLevel.Error())
	}
	if cfg.Table != "" && !validIdentifier.MatchString(cfg.Table) {
		errs = append(errs, clickhouse_errors.ErrInvalidTable.Error())
	}
	if cfg.BatchSize < 0 {
		errs = append(errs, clickhouse_errors.ErrBatchSizeMustNotBeNegative.Error())
	}
	if cfg.ChannelBuffer < 0 {
		errs = append(errs, clickhouse_errors.ErrChannelBufferMustNotBeNegative.Error())
	}
	if cfg.FlushInterval < 0 {
		errs = append(errs, clickhouse_errors.ErrFlushIntervalMustNotBeNegative.Error())
	}
	if cfg.DialTimeout < 0 {
		errs = append(errs, clickhouse_errors.ErrDialTimeoutMustNotBeNegative.Error())
	}
	if cfg.WriteTimeout < 0 {
		errs = append(errs, clickhouse_errors.ErrWriteTimeoutMustNotBeNegative.Error())
	}
	if cfg.MaxOpenConns < 0 {
		errs = append(errs, clickhouse_errors.ErrMaxOpenConnsMustNotBeNegative.Error())
	}
	if cfg.MaxIdleConns < 0 {
		errs = append(errs, clickhouse_errors.ErrMaxIdleConnsMustNotBeNegative.Error())
	}
	if cfg.ConnMaxLifetime < 0 {
		errs = append(errs, clickhouse_errors.ErrConnMaxLifetimeMustNotBeNegative.Error())
	}
	if cfg.MaxOpenConns > 0 && cfg.MaxIdleConns > cfg.MaxOpenConns {
		errs = append(errs, clickhouse_errors.ErrMaxIdleConnsExceedsMaxOpenConns.Error())
	}

	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("clickhouse config validation failed: %s", strings.Join(errs, "; "))
}
