package clickhouse

import (
	"context"
	"errors"
	"time"

	"github.com/JupiterMetaLabs/ion/clickhouse/config"
)

/*
	I dont want multiple instances of the same ClickHouse core. TODO
	- Relatively simple is to implement the singleton pattern.
	  * But i dont want to use the singleton pattern on this.
*/

// Core is the ClickHouse zapcore.Core implementation.
// Use New() to construct, Open() to connect, Shutdown() to close.
type Core struct {
	ctx context.Context
	Config config.Config
}

// New validates cfg, applies defaults, and r	eturns an uninitialised Core.
// It does not connect to ClickHouse. Call Open() before attaching to a logger.
func New(ctx context.Context, cfg config.Config) (*Core, error) {
	cfg = cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, errors.New("clickhouse core: validation failed: " + err.Error())
	}

	pingCtx, pingCtxCancel := context.WithTimeout(ctx, 10*time.Second)
	defer pingCtxCancel()
	if err := cfg.Ping(pingCtx); err != nil {
		return nil, errors.New("clickhouse core: ping failed: " + err.Error())
	}

	// Typically its safe to take the child context because the parent context is usually the application context.
	cfg_context := context.WithValue(ctx, "config.ClickHouse", cfg)
	return &Core{ctx: cfg_context, Config: cfg}, nil
}
