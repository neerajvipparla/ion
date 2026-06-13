package config

import (
	"context"
	"errors"

	chdriver "github.com/ClickHouse/clickhouse-go/v2"
	clickhouse_errors "github.com/neerajvipparla/ion/internal/clickhouse/config/errors"
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
		return errors.New(clickhouse_errors.ErrInvalidDSN.Error() + ": " + err.Error())
	}

	conn, err := chdriver.Open(opts)
	if err != nil {
		return errors.New(clickhouse_errors.ErrOpenFailed.Error() + ": " + err.Error())
	}
	// Open errors return above with no defer; from here conn is live and defer always runs on exit (including Ping errors).
	defer conn.Close() //nolint:errcheck // best-effort close on a temp connection

	if err := conn.Ping(ctx); err != nil {
		return errors.New(clickhouse_errors.ErrPingFailed.Error() + ": " + err.Error())
	}
	return nil
}
