package clickhouse

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"go.uber.org/zap/zapcore"

	"github.com/JupiterMetaLabs/ion/internal/core"
)

// logRow is the internal representation of a single log entry
// as it will be stored in ClickHouse.
type logRow struct {
	Timestamp  time.Time         `ch:"timestamp"`
	Level      string            `ch:"level"`
	Service    string            `ch:"service"`
	Version    string            `ch:"version"`
	Logger     string            `ch:"logger"`
	Message    string            `ch:"message"`
	TraceID    string            `ch:"trace_id"`
	SpanID     string            `ch:"span_id"`
	RequestID  string            `ch:"request_id"`
	UserID     string            `ch:"user_id"`
	Caller     string            `ch:"caller"`
	StrFields  map[string]string  `ch:"str_fields"`
	IntFields  map[string]int64   `ch:"int_fields"`
	FltFields  map[string]float64 `ch:"flt_fields"`
	BoolFields map[string]uint8   `ch:"bool_fields"`
	Extra      string             `ch:"extra"`
}

// dedicatedKeys are field keys that map directly to top-level logRow columns
// instead of the typed map columns.
var dedicatedKeys = map[string]bool{
	"trace_id":   true,
	"span_id":    true,
	"request_id": true,
	"user_id":    true,
	"service":    true,
	"version":    true,
}

// extractRow converts a zapcore Entry and its fields into a logRow.
// It runs once per log call in the caller's goroutine and must stay allocation-lean
// for fields that are typed primitives.
//
// Field routing:
//   - Internal sentinel/system keys  → dropped
//   - Dedicated column keys          → top-level row fields
//   - String, Int, Uint, Float, Bool → typed map columns (lazy-allocated)
//   - Duration, Stringer             → StrFields (human-readable string)
//   - Error, Reflect, and others     → Extra (single JSON object, lazy-built)
func extractRow(entry zapcore.Entry, fields []zapcore.Field) logRow {
	row := logRow{
		Timestamp: entry.Time,
		Level:     entry.Level.String(),
		Logger:    entry.LoggerName,
		Message:   entry.Message,
	}

	if entry.Caller.Defined {
		row.Caller = entry.Caller.String()
	}

	if len(fields) == 0 {
		return row
	}

	// overflow accumulates fields that cannot be stored in typed columns.
	// Lazy-allocated: nil until the first overflow field is encountered.
	var overflow map[string]any

	for _, f := range fields {
		// Drop internal ion infrastructure keys.
		if f.Key == core.SentinelKey || strings.HasPrefix(f.Key, core.SystemFieldPrefix) {
			continue
		}

		// Route dedicated column keys to top-level fields.
		if dedicatedKeys[f.Key] {
			switch f.Key {
			case "trace_id":
				row.TraceID = f.String
			case "span_id":
				row.SpanID = f.String
			case "request_id":
				row.RequestID = f.String
			case "user_id":
				row.UserID = f.String
			case "service":
				row.Service = f.String
			case "version":
				row.Version = f.String
			}
			continue
		}

		switch f.Type {
		// --- String-like ---
		case zapcore.StringType:
			if row.StrFields == nil {
				row.StrFields = make(map[string]string)
			}
			row.StrFields[f.Key] = f.String

		case zapcore.DurationType:
			if row.StrFields == nil {
				row.StrFields = make(map[string]string)
			}
			row.StrFields[f.Key] = time.Duration(f.Integer).String()

		case zapcore.StringerType:
			if row.StrFields == nil {
				row.StrFields = make(map[string]string)
			}
			if s, ok := f.Interface.(fmt.Stringer); ok {
				row.StrFields[f.Key] = s.String()
			}

		// --- Signed integers — value in f.Integer ---
		case zapcore.Int64Type, zapcore.Int32Type, zapcore.Int16Type, zapcore.Int8Type:
			if row.IntFields == nil {
				row.IntFields = make(map[string]int64)
			}
			row.IntFields[f.Key] = f.Integer

		// --- Unsigned integers ---
		// Uint64 values > math.MaxInt64 cannot round-trip through Int64 without
		// becoming negative. Route them to overflow instead of silently corrupting.
		case zapcore.Uint64Type:
			if u := uint64(f.Integer); u > math.MaxInt64 {
				if overflow == nil {
					overflow = make(map[string]any)
				}
				overflow[f.Key] = u
			} else {
				if row.IntFields == nil {
					row.IntFields = make(map[string]int64)
				}
				row.IntFields[f.Key] = f.Integer
			}

		case zapcore.Uint32Type, zapcore.Uint16Type, zapcore.Uint8Type:
			if row.IntFields == nil {
				row.IntFields = make(map[string]int64)
			}
			row.IntFields[f.Key] = f.Integer

		// --- Floats — bits packed into f.Integer by zap ---
		case zapcore.Float64Type:
			if row.FltFields == nil {
				row.FltFields = make(map[string]float64)
			}
			row.FltFields[f.Key] = math.Float64frombits(uint64(f.Integer))

		case zapcore.Float32Type:
			if row.FltFields == nil {
				row.FltFields = make(map[string]float64)
			}
			row.FltFields[f.Key] = float64(math.Float32frombits(uint32(f.Integer)))

		// --- Bool — 0 or 1 in f.Integer ---
		case zapcore.BoolType:
			if row.BoolFields == nil {
				row.BoolFields = make(map[string]uint8)
			}
			row.BoolFields[f.Key] = uint8(f.Integer) //nolint:gosec // Integer is always 0 or 1 for BoolType

		// --- ByteString — []byte that represents a valid UTF-8 string ---
		// zap.ByteString() uses this type. JSON encoders treat it as a string,
		// not as binary (base64). Route to StrFields for consistency.
		case zapcore.ByteStringType:
			if b, ok := f.Interface.([]byte); ok {
				if row.StrFields == nil {
					row.StrFields = make(map[string]string)
				}
				row.StrFields[f.Key] = string(b)
			}

		// --- Overflow: error message as plain string ---
		case zapcore.ErrorType:
			if err, ok := f.Interface.(error); ok {
				if overflow == nil {
					overflow = make(map[string]any)
				}
				overflow[f.Key] = err.Error()
			}

		// --- Time — stored as UnixNano in f.Integer, timezone in f.Interface ---
		case zapcore.TimeType:
			t := time.Unix(0, f.Integer)
			if loc, ok := f.Interface.(*time.Location); ok && loc != nil {
				t = t.In(loc)
			}
			if row.StrFields == nil {
				row.StrFields = make(map[string]string)
			}
			row.StrFields[f.Key] = t.Format(time.RFC3339Nano)

		// --- TimeFullType — full time.Time in f.Interface ---
		case zapcore.TimeFullType:
			if t, ok := f.Interface.(time.Time); ok {
				if row.StrFields == nil {
					row.StrFields = make(map[string]string)
				}
				row.StrFields[f.Key] = t.Format(time.RFC3339Nano)
			}

		// --- Complex numbers — json.Marshal cannot handle complex types ---
		case zapcore.Complex128Type:
			if c, ok := f.Interface.(complex128); ok {
				if row.StrFields == nil {
					row.StrFields = make(map[string]string)
				}
				row.StrFields[f.Key] = fmt.Sprintf("%v", c)
			}

		case zapcore.Complex64Type:
			if c, ok := f.Interface.(complex64); ok {
				if row.StrFields == nil {
					row.StrFields = make(map[string]string)
				}
				row.StrFields[f.Key] = fmt.Sprintf("%v", c)
			}

		// --- Namespace — not representable in a flat Map schema ---
		case zapcore.NamespaceType:

		// --- Skip intentional no-ops ---
		case zapcore.SkipType:

		// --- Overflow: everything else marshalled as JSON value ---
		default:
			if f.Interface != nil {
				if overflow == nil {
					overflow = make(map[string]any)
				}
				overflow[f.Key] = f.Interface
			}
		}
	}

	if len(overflow) > 0 {
		if b, err := json.Marshal(overflow); err == nil {
			row.Extra = string(b)
		}
		// json.Marshal failure: silently omit Extra — other fields still written.
	}

	return row
}
