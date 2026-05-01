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
	ch      chan logRow
	cfg     batchConfig
	flushFn func([]logRow) error
	stop    chan struct{}
	wg      sync.WaitGroup
	dropped atomic.Int64
}

func newBatchWriter(cfg batchConfig, flushFn func([]logRow) error) *batchWriter {
	return &batchWriter{
		ch:      make(chan logRow, cfg.ChannelBuffer),
		cfg:     cfg,
		flushFn: flushFn,
		stop:    make(chan struct{}),
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

		case <-bw.stop:
			// drain whatever is left in the channel
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
	}
}

func (bw *batchWriter) shutdown() {
	close(bw.stop)
	bw.wg.Wait()
}
