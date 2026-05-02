package config

import "time"

func (cfg Config) SetDSN(dsn string) Config {
	cfg.DSN = dsn
	return cfg
}

func (cfg Config) SetTable(table string) Config {
	cfg.Table = table
	return cfg
}

func (cfg Config) SetLevel(level string) Config {
	cfg.Level = level
	return cfg
}

func (cfg Config) SetBatchSize(batchSize int) Config {
	cfg.BatchSize = batchSize
	return cfg
}

func (cfg Config) SetFlushInterval(flushInterval time.Duration) Config {
	cfg.FlushInterval = flushInterval
	return cfg
}

func (cfg Config) SetChannelBuffer(channelBuffer int) Config {
	cfg.ChannelBuffer = channelBuffer
	return cfg
}

func (cfg Config) SetAutoSchema(autoSchema bool) Config {
	cfg.AutoSchema = autoSchema
	return cfg
}

func (cfg Config) SetDialTimeout(dialTimeout time.Duration) Config {
	cfg.DialTimeout = dialTimeout
	return cfg
}

func (cfg Config) SetWriteTimeout(writeTimeout time.Duration) Config {
	cfg.WriteTimeout = writeTimeout
	return cfg
}

func (cfg Config) SetMaxOpenConns(maxOpenConns int) Config {
	cfg.MaxOpenConns = maxOpenConns
	return cfg
}

func (cfg Config) SetMaxIdleConns(maxIdleConns int) Config {
	cfg.MaxIdleConns = maxIdleConns
	return cfg
}

func (cfg Config) SetConnMaxLifetime(connMaxLifetime time.Duration) Config {
	cfg.ConnMaxLifetime = connMaxLifetime
	return cfg
}
