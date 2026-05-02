package clickhouse

import (
	"context"
	"errors"
	"testing"
	"time"

	chdriver "github.com/ClickHouse/clickhouse-go/v2"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/JupiterMetaLabs/ion/internal/clickhouse/config"
)

// coreWithWriter builds a Core backed by a fake batchWriter — no real ClickHouse needed.
func coreWithWriter(t *testing.T, level zapcore.Level) (*Core, chan []logRow) {
	t.Helper()
	flushed := make(chan []logRow, 100)
	bw := newBatchWriter(batchConfig{
		BatchSize:     1000,
		FlushInterval: 10 * time.Second,
		ChannelBuffer: 100,
	}, func(rows []logRow) error {
		flushed <- append([]logRow{}, rows...)
		return nil
	})
	bw.start()
	t.Cleanup(bw.shutdown)
	return &Core{level: level, writer: bw}, flushed
}

// drainRows collects exactly n rows across potentially multiple flush batches.
func drainRows(t *testing.T, flushed <-chan []logRow, want int) []logRow {
	t.Helper()
	var all []logRow
	deadline := time.After(2 * time.Second)
	for len(all) < want {
		select {
		case rows := <-flushed:
			all = append(all, rows...)
		case <-deadline:
			t.Fatalf("timed out: got %d rows, want %d", len(all), want)
		}
	}
	return all
}

// --- Enabled ---

func TestCore_Enabled_InfoLevel(t *testing.T) {
	c, _ := coreWithWriter(t, zapcore.InfoLevel)
	cases := []struct {
		lvl  zapcore.Level
		want bool
	}{
		{zapcore.DebugLevel, false},
		{zapcore.InfoLevel, true},
		{zapcore.WarnLevel, true},
		{zapcore.ErrorLevel, true},
	}
	for _, tc := range cases {
		if got := c.Enabled(tc.lvl); got != tc.want {
			t.Errorf("Enabled(%v) = %v, want %v", tc.lvl, got, tc.want)
		}
	}
}

func TestCore_Enabled_DebugLevel(t *testing.T) {
	c, _ := coreWithWriter(t, zapcore.DebugLevel)
	if !c.Enabled(zapcore.DebugLevel) {
		t.Error("Debug should be enabled on a debug-level core")
	}
}

// --- Check ---

func TestCore_Check_EnabledLevelAddsCore(t *testing.T) {
	c, _ := coreWithWriter(t, zapcore.InfoLevel)
	entry := zapcore.Entry{Level: zapcore.InfoLevel, Message: "test"}
	ce := c.Check(entry, nil)
	if ce == nil {
		t.Error("Check must return non-nil CheckedEntry for an enabled level")
	}
}

func TestCore_Check_DisabledLevelSkips(t *testing.T) {
	c, _ := coreWithWriter(t, zapcore.InfoLevel)
	entry := zapcore.Entry{Level: zapcore.DebugLevel, Message: "test"}
	ce := c.Check(entry, nil)
	if ce != nil {
		t.Error("Check must return nil for a disabled level")
	}
}

// --- Write ---

func TestCore_Write_ReturnsNil(t *testing.T) {
	c, _ := coreWithWriter(t, zapcore.InfoLevel)
	if err := c.Write(basicEntry(), nil); err != nil {
		t.Errorf("Write must always return nil, got %v", err)
	}
}

func TestCore_Write_RowReachesWriter(t *testing.T) {
	c, flushed := coreWithWriter(t, zapcore.InfoLevel)
	entry := basicEntry()
	entry.Message = "reach writer"

	_ = c.Write(entry, nil)
	_ = c.Sync()

	rows := drainRows(t, flushed, 1)
	if rows[0].Message != "reach writer" {
		t.Errorf("Message: got %q, want %q", rows[0].Message, "reach writer")
	}
}

func TestCore_Write_LevelPreserved(t *testing.T) {
	c, flushed := coreWithWriter(t, zapcore.DebugLevel)
	entry := basicEntry()
	entry.Level = zapcore.WarnLevel

	_ = c.Write(entry, nil)
	_ = c.Sync()

	rows := drainRows(t, flushed, 1)
	if rows[0].Level != "warn" {
		t.Errorf("Level: got %q, want %q", rows[0].Level, "warn")
	}
}

func TestCore_Write_NilWriter_ReturnsNil(t *testing.T) {
	c := &Core{level: zapcore.InfoLevel} // Open() never called
	if err := c.Write(basicEntry(), nil); err != nil {
		t.Errorf("Write with nil writer must return nil, got %v", err)
	}
}

// --- With ---

func TestCore_With_DoesNotMutateReceiver(t *testing.T) {
	c, _ := coreWithWriter(t, zapcore.InfoLevel)
	before := len(c.presetFields)

	_ = c.With([]zapcore.Field{zap.String("k", "v")})

	if len(c.presetFields) != before {
		t.Errorf("With() mutated receiver: presetFields len %d → %d", before, len(c.presetFields))
	}
}

func TestCore_With_PresetFieldsPropagated(t *testing.T) {
	c, flushed := coreWithWriter(t, zapcore.InfoLevel)
	child := c.With([]zapcore.Field{zap.String("env", "staging")})

	_ = child.Write(basicEntry(), nil)
	_ = child.Sync()

	rows := drainRows(t, flushed, 1)
	if rows[0].StrFields["env"] != "staging" {
		t.Errorf("StrFields[env]: got %q, want %q", rows[0].StrFields["env"], "staging")
	}
}

func TestCore_With_WriteFieldsDoNotPersist(t *testing.T) {
	c, flushed := coreWithWriter(t, zapcore.InfoLevel)

	_ = c.Write(basicEntry(), []zapcore.Field{zap.String("once", "x")})
	_ = c.Sync()
	drainRows(t, flushed, 1) // consume

	_ = c.Write(basicEntry(), nil)
	_ = c.Sync()
	rows := drainRows(t, flushed, 1)
	if _, ok := rows[0].StrFields["once"]; ok {
		t.Error("Write() field leaked into a subsequent Write() call")
	}
}

func TestCore_With_ChildSharesWriter(t *testing.T) {
	c, flushed := coreWithWriter(t, zapcore.InfoLevel)
	child := c.With([]zapcore.Field{zap.String("src", "child")})

	_ = c.Write(basicEntry(), nil)
	_ = child.Write(basicEntry(), nil)
	_ = c.Sync()

	rows := drainRows(t, flushed, 2)
	if len(rows) != 2 {
		t.Errorf("expected 2 rows from parent+child sharing writer, got %d", len(rows))
	}
}

// --- Sync ---

func TestCore_Sync_FlushesBeforeTicker(t *testing.T) {
	c, flushed := coreWithWriter(t, zapcore.InfoLevel)

	_ = c.Write(basicEntry(), nil)
	if err := c.Sync(); err != nil {
		t.Fatalf("Sync returned error: %v", err)
	}

	select {
	case rows := <-flushed:
		if len(rows) != 1 {
			t.Errorf("Sync flushed %d rows, want 1", len(rows))
		}
	default:
		t.Fatal("Sync returned but no rows were flushed")
	}
}

// --- Shutdown ---

func TestCore_Shutdown_RespectsContextDeadline(t *testing.T) {
	block := make(chan struct{})
	bw := newBatchWriter(batchConfig{
		BatchSize:     1,
		FlushInterval: 10 * time.Second,
		ChannelBuffer: 10,
	}, func(rows []logRow) error {
		<-block // never returns until test unblocks it
		return nil
	})
	bw.start()
	bw.send(testLogRow())
	time.Sleep(30 * time.Millisecond) // let goroutine pick up the row and block in flush

	c := &Core{level: zapcore.InfoLevel, writer: bw}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	defer close(block)

	err := c.Shutdown(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Shutdown: expected DeadlineExceeded, got %v", err)
	}
}

// --- DroppedCount ---

func TestCore_Open_GuardsAgainstDoubleOpen(t *testing.T) {
	// Simulate an already-open Core by assigning a non-nil conn.
	// dummyConn embeds the interface so it satisfies it without implementing methods.
	type dummyConn struct{ chdriver.Conn }
	c := &Core{
		Config: config.Config{DSN: "clickhouse://localhost:9000/default"}.WithDefaults(),
		level:  zapcore.InfoLevel,
		conn:   dummyConn{},
	}
	err := c.Open(context.Background())
	if err == nil {
		t.Fatal("Open on already-open Core should return error, got nil")
	}
}

func TestCore_Shutdown_IsIdempotent(t *testing.T) {
	bw := newBatchWriter(batchConfig{
		BatchSize:     1,
		FlushInterval: time.Second,
		ChannelBuffer: 10,
	}, func([]logRow) error { return nil })
	bw.start()
	c := &Core{level: zapcore.InfoLevel, writer: bw}
	ctx := context.Background()
	if err := c.Shutdown(ctx); err != nil {
		t.Fatalf("first Shutdown: %v", err)
	}
	if err := c.Shutdown(ctx); err != nil {
		t.Fatalf("second Shutdown: %v", err)
	}
}

func TestCore_DroppedCount_ReflectsWriterDrops(t *testing.T) {
	block := make(chan struct{})
	bw := newBatchWriter(batchConfig{
		BatchSize:     1,
		FlushInterval: 10 * time.Second,
		ChannelBuffer: 1,
	}, func(rows []logRow) error {
		<-block
		return nil
	})
	bw.start()
	c := &Core{level: zapcore.InfoLevel, writer: bw}

	for range 10 {
		_ = c.Write(basicEntry(), nil)
	}

	if c.DroppedCount() == 0 {
		t.Error("DroppedCount should be > 0 when channel is full")
	}

	close(block)
	bw.shutdown()
}
