package clickhouse

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/JupiterMetaLabs/ion/internal/core"
)

// --- entry-level fields ---

func TestExtractRow_EntryFields(t *testing.T) {
	now := time.Now()
	entry := zapcore.Entry{
		Level:      zapcore.InfoLevel,
		Time:       now,
		Message:    "hello world",
		LoggerName: "svc.sub",
	}

	row := extractRow(entry, nil)

	if !row.Timestamp.Equal(now) {
		t.Errorf("Timestamp: got %v, want %v", row.Timestamp, now)
	}
	if row.Level != "info" {
		t.Errorf("Level: got %q, want %q", row.Level, "info")
	}
	if row.Message != "hello world" {
		t.Errorf("Message: got %q, want %q", row.Message, "hello world")
	}
	if row.Logger != "svc.sub" {
		t.Errorf("Logger: got %q, want %q", row.Logger, "svc.sub")
	}
}

func TestExtractRow_CallerSet(t *testing.T) {
	entry := basicEntry()
	entry.Caller = zapcore.EntryCaller{Defined: true, File: "main.go", Line: 42}

	row := extractRow(entry, nil)

	if row.Caller == "" {
		t.Error("Caller: expected non-empty, got empty")
	}
}

func TestExtractRow_CallerNotSet(t *testing.T) {
	row := extractRow(basicEntry(), nil)
	if row.Caller != "" {
		t.Errorf("Caller: expected empty, got %q", row.Caller)
	}
}

// --- typed primitive fields → typed map columns ---

func TestExtractRow_StringField(t *testing.T) {
	row := extractRow(basicEntry(), []zapcore.Field{
		zap.String("user", "alice"),
	})
	if row.StrFields["user"] != "alice" {
		t.Errorf("StrFields[user]: got %q, want %q", row.StrFields["user"], "alice")
	}
}

func TestExtractRow_IntFields(t *testing.T) {
	row := extractRow(basicEntry(), []zapcore.Field{
		zap.Int64("i64", 100),
		zap.Int32("i32", 200),
		zap.Int16("i16", 300),
		zap.Int8("i8", 127),
	})
	for key, want := range map[string]int64{"i64": 100, "i32": 200, "i16": 300, "i8": 127} {
		if row.IntFields[key] != want {
			t.Errorf("IntFields[%s]: got %d, want %d", key, row.IntFields[key], want)
		}
	}
}

func TestExtractRow_UintFields(t *testing.T) {
	row := extractRow(basicEntry(), []zapcore.Field{
		zap.Uint64("u64", 9999),
		zap.Uint32("u32", 8888),
		zap.Uint16("u16", 7777),
		zap.Uint8("u8", 255),
	})
	for key, want := range map[string]int64{"u64": 9999, "u32": 8888, "u16": 7777, "u8": 255} {
		if row.IntFields[key] != want {
			t.Errorf("IntFields[%s]: got %d, want %d", key, row.IntFields[key], want)
		}
	}
}

func TestExtractRow_FloatFields(t *testing.T) {
	row := extractRow(basicEntry(), []zapcore.Field{
		zap.Float64("pi", 3.14159),
		zap.Float32("e", 2.71),
	})
	if diff := absf(row.FltFields["pi"] - 3.14159); diff > 1e-9 {
		t.Errorf("FltFields[pi]: got %v, diff %v", row.FltFields["pi"], diff)
	}
	if diff := absf(row.FltFields["e"] - float64(float32(2.71))); diff > 1e-6 {
		t.Errorf("FltFields[e]: got %v, diff %v", row.FltFields["e"], diff)
	}
}

func TestExtractRow_BoolField(t *testing.T) {
	row := extractRow(basicEntry(), []zapcore.Field{
		zap.Bool("yes", true),
		zap.Bool("no", false),
	})
	if row.BoolFields["yes"] != 1 {
		t.Errorf("BoolFields[yes]: got %d, want 1", row.BoolFields["yes"])
	}
	if row.BoolFields["no"] != 0 {
		t.Errorf("BoolFields[no]: got %d, want 0", row.BoolFields["no"])
	}
}

func TestExtractRow_DurationGoesToStrFields(t *testing.T) {
	row := extractRow(basicEntry(), []zapcore.Field{
		zap.Duration("elapsed", 250*time.Millisecond),
	})
	if row.StrFields["elapsed"] != "250ms" {
		t.Errorf("StrFields[elapsed]: got %q, want %q", row.StrFields["elapsed"], "250ms")
	}
}

// --- overflow types → Extra JSON ---

func TestExtractRow_ErrorGoesToExtra(t *testing.T) {
	row := extractRow(basicEntry(), []zapcore.Field{
		zap.Error(errors.New("something went wrong")),
	})
	if row.Extra == "" {
		t.Fatal("Extra: expected JSON, got empty")
	}
	if !strings.Contains(row.Extra, "something went wrong") {
		t.Errorf("Extra: %q missing error message", row.Extra)
	}
}

func TestExtractRow_ReflectGoesToExtra(t *testing.T) {
	type payload struct {
		Name  string
		Value int
	}
	row := extractRow(basicEntry(), []zapcore.Field{
		zap.Reflect("data", payload{Name: "ion", Value: 42}),
	})
	if !strings.Contains(row.Extra, `"Name"`) {
		t.Errorf("Extra: %q missing reflected struct field", row.Extra)
	}
	if !strings.Contains(row.Extra, "ion") {
		t.Errorf("Extra: %q missing reflected value", row.Extra)
	}
}

func TestExtractRow_MultipleOverflowFields_SingleJSONObject(t *testing.T) {
	type blob struct{ X int }
	row := extractRow(basicEntry(), []zapcore.Field{
		zap.Error(errors.New("oops")),
		zap.Reflect("meta", blob{X: 7}),
	})

	if row.Extra == "" {
		t.Fatal("Extra: expected JSON object, got empty")
	}
	if !strings.HasPrefix(row.Extra, "{") || !strings.HasSuffix(row.Extra, "}") {
		t.Errorf("Extra: expected JSON object, got %q", row.Extra)
	}
	if !strings.Contains(row.Extra, `"error"`) {
		t.Errorf("Extra missing error key: %s", row.Extra)
	}
	if !strings.Contains(row.Extra, `"meta"`) {
		t.Errorf("Extra missing meta key: %s", row.Extra)
	}
}

// --- internal keys filtered out ---

func TestExtractRow_SentinelKeySkipped(t *testing.T) {
	row := extractRow(basicEntry(), []zapcore.Field{
		{Key: core.SentinelKey, Type: zapcore.ReflectType, Interface: "ctx-value"},
		zap.String("keep", "me"),
	})

	if _, found := row.StrFields[core.SentinelKey]; found {
		t.Error("SentinelKey must not appear in StrFields")
	}
	if strings.Contains(row.Extra, core.SentinelKey) {
		t.Error("SentinelKey must not appear in Extra")
	}
	if row.StrFields["keep"] != "me" {
		t.Errorf("non-sentinel field lost: got %q", row.StrFields["keep"])
	}
}

func TestExtractRow_SystemPrefixSkipped(t *testing.T) {
	row := extractRow(basicEntry(), []zapcore.Field{
		{Key: core.SystemFieldPrefix + "hidden", Type: zapcore.StringType, String: "secret"},
		zap.String("visible", "yes"),
	})

	if _, found := row.StrFields[core.SystemFieldPrefix+"hidden"]; found {
		t.Error("SystemFieldPrefix key must be skipped")
	}
	if row.StrFields["visible"] != "yes" {
		t.Errorf("visible field lost: got %q", row.StrFields["visible"])
	}
}

// --- dedicated column routing ---

func TestExtractRow_DedicatedColumnsRouted(t *testing.T) {
	row := extractRow(basicEntry(), []zapcore.Field{
		zap.String("trace_id", "trace-abc"),
		zap.String("span_id", "span-def"),
		zap.String("request_id", "req-ghi"),
		zap.String("user_id", "usr-jkl"),
		zap.String("service", "payment-svc"),
		zap.String("version", "v2.0.0"),
	})

	cases := map[string]string{
		"TraceID": row.TraceID, "SpanID": row.SpanID,
		"RequestID": row.RequestID, "UserID": row.UserID,
		"Service": row.Service, "Version": row.Version,
	}
	wants := map[string]string{
		"TraceID": "trace-abc", "SpanID": "span-def",
		"RequestID": "req-ghi", "UserID": "usr-jkl",
		"Service": "payment-svc", "Version": "v2.0.0",
	}
	for field, got := range cases {
		if got != wants[field] {
			t.Errorf("%s: got %q, want %q", field, got, wants[field])
		}
	}
}

func TestExtractRow_DedicatedColumnsNotInStrFields(t *testing.T) {
	row := extractRow(basicEntry(), []zapcore.Field{
		zap.String("trace_id", "t"),
		zap.String("service", "s"),
	})
	for _, key := range []string{"trace_id", "span_id", "request_id", "user_id", "service", "version"} {
		if _, found := row.StrFields[key]; found {
			t.Errorf("dedicated column key %q leaked into StrFields", key)
		}
	}
}

// --- lazy allocation ---

func TestExtractRow_NoFieldsNoMapsAllocated(t *testing.T) {
	row := extractRow(basicEntry(), nil)

	if row.StrFields != nil {
		t.Error("StrFields must be nil when empty")
	}
	if row.IntFields != nil {
		t.Error("IntFields must be nil when empty")
	}
	if row.FltFields != nil {
		t.Error("FltFields must be nil when empty")
	}
	if row.BoolFields != nil {
		t.Error("BoolFields must be nil when empty")
	}
	if row.Extra != "" {
		t.Errorf("Extra must be empty when no overflow fields: got %q", row.Extra)
	}
}

// --- uint64 overflow ---

func TestExtractRow_LargeUint64_GoesToExtra(t *testing.T) {
	// math.MaxInt64+1 wraps to a negative int64 — silent data corruption
	largeU64 := uint64(math.MaxInt64) + 1
	row := extractRow(basicEntry(), []zapcore.Field{
		{Key: "big", Type: zapcore.Uint64Type, Integer: int64(largeU64)},
	})
	if _, ok := row.IntFields["big"]; ok {
		t.Error("large uint64 (> MaxInt64) must not be stored in IntFields — it would be negative")
	}
	if !strings.Contains(row.Extra, "big") {
		t.Errorf("large uint64 must appear in Extra, got Extra=%q", row.Extra)
	}
}

func TestExtractRow_SmallUint64_StaysInIntFields(t *testing.T) {
	row := extractRow(basicEntry(), []zapcore.Field{
		zap.Uint64("count", 42),
	})
	if row.IntFields["count"] != 42 {
		t.Errorf("IntFields[count]: got %d, want 42", row.IntFields["count"])
	}
	if strings.Contains(row.Extra, "count") {
		t.Error("small uint64 must not appear in Extra")
	}
}

// --- time fields ---

func TestExtractRow_TimeGoesToStrFields(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Nanosecond)
	row := extractRow(basicEntry(), []zapcore.Field{
		zap.Time("when", now),
	})
	if row.StrFields["when"] == "" {
		t.Fatal("TimeType field missing from StrFields")
	}
	if _, err := time.Parse(time.RFC3339Nano, row.StrFields["when"]); err != nil {
		t.Errorf("StrFields[when] is not RFC3339Nano: %v (got %q)", err, row.StrFields["when"])
	}
}

func TestExtractRow_TimeFullGoesToStrFields(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Nanosecond)
	row := extractRow(basicEntry(), []zapcore.Field{
		{Key: "when", Type: zapcore.TimeFullType, Interface: now},
	})
	if row.StrFields["when"] == "" {
		t.Fatal("TimeFullType field missing from StrFields")
	}
}

// --- complex numbers ---

func TestExtractRow_Complex128GoesToStrFields(t *testing.T) {
	row := extractRow(basicEntry(), []zapcore.Field{
		zap.Complex128("c", 3+4i),
	})
	if row.StrFields["c"] == "" {
		t.Fatal("Complex128 field missing from StrFields")
	}
	if !strings.Contains(row.StrFields["c"], "3") {
		t.Errorf("StrFields[c] missing real part: got %q", row.StrFields["c"])
	}
}

func TestExtractRow_Complex64GoesToStrFields(t *testing.T) {
	row := extractRow(basicEntry(), []zapcore.Field{
		zap.Complex64("c", 1+2i),
	})
	if row.StrFields["c"] == "" {
		t.Fatal("Complex64 field missing from StrFields")
	}
}

// --- helpers ---

func basicEntry() zapcore.Entry {
	return zapcore.Entry{
		Level:   zapcore.InfoLevel,
		Time:    time.Now(),
		Message: "msg",
	}
}

func absf(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
