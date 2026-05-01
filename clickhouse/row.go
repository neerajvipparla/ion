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
	Timestamp  time.Time
	Level      string
	Service    string
	Version    string
	Logger     string
	Message    string
	TraceID    string
	SpanID     string
	RequestID  string
	UserID     string
	Caller     string
	StrFields  map[string]string
	IntFields  map[string]int64
	FltFields  map[string]float64
	BoolFields map[string]uint8
	Extra      string // JSON object for overflow/complex field types
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

		// --- Unsigned integers — value stored bit-for-bit in f.Integer ---
		case zapcore.Uint64Type, zapcore.Uint32Type, zapcore.Uint16Type, zapcore.Uint8Type:
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

		// --- Overflow: error message as plain string ---
		case zapcore.ErrorType:
			if err, ok := f.Interface.(error); ok {
				if overflow == nil {
					overflow = make(map[string]any)
				}
				overflow[f.Key] = err.Error()
			}

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
