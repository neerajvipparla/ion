CREATE TABLE IF NOT EXISTS %s
(
    timestamp   DateTime64(9, 'UTC'),
    level       LowCardinality(String),
    service     LowCardinality(String),
    version     LowCardinality(String),
    logger      String,
    message     String,
    trace_id    String,
    span_id     String,
    request_id  String,
    user_id     String,
    caller      String,
    str_fields  Map(String, String),
    int_fields  Map(String, Int64),
    flt_fields  Map(String, Float64),
    bool_fields Map(String, UInt8),
    extra       String
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (service, level, timestamp)
TTL timestamp + INTERVAL 30 DAY DELETE
SETTINGS index_granularity = 8192
