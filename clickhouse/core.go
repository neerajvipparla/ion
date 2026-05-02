package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"time"

	chdriver "github.com/ClickHouse/clickhouse-go/v2"
	"go.uber.org/zap/zapcore"

	"github.com/JupiterMetaLabs/ion/clickhouse/config"
)

// Core is a zapcore.Core that writes log entries to ClickHouse.
// Call New() to construct, Open() to connect, Shutdown() to close.
type Core struct {
	Config       config.Config
	conn         chdriver.Conn
	level        zapcore.Level
	writer       *batchWriter
	presetFields []zapcore.Field
}

// New validates cfg, applies defaults, pings ClickHouse, and returns a Core
// ready to be opened. Call Open() before attaching to a logger.
func New(ctx context.Context, cfg config.Config) (*Core, error) {
	cfg = cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, errors.New("clickhouse core: validation failed: " + err.Error())
	}

	var lvl zapcore.Level
	_ = lvl.UnmarshalText([]byte(cfg.Level)) // already validated by Validate()

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := cfg.Ping(pingCtx); err != nil {
		return nil, errors.New("clickhouse core: ping failed: " + err.Error())
	}

	return &Core{Config: cfg, level: lvl}, nil
}

// Open opens a persistent ClickHouse connection, optionally creates the table
// (when AutoSchema is true), and starts the background flush goroutine.
// Returns an error if called on an already-open Core.
func (c *Core) Open(ctx context.Context) error {
	if c.conn != nil {
		return errors.New("clickhouse core: already open")
	}
	opts, err := chdriver.ParseDSN(c.Config.DSN)
	if err != nil {
		return fmt.Errorf("clickhouse core: parse DSN: %w", err)
	}
	opts.MaxOpenConns = c.Config.MaxOpenConns
	opts.MaxIdleConns = c.Config.MaxIdleConns
	opts.ConnMaxLifetime = c.Config.ConnMaxLifetime
	opts.DialTimeout = c.Config.DialTimeout

	conn, err := chdriver.Open(opts)
	if err != nil {
		return fmt.Errorf("clickhouse core: open: %w", err)
	}

	if c.Config.AutoSchema {
		if err := ensureSchema(ctx, conn, schemaConfig{Table: c.Config.Table}); err != nil {
			_ = conn.Close()
			return fmt.Errorf("clickhouse core: ensure schema: %w", err)
		}
	}

	c.conn = conn
	c.writer = newBatchWriter(batchConfig{
		BatchSize:     c.Config.BatchSize,
		FlushInterval: c.Config.FlushInterval,
		ChannelBuffer: c.Config.ChannelBuffer,
		Table:         c.Config.Table,
	}, func(rows []logRow) error {
		return c.flushRows(rows)
	})
	c.writer.start()
	return nil
}

// Shutdown drains remaining buffered rows, flushes them, and closes the connection.
// Respects ctx deadline — returns ctx.Err() if the drain takes too long.
func (c *Core) Shutdown(ctx context.Context) error {
	if c.writer != nil {
		done := make(chan struct{})
		go func() {
			c.writer.shutdown()
			close(done)
		}()
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if c.conn != nil {
		err := c.conn.Close() //nolint:wrapcheck // direct driver close
		c.conn = nil
		return err
	}
	return nil
}

// DroppedCount returns the total log entries dropped due to a full buffer.
func (c *Core) DroppedCount() int64 {
	if c.writer == nil {
		return 0
	}
	return c.writer.droppedCount()
}

// --- zapcore.Core interface ---

func (c *Core) Enabled(lvl zapcore.Level) bool {
	return lvl >= c.level
}

func (c *Core) With(fields []zapcore.Field) zapcore.Core {
	clone := *c
	clone.presetFields = make([]zapcore.Field, len(c.presetFields)+len(fields))
	copy(clone.presetFields, c.presetFields)
	copy(clone.presetFields[len(c.presetFields):], fields)
	return &clone
}

func (c *Core) Check(entry zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(entry.Level) {
		return ce.AddCore(entry, c)
	}
	return ce
}

func (c *Core) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	if c.writer == nil {
		return nil
	}
	all := make([]zapcore.Field, 0, len(c.presetFields)+len(fields))
	all = append(all, c.presetFields...)
	all = append(all, fields...)
	c.writer.send(extractRow(entry, all))
	return nil
}

func (c *Core) Sync() error {
	if c.writer != nil {
		c.writer.sync()
	}
	return nil
}

// --- private ---

func (c *Core) flushRows(rows []logRow) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.Config.WriteTimeout)
	defer cancel()
	b, err := c.conn.PrepareBatch(ctx,
		fmt.Sprintf("INSERT INTO %s", c.Config.Table))
	if err != nil {
		return err
	}
	for i := range rows {
		normalizeRow(&rows[i])
		if err := b.AppendStruct(&rows[i]); err != nil {
			return err
		}
	}
	return b.Send()
}

// normalizeRow replaces nil maps with empty maps before sending to ClickHouse.
// ClickHouse Map columns cannot be null; the driver requires non-nil maps.
func normalizeRow(row *logRow) {
	if row.StrFields == nil {
		row.StrFields = map[string]string{}
	}
	if row.IntFields == nil {
		row.IntFields = map[string]int64{}
	}
	if row.FltFields == nil {
		row.FltFields = map[string]float64{}
	}
	if row.BoolFields == nil {
		row.BoolFields = map[string]uint8{}
	}
}
