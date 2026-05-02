package clickhouse

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

type batchConfig struct {
	BatchSize     int
	FlushInterval time.Duration
	ChannelBuffer int
	Table         string
}

type batchWriter struct {
	ch       chan logRow
	cfg      batchConfig
	flushFn  func([]logRow) error
	stop     chan struct{}
	stopOnce sync.Once
	syncReqs chan chan struct{}
	wg       sync.WaitGroup
	dropped  atomic.Int64
}

func newBatchWriter(cfg batchConfig, flushFn func([]logRow) error) *batchWriter {
	return &batchWriter{
		ch:       make(chan logRow, cfg.ChannelBuffer),
		cfg:      cfg,
		flushFn:  flushFn,
		stop:     make(chan struct{}),
		syncReqs: make(chan chan struct{}, 1),
	}
}

func (bw *batchWriter) start() {
	bw.wg.Add(1)
	go bw.run()
}

// send enqueues a row. Non-blocking: drops and increments counter if channel is full.
func (bw *batchWriter) send(row logRow) {
	select {
	case bw.ch <- row:
	default:
		bw.dropped.Add(1)
	}
}

func (bw *batchWriter) droppedCount() int64 {
	return bw.dropped.Load()
}

// sync drains the channel and flushes all pending rows before returning.
// Safe to call after shutdown() — returns immediately instead of deadlocking.
func (bw *batchWriter) sync() {
	done := make(chan struct{})
	select {
	case bw.syncReqs <- done:
		// sent; wait for the goroutine to confirm flush or for stop
		select {
		case <-done:
		case <-bw.stop:
		}
	case <-bw.stop:
		// already stopped — nothing to flush
	}
}

func (bw *batchWriter) run() {
	defer bw.wg.Done()
	ticker := time.NewTicker(bw.cfg.FlushInterval)
	defer ticker.Stop()

	var batch []logRow

	for {
		select {
		case row := <-bw.ch:
			batch = append(batch, row)
			if len(batch) >= bw.cfg.BatchSize {
				bw.flush(batch)
				batch = nil
			}

		case <-ticker.C:
			if len(batch) > 0 {
				bw.flush(batch)
				batch = nil
			}

		case done := <-bw.syncReqs:
			// drain everything currently in the channel before flushing
		drain:
			for {
				select {
				case row := <-bw.ch:
					batch = append(batch, row)
				default:
					break drain
				}
			}
			if len(batch) > 0 {
				bw.flush(batch)
				batch = nil
			}
			close(done)

		case <-bw.stop:
			for {
				select {
				case row := <-bw.ch:
					batch = append(batch, row)
				default:
					if len(batch) > 0 {
						bw.flush(batch)
					}
					return
				}
			}
		}
	}
}

func (bw *batchWriter) flush(rows []logRow) {
	if err := bw.flushFn(rows); err != nil {
		fmt.Fprintf(os.Stderr, "[ion/clickhouse] flush error: %v\n", err)
		bw.dropped.Add(int64(len(rows)))
	}
}

// shutdown signals the run goroutine to stop and waits for it to drain and flush.
// Safe to call multiple times — only the first call closes the stop channel.
func (bw *batchWriter) shutdown() {
	bw.stopOnce.Do(func() { close(bw.stop) })
	bw.wg.Wait()
}
