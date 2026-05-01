package errors

import "errors"

var (
	ErrOpenFailed = errors.New("clickhouse core: open failed")
	ErrShutdownFailed = errors.New("clickhouse core: shutdown failed")
	ErrWriteFailed = errors.New("clickhouse core: write failed")
	ErrFlushFailed = errors.New("clickhouse core: flush failed")
	ErrBufferFull = errors.New("clickhouse core: buffer full")
	ErrContextCancelled = errors.New("clickhouse core: context cancelled")
)

var (
	ErrInvalidDSN = errors.New("clickhouse config: invalid DSN")
	ErrPingFailed = errors.New("clickhouse config: ping failed")
	ErrInvalidLevel = errors.New("clickhouse config: invalid level")
	ErrBatchSizeMustNotBeNegative = errors.New("clickhouse config: batch size must not be negative")
	ErrChannelBufferMustNotBeNegative = errors.New("clickhouse config: channel buffer must not be negative")
)