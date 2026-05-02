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
	ErrInvalidDSN                     = errors.New("clickhouse config: invalid DSN")
	ErrPingFailed                     = errors.New("clickhouse config: ping failed")
	ErrInvalidLevel                   = errors.New("clickhouse config: invalid level")
	ErrInvalidTable                   = errors.New("clickhouse config: invalid table name")
	ErrBatchSizeMustNotBeNegative     = errors.New("clickhouse config: batch size must not be negative")
	ErrChannelBufferMustNotBeNegative = errors.New("clickhouse config: channel buffer must not be negative")
	ErrFlushIntervalMustNotBeNegative = errors.New("clickhouse config: flush interval must not be negative")
	ErrDialTimeoutMustNotBeNegative   = errors.New("clickhouse config: dial timeout must not be negative")
	ErrWriteTimeoutMustNotBeNegative  = errors.New("clickhouse config: write timeout must not be negative")
	ErrMaxOpenConnsMustNotBeNegative  = errors.New("clickhouse config: max open conns must not be negative")
	ErrMaxIdleConnsMustNotBeNegative  = errors.New("clickhouse config: max idle conns must not be negative")
	ErrConnMaxLifetimeMustNotBeNegative = errors.New("clickhouse config: conn max lifetime must not be negative")
	ErrMaxIdleConnsExceedsMaxOpenConns  = errors.New("clickhouse config: max idle conns must not exceed max open conns")
)