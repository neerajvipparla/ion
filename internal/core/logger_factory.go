package core

import (
	"fmt"
	"os"
	"strings"

	"go.opentelemetry.io/contrib/bridges/otelzap"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/JupiterMetaLabs/ion/internal/config"
)

// ZapFactoryResult holds the result of constructing the zap logger.
type ZapFactoryResult struct {
	Logger       *zap.Logger
	AtomicLevel  zap.AtomicLevel
	OTELProvider *LogProvider
}

// NewZapLogger creates a new configured Zap logger.
// It sets up console, file, and OTEL cores as configured.
func NewZapLogger(cfg config.Config) (*ZapFactoryResult, error) {
	var otelProvider *LogProvider
	var otelCore zapcore.Core
	var err error

	// Determine global level
	globalLevel := parseLevel(cfg.Level)

	// Determine sink-specific levels (defaulting to global)
	consoleLevel := globalLevel
	if cfg.Console.Level != "" {
		consoleLevel = parseLevel(cfg.Console.Level)
	}

	fileLevel := globalLevel
	if cfg.File.Level != "" {
		fileLevel = parseLevel(cfg.File.Level)
	}

	otelLevel := globalLevel
	if cfg.OTEL.Level != "" {
		otelLevel = parseLevel(cfg.OTEL.Level)
	}

	chLevel := globalLevel
	if cfg.ClickHouse.Level != "" {
		chLevel = parseLevel(cfg.ClickHouse.Level)
	}

	// Calculate the minimum level across all ENABLED sinks.
	// This ensures the main atomicLevel (used for SetLevel and early filtering)
	// allows logs to pass if ANY sink needs them.
	minLevel := globalLevel // Start with global (safe default)

	// If a sink is enabled, check if its level is lower (more verbose)
	// zapcore.DebugLevel (-1) < zapcore.InfoLevel (0)
	if cfg.Console.Enabled && consoleLevel < minLevel {
		minLevel = consoleLevel
	}
	if cfg.File.Enabled && fileLevel < minLevel {
		minLevel = fileLevel
	}
	if cfg.OTEL.Enabled && otelLevel < minLevel {
		minLevel = otelLevel
	}
	if cfg.ClickHouse.Enabled && chLevel < minLevel {
		minLevel = chLevel
	}

	// The atomicLevel acts as the master gatekeeper in logger_impl.go
	atomicLevel := zap.NewAtomicLevelAt(minLevel)

	// 1. Setup OTEL if enabled
	if cfg.OTEL.Enabled && cfg.OTEL.Endpoint != "" {
		// Inject Basic Auth header if credentials provided
		cfg.OTEL.Headers = injectBasicAuth(cfg.OTEL.Headers, cfg.OTEL.Username, cfg.OTEL.Password, cfg.OTEL.Protocol)

		otelProvider, err = SetupLogProvider(cfg.OTEL, cfg.ServiceName, cfg.Version)
		if err != nil {
			return nil, fmt.Errorf("otel setup failed: %w", err)
		}

		if otelProvider != nil && otelProvider.LoggerProvider() != nil {
			otelCore = otelzap.NewCore(
				cfg.ServiceName,
				otelzap.WithLoggerProvider(otelProvider.LoggerProvider()),
			)
		}
	}

	// 2. Build Cores
	cores := make([]zapcore.Core, 0, 4)

	// Console
	if cfg.Console.Enabled {
		// Use specific consoleLevel for console cores
		consoleCores := buildConsoleCores(cfg, consoleLevel)
		for _, c := range consoleCores {
			cores = append(cores, NewFilteringCore(c, SentinelKey))
		}
	}

	// File
	if cfg.File.Enabled && cfg.File.Path != "" {
		// Use specific fileLevel for file core
		fileCore := buildFileCore(cfg, fileLevel)
		if fileCore != nil {
			cores = append(cores, NewFilteringCore(fileCore, SentinelKey))
		}
	}

	// OTEL
	if otelCore != nil {
		// Use specific otelLevel for OTEL core
		otelCore = &levelEnforcer{Core: otelCore, level: otelLevel}

		// Filter SentinelKey (internal context carrier) but allow trace_id/span_id
		// to pass through as explicit attributes. This ensures they are present in the
		// log body/attributes for easy regex extraction and visibility in Loki.
		cores = append(cores, NewFilteringCore(otelCore, SentinelKey))
	}

	// 3. Combine
	var core zapcore.Core
	switch len(cores) {
	case 0:
		core = zapcore.NewNopCore()
	case 1:
		core = cores[0]
	default:
		core = zapcore.NewTee(cores...)
	}

	// 4. Build options
	opts := buildZapOptions(cfg)

	// Add Fatal hook to prevent exit on Critical/Fatal logs
	// This ensures Critical() logs as Fatal level but doesn't kill the process.
	opts = append(opts, zap.WithFatalHook(noExitHook{}))

	logger := zap.New(core, opts...)

	return &ZapFactoryResult{
		Logger:       logger,
		AtomicLevel:  atomicLevel,
		OTELProvider: otelProvider,
	}, nil
}

type noExitHook struct{}

func (noExitHook) OnWrite(ce *zapcore.CheckedEntry, fields []zapcore.Field) {
	// Do nothing, preventing os.Exit
}

func buildZapOptions(cfg config.Config) []zap.Option {
	opts := []zap.Option{
		zap.AddCallerSkip(1), // Skip the wrapper methods
	}

	if cfg.Development {
		opts = append(opts, zap.Development())
		opts = append(opts, zap.AddCaller())
		opts = append(opts, zap.AddStacktrace(zapcore.ErrorLevel))
	}

	if cfg.ServiceName != "" {
		opts = append(opts, zap.Fields(zap.String("service", cfg.ServiceName)))
	}
	if cfg.Version != "" {
		opts = append(opts, zap.Fields(zap.String("version", cfg.Version)))
	}

	return opts
}

func buildConsoleCores(cfg config.Config, level zapcore.LevelEnabler) []zapcore.Core {
	encoder := buildConsoleEncoder(cfg)

	if cfg.Console.ErrorsToStderr {
		// stdout: [configLevel, Warn)
		stdoutLevel := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
			return level.Enabled(lvl) && lvl < zapcore.WarnLevel
		})

		// stderr: [Warn, Fatal] AND >= configLevel
		// e.g., if config=Error, stderr only shows Error+ (Warn is suppressed).
		// if config=Debug, stderr shows Warn+.
		stderrLevel := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
			// Helper to check minimum enabled level of the enabler
			// We can't easily get the "min" from a generic LevelEnabler,
			// but we can check if it's enabled for the current lvl.
			//
			// Rule: Log to stderr if it IS enabled by config AND it is >= Warn
			return level.Enabled(lvl) && lvl >= zapcore.WarnLevel
		})

		return []zapcore.Core{
			zapcore.NewCore(encoder, zapcore.Lock(os.Stdout), stdoutLevel),
			zapcore.NewCore(encoder, zapcore.Lock(os.Stderr), stderrLevel),
		}
	}

	return []zapcore.Core{
		zapcore.NewCore(encoder, zapcore.Lock(os.Stdout), level),
	}
}

func buildConsoleEncoder(cfg config.Config) zapcore.Encoder {
	switch cfg.Console.Format {
	case "systemd":
		return buildSystemdEncoder()
	case "pretty":
		return buildPrettyEncoder(cfg)
	case "json":
		return buildJSONEncoder()
	default:
		// Smart defaults based on environment
		if cfg.Development {
			return buildPrettyEncoder(cfg)
		}
		return buildJSONEncoder()
	}
}

// syslogPriority maps Zap levels to syslog priority prefixes (RFC 5424).
func syslogPriority(level zapcore.Level) string {
	switch level {
	case zapcore.DebugLevel:
		return "<7>" // Debug
	case zapcore.InfoLevel:
		return "<6>" // Info
	case zapcore.WarnLevel:
		return "<4>" // Warning
	case zapcore.ErrorLevel:
		return "<3>" // Error
	case zapcore.DPanicLevel, zapcore.PanicLevel, zapcore.FatalLevel:
		return "<2>" // Critical
	default:
		return "<6>" // Default to Info
	}
}

// buildSystemdEncoder creates a console encoder optimized for systemd/journald.
// Output format: <N>LEVEL   Message   key=value key2=value2
// - Priority prefix (<6>, <3>, etc.) is parsed and stripped by journald
// - User sees: LEVEL   Message   key=value
// - No timestamp (journald provides it)
// - No caller (keeps output clean)
func buildSystemdEncoder() zapcore.Encoder {
	encoderCfg := zap.NewDevelopmentEncoderConfig()

	// No timestamp - Journald handles it
	encoderCfg.TimeKey = ""
	encoderCfg.EncodeTime = nil

	// No caller - keep it clean for ops debugging
	encoderCfg.CallerKey = ""
	encoderCfg.EncodeCaller = nil

	// Custom level encoder: outputs "<6>INFO" (priority prefix + text level)
	// Journald strips the <6>, user sees "INFO"
	encoderCfg.EncodeLevel = func(l zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString(syslogPriority(l) + l.CapitalString())
	}

	return zapcore.NewConsoleEncoder(encoderCfg)
}

func buildPrettyEncoder(cfg config.Config) zapcore.Encoder {
	encoderCfg := zap.NewDevelopmentEncoderConfig()
	if cfg.Console.Color {
		encoderCfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
		encoderCfg.EncodeTime = zapcore.TimeEncoderOfLayout("15:04:05.000")
	} else {
		encoderCfg.EncodeLevel = zapcore.CapitalLevelEncoder
		encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	}
	encoderCfg.EncodeCaller = zapcore.ShortCallerEncoder
	return zapcore.NewConsoleEncoder(encoderCfg)
}

func buildJSONEncoder() zapcore.Encoder {
	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "timestamp"
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderCfg.MessageKey = "msg"
	encoderCfg.LevelKey = "level"
	encoderCfg.CallerKey = "caller"
	return zapcore.NewJSONEncoder(encoderCfg)
}

func buildFileCore(cfg config.Config, level zapcore.LevelEnabler) zapcore.Core {
	writer := config.NewFileWriter(cfg.File)
	if writer == nil {
		return nil
	}

	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "timestamp"
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	encoder := zapcore.NewJSONEncoder(encoderCfg)

	return zapcore.NewCore(encoder, zapcore.AddSync(writer), level)
}

func parseLevel(level string) zapcore.Level {
	switch strings.ToLower(level) {
	case "debug":
		return zapcore.DebugLevel
	case "info":
		return zapcore.InfoLevel
	case "warn", "warning":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	case "fatal":
		return zapcore.FatalLevel
	default:
		return zapcore.InfoLevel
	}
}
