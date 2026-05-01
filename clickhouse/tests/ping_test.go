package tests

import (
	"context"
	"testing"
	"time"

	"github.com/JupiterMetaLabs/ion/clickhouse/config"
)

func TestPing_MalformedDSN(t *testing.T) {
	ctx := context.Background()
	err := config.Config{DSN: "://not-valid"}.Ping(ctx)
	if err == nil {
		t.Fatal("expected error for malformed DSN, got nil")
	}
}

func TestPing_UnreachableHost(t *testing.T) {
	// Port 19999 — nothing listening. Expect a connection error, not a hang.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := config.Config{DSN: "clickhouse://localhost:19999/default"}.Ping(ctx)
	if err == nil {
		t.Fatal("expected error for unreachable host, got nil")
	}
}

func TestPing_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := config.Config{DSN: "clickhouse://localhost:9000/default"}.Ping(ctx)
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}
