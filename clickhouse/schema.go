package clickhouse

import (
	"context"
	_ "embed"
	"fmt"

	chdriver "github.com/ClickHouse/clickhouse-go/v2"

	"github.com/JupiterMetaLabs/ion/clickhouse/config"
)

/*
	Below line is used to embed the create table sql into the binary. its not a comment.
*/

//go:embed sql/create_table.sql
var createTableSQL string

type schemaConfig struct {
	Table string
}

type schemaExecer interface {
	Exec(ctx context.Context, query string, args ...any) error
}

// BuildDDL returns the CREATE TABLE IF NOT EXISTS DDL for the given table name.
func BuildDDL(table string) string {
	return fmt.Sprintf(createTableSQL, table)
}

func ensureSchema(ctx context.Context, exec schemaExecer, cfg schemaConfig) error {
	return exec.Exec(ctx, BuildDDL(cfg.Table))
}

// RunEnsureSchema opens a temporary connection using cfg, creates the table
// for the given table name if it does not exist, then closes the connection.
// Safe to call on every startup — the DDL is idempotent (IF NOT EXISTS).
func RunEnsureSchema(ctx context.Context, cfg config.Config, table string) error {
	opts, err := chdriver.ParseDSN(cfg.DSN)
	if err != nil {
		return fmt.Errorf("clickhouse schema: parse DSN: %w", err)
	}
	conn, err := chdriver.Open(opts)
	if err != nil {
		return fmt.Errorf("clickhouse schema: open: %w", err)
	}
	defer conn.Close() //nolint:errcheck // best-effort close on a temp connection
	return ensureSchema(ctx, conn, schemaConfig{Table: table})
}
