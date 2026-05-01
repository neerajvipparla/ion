package config

import (
	"context"

	chdriver "github.com/ClickHouse/clickhouse-go/v2"
	clickhouse_errors "github.com/JupiterMetaLabs/ion/clickhouse/config/errors"
)

// Ping opens a temporary connection to ClickHouse using the given DSN,
// sends a ping, and closes the connection. It does not retain any state.
//
// Use this to validate a DSN at startup before calling Open(), or in
// health-check handlers to verify ClickHouse is reachable.
//
// The provided context controls the dial and ping timeout. A cancelled
// or deadline-exceeded context will propagate as an error.
func (cfg Config) Ping(ctx context.Context) error {
	opts, err := chdriver.ParseDSN(cfg.DSN)
	if err != nil {
		return clickhouse_errors.ErrInvalidDSN
	}

	conn, err := chdriver.Open(opts)
	if err != nil {
		return clickhouse_errors.ErrOpenFailed
	}
	defer conn.Close() //nolint:errcheck // best-effort close on a temp connection

	if err := conn.Ping(ctx); err != nil {
		return clickhouse_errors.ErrPingFailed
	}
	return nil
}
