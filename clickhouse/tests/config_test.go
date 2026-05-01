package tests

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/JupiterMetaLabs/ion/clickhouse"
	"github.com/JupiterMetaLabs/ion/clickhouse/config"
	cherrors "github.com/JupiterMetaLabs/ion/clickhouse/config/errors"
)

// --- Validate() — no connection required ---

func TestValidate_EmptyDSN(t *testing.T) {
	err := config.Config{}.Validate()
	if err == nil {
		t.Fatal("expected error for empty DSN, got nil")
	}
	if !strings.Contains(err.Error(), cherrors.ErrInvalidDSN.Error()) {
		t.Errorf("error %q missing sentinel %q", err, cherrors.ErrInvalidDSN)
	}
}

func TestValidate_InvalidLevel(t *testing.T) {
	err := config.Config{DSN: "clickhouse://localhost:9000/default", Level: "nonsense"}.Validate()
	if err == nil {
		t.Fatal("expected error for invalid level, got nil")
	}
	if !strings.Contains(err.Error(), cherrors.ErrInvalidLevel.Error()) {
		t.Errorf("error %q missing sentinel %q", err, cherrors.ErrInvalidLevel)
	}
}

func TestValidate_NegativeBatchSize(t *testing.T) {
	err := config.Config{DSN: "clickhouse://localhost:9000/default", BatchSize: -1}.Validate()
	if err == nil {
		t.Fatal("expected error for negative BatchSize, got nil")
	}
	if !strings.Contains(err.Error(), cherrors.ErrBatchSizeMustNotBeNegative.Error()) {
		t.Errorf("error %q missing sentinel %q", err, cherrors.ErrBatchSizeMustNotBeNegative)
	}
}

func TestValidate_NegativeChannelBuffer(t *testing.T) {
	err := config.Config{DSN: "clickhouse://localhost:9000/default", ChannelBuffer: -1}.Validate()
	if err == nil {
		t.Fatal("expected error for negative ChannelBuffer, got nil")
	}
	if !strings.Contains(err.Error(), cherrors.ErrChannelBufferMustNotBeNegative.Error()) {
		t.Errorf("error %q missing sentinel %q", err, cherrors.ErrChannelBufferMustNotBeNegative)
	}
}

func TestValidate_ValidConfig(t *testing.T) {
	// WithDefaults() must precede Validate() — same order New() uses internally.
	err := config.Config{DSN: "clickhouse://localhost:9000/default"}.WithDefaults().Validate()
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

// New() propagates validation errors before it ever pings.

func TestNew_RejectsInvalidConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  config.Config
	}{
		{"empty DSN", config.Config{}},
		{"invalid level", config.Config{DSN: "clickhouse://localhost:9000/default", Level: "bad"}},
		{"negative batch size", config.Config{DSN: "clickhouse://localhost:9000/default", BatchSize: -1}},
		{"negative channel buffer", config.Config{DSN: "clickhouse://localhost:9000/default", ChannelBuffer: -1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := clickhouse.New(context.Background(), tc.cfg)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

// --- WithDefaults() — no connection required ---

func TestDefaults_AppliedOnZeroValues(t *testing.T) {
	cfg := config.Config{DSN: "clickhouse://localhost:9000/default"}.WithDefaults()

	checks := []struct {
		field string
		got   any
		want  any
	}{
		{"Table", cfg.Table, "ion_logs"},
		{"Level", cfg.Level, "info"},
		{"BatchSize", cfg.BatchSize, 1000},
		{"FlushInterval", cfg.FlushInterval, 5 * time.Second},
		{"ChannelBuffer", cfg.ChannelBuffer, 10000},
		{"DialTimeout", cfg.DialTimeout, 10 * time.Second},
		{"WriteTimeout", cfg.WriteTimeout, 30 * time.Second},
		{"MaxOpenConns", cfg.MaxOpenConns, 5},
		{"MaxIdleConns", cfg.MaxIdleConns, 5},
		{"ConnMaxLifetime", cfg.ConnMaxLifetime, time.Hour},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: got %v, want %v", c.field, c.got, c.want)
		}
	}
}

func TestDefaults_UserValuesNotOverwritten(t *testing.T) {
	cfg := config.Config{
		DSN:           "clickhouse://localhost:9000/default",
		Table:         "my_logs",
		Level:         "debug",
		BatchSize:     500,
		FlushInterval: 2 * time.Second,
		ChannelBuffer: 5000,
		MaxOpenConns:  10,
	}.WithDefaults()

	checks := []struct {
		field string
		got   any
		want  any
	}{
		{"Table", cfg.Table, "my_logs"},
		{"Level", cfg.Level, "debug"},
		{"BatchSize", cfg.BatchSize, 500},
		{"FlushInterval", cfg.FlushInterval, 2 * time.Second},
		{"ChannelBuffer", cfg.ChannelBuffer, 5000},
		{"MaxOpenConns", cfg.MaxOpenConns, 10},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s overwritten: got %v, want %v", c.field, c.got, c.want)
		}
	}
}

// --- Integration: New() success requires a live ClickHouse ---
// Skipped automatically when CLICKHOUSE_TEST_DSN is not set.

func TestNew_ConnectsAndExposesConfig(t *testing.T) {
	dsn := os.Getenv("CLICKHOUSE_TEST_DSN")
	if dsn == "" {
		t.Skip("CLICKHOUSE_TEST_DSN not set — skipping integration test")
	}

	core, err := clickhouse.New(context.Background(), config.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	if core == nil {
		t.Fatal("expected non-nil Core")
	}
	if core.Config.DSN != dsn {
		t.Errorf("Config.DSN: got %q, want %q", core.Config.DSN, dsn)
	}
}

// --- extended Validate() coverage ---

func TestValidate_NegativeFlushInterval(t *testing.T) {
	err := config.Config{DSN: "clickhouse://localhost:9000/default", FlushInterval: -1}.Validate()
	if err == nil {
		t.Fatal("expected error for negative FlushInterval, got nil")
	}
}

func TestValidate_NegativeDialTimeout(t *testing.T) {
	err := config.Config{DSN: "clickhouse://localhost:9000/default", DialTimeout: -1}.Validate()
	if err == nil {
		t.Fatal("expected error for negative DialTimeout, got nil")
	}
}

func TestValidate_NegativeWriteTimeout(t *testing.T) {
	err := config.Config{DSN: "clickhouse://localhost:9000/default", WriteTimeout: -1}.Validate()
	if err == nil {
		t.Fatal("expected error for negative WriteTimeout, got nil")
	}
}

func TestValidate_NegativeMaxOpenConns(t *testing.T) {
	err := config.Config{DSN: "clickhouse://localhost:9000/default", MaxOpenConns: -1}.Validate()
	if err == nil {
		t.Fatal("expected error for negative MaxOpenConns, got nil")
	}
}

func TestValidate_NegativeMaxIdleConns(t *testing.T) {
	err := config.Config{DSN: "clickhouse://localhost:9000/default", MaxIdleConns: -1}.Validate()
	if err == nil {
		t.Fatal("expected error for negative MaxIdleConns, got nil")
	}
}

func TestValidate_MaxIdleConnsExceedsMaxOpenConns(t *testing.T) {
	err := config.Config{
		DSN:          "clickhouse://localhost:9000/default",
		MaxOpenConns: 3,
		MaxIdleConns: 5,
	}.Validate()
	if err == nil {
		t.Fatal("expected error when MaxIdleConns > MaxOpenConns, got nil")
	}
}

func TestValidate_NegativeConnMaxLifetime(t *testing.T) {
	err := config.Config{DSN: "clickhouse://localhost:9000/default", ConnMaxLifetime: -1}.Validate()
	if err == nil {
		t.Fatal("expected error for negative ConnMaxLifetime, got nil")
	}
}

func TestValidate_InvalidTableName(t *testing.T) {
	cases := []string{"", "my-table", "my table", "1table", "'; DROP TABLE"}
	for _, table := range cases {
		table := table
		t.Run(fmt.Sprintf("%q", table), func(t *testing.T) {
			err := config.Config{DSN: "clickhouse://localhost:9000/default", Table: table}.Validate()
			if err == nil {
				t.Errorf("Validate() with table %q should return error, got nil", table)
			}
		})
	}
}
