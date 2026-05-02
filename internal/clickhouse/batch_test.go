package clickhouse

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func testLogRow() logRow {
	return logRow{Message: "test", Level: "info"}
}

func TestBatchWriter_FlushesOnBatchSize(t *testing.T) {
	flushed := make(chan []logRow, 1)
	bw := newBatchWriter(batchConfig{
		BatchSize:     3,
		FlushInterval: 10 * time.Second,
		ChannelBuffer: 100,
	}, func(rows []logRow) error {
		flushed <- append([]logRow{}, rows...)
		return nil
	})
	bw.start()
	defer bw.shutdown()

	for range 3 {
		bw.send(testLogRow())
	}

	select {
	case rows := <-flushed:
		if len(rows) != 3 {
			t.Errorf("got %d rows, want 3", len(rows))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("flush not triggered after BatchSize rows")
	}
}

func TestBatchWriter_FlushesOnTicker(t *testing.T) {
	flushed := make(chan []logRow, 1)
	bw := newBatchWriter(batchConfig{
		BatchSize:     1000,
		FlushInterval: 50 * time.Millisecond,
		ChannelBuffer: 100,
	}, func(rows []logRow) error {
		flushed <- append([]logRow{}, rows...)
		return nil
	})
	bw.start()
	defer bw.shutdown()

	bw.send(testLogRow())

	select {
	case rows := <-flushed:
		if len(rows) != 1 {
			t.Errorf("got %d rows, want 1", len(rows))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("flush not triggered by ticker")
	}
}

func TestBatchWriter_ShutdownFlushesRemaining(t *testing.T) {
	var mu sync.Mutex
	var total int
	bw := newBatchWriter(batchConfig{
		BatchSize:     1000,
		FlushInterval: 10 * time.Second,
		ChannelBuffer: 100,
	}, func(rows []logRow) error {
		mu.Lock()
		total += len(rows)
		mu.Unlock()
		return nil
	})
	bw.start()

	for range 5 {
		bw.send(testLogRow())
	}
	bw.shutdown()

	mu.Lock()
	defer mu.Unlock()
	if total != 5 {
		t.Errorf("shutdown flushed %d rows, want 5", total)
	}
}

func TestBatchWriter_DropsWhenFull(t *testing.T) {
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

	for range 10 {
		bw.send(testLogRow())
	}

	if bw.droppedCount() == 0 {
		t.Error("expected dropped > 0 when channel is full")
	}

	close(block)
	bw.shutdown()
}

func TestBatchWriter_FlushErrorContinues(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	bw := newBatchWriter(batchConfig{
		BatchSize:     1,
		FlushInterval: 10 * time.Second,
		ChannelBuffer: 100,
	}, func(rows []logRow) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return errors.New("ch down")
	})
	bw.start()
	defer bw.shutdown()

	bw.send(testLogRow())
	bw.send(testLogRow())

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := calls
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if calls < 2 {
		t.Errorf("flush fn called %d times, want >= 2 (errors must not stop the writer)", calls)
	}
}

func TestBatchWriter_NoRowsNoFlush(t *testing.T) {
	called := false
	bw := newBatchWriter(batchConfig{
		BatchSize:     10,
		FlushInterval: 50 * time.Millisecond,
		ChannelBuffer: 10,
	}, func(rows []logRow) error {
		called = true
		return nil
	})
	bw.start()
	time.Sleep(120 * time.Millisecond) // let ticker fire at least once
	bw.shutdown()

	if called {
		t.Error("flush must not be called when no rows were sent")
	}
}

func TestBatchWriter_SyncFlushesCurrentBatch(t *testing.T) {
	flushed := make(chan []logRow, 1)
	bw := newBatchWriter(batchConfig{
		BatchSize:     1000,         // high — ticker/sync must trigger, not size
		FlushInterval: 10 * time.Second, // long — sync must trigger, not ticker
		ChannelBuffer: 100,
	}, func(rows []logRow) error {
		flushed <- append([]logRow{}, rows...)
		return nil
	})
	bw.start()
	defer bw.shutdown()

	bw.send(testLogRow())
	bw.sync() // must flush synchronously before returning

	select {
	case rows := <-flushed:
		if len(rows) != 1 {
			t.Errorf("sync flushed %d rows, want 1", len(rows))
		}
	default:
		t.Fatal("sync returned but no rows were flushed")
	}
}

func TestBatchWriter_FlushErrorIncrementsDropped(t *testing.T) {
	bw := newBatchWriter(batchConfig{
		BatchSize:     3,
		FlushInterval: 10 * time.Second,
		ChannelBuffer: 100,
	}, func(rows []logRow) error {
		return errors.New("ch down")
	})
	bw.start()
	defer bw.shutdown()

	for range 3 {
		bw.send(testLogRow())
	}

	// wait for the batch to flush (and fail)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if bw.droppedCount() == 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if bw.droppedCount() != 3 {
		t.Errorf("droppedCount = %d, want 3 after flush error", bw.droppedCount())
	}
}

func TestBatchWriter_ShutdownIdempotent(t *testing.T) {
	bw := newBatchWriter(batchConfig{
		BatchSize:     10,
		FlushInterval: time.Second,
		ChannelBuffer: 10,
	}, func(rows []logRow) error { return nil })
	bw.start()
	bw.shutdown()
	bw.shutdown() // must not panic
}

func TestBatchWriter_SyncAfterShutdown_DoesNotDeadlock(t *testing.T) {
	bw := newBatchWriter(batchConfig{
		BatchSize:     10,
		FlushInterval: time.Second,
		ChannelBuffer: 10,
	}, func(rows []logRow) error { return nil })
	bw.start()
	bw.shutdown()

	done := make(chan struct{})
	go func() {
		bw.sync()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("sync() deadlocked after shutdown()")
	}
}

func TestBatchWriter_BatchSizeSpansMultipleFlushes(t *testing.T) {
	flushed := make(chan []logRow, 10)
	bw := newBatchWriter(batchConfig{
		BatchSize:     2,
		FlushInterval: 10 * time.Second,
		ChannelBuffer: 100,
	}, func(rows []logRow) error {
		flushed <- append([]logRow{}, rows...)
		return nil
	})
	bw.start()
	defer bw.shutdown()

	for range 6 {
		bw.send(testLogRow())
	}

	got := 0
	deadline := time.After(2 * time.Second)
	for got < 6 {
		select {
		case rows := <-flushed:
			if len(rows) != 2 {
				t.Errorf("flush batch size: got %d, want 2", len(rows))
			}
			got += len(rows)
		case <-deadline:
			t.Fatalf("only %d of 6 rows flushed before timeout", got)
		}
	}
}
