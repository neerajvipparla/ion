package clickhouse

import (
	"context"
	_ "embed"
	"fmt"
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

func buildDDL(table string) string {
	return fmt.Sprintf(createTableSQL, table)
}

func EnsureSchema(ctx context.Context, exec schemaExecer, cfg schemaConfig) error {
	return exec.Exec(ctx, buildDDL(cfg.Table))
}
