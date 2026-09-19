package log

import (
	"log/slog"
	"os"
	"sync"
	"time"

	"gitea.com/gitea/gitea-mcp/pkg/flag"

	"go.uber.org/zap"
	"go.uber.org/zap/exp/zapslog"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	defaultLoggerOnce sync.Once
	defaultLogger     *zap.Logger
)

// NewProductionLogger builds a production zap.Logger with LOG-1 redaction.
// Any extraSyncers provided receive a copy of log output.
func NewProductionLogger(extraSyncers ...zapcore.WriteSyncer) *zap.Logger {
	ec := zap.NewProductionEncoderConfig()
	ec.EncodeTime = zapcore.TimeEncoderOfLayout(time.DateTime)
	ec.EncodeLevel = zapcore.CapitalLevelEncoder

	var wss []zapcore.WriteSyncer

	home, _ := os.UserHomeDir()
	if home == "" {
		home = os.TempDir()
	}

	logDir := home + "/.gitea-mcp"
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		// Fallback to temp directory if creation fails
		logDir = os.TempDir()
	}

	wss = append(wss, zapcore.AddSync(&lumberjack.Logger{
		Filename:   logDir + "/gitea-mcp.log",
		MaxSize:    100,
		MaxBackups: 10,
		MaxAge:     30,
	}))

	if flag.Mode == "http" {
		wss = append(wss, zapcore.AddSync(os.Stdout))
	}
	wss = append(wss, extraSyncers...)

	ws := zapcore.NewMultiWriteSyncer(wss...)

	enc := zapcore.NewConsoleEncoder(ec)
	var level zapcore.Level
	if flag.Debug {
		level = zapcore.DebugLevel
	} else {
		level = zapcore.InfoLevel
	}
	core := zapcore.NewCore(enc, ws, level)
	core = NewRedactingCore(core)
	options := []zap.Option{
		zap.AddStacktrace(zapcore.DPanicLevel),
		zap.AddCaller(),
		zap.AddCallerSkip(1),
	}
	return zap.New(core, options...)
}

func Default() *zap.Logger {
	defaultLoggerOnce.Do(func() {
		if defaultLogger != nil {
			return
		}
		defaultLogger = NewProductionLogger()
	})

	return defaultLogger
}

func SetDefault(logger *zap.Logger) {
	if logger != nil {
		defaultLogger = logger
	}
}

// Slog returns a *slog.Logger that writes through the default zap logger,
// so structured logging from dependencies (e.g. the MCP HTTP transport) shares
// the same destination and formatting as the rest of the server.
func Slog() *slog.Logger {
	return slog.New(zapslog.NewHandler(Default().Core()))
}

func Debug(msg string, fields ...zap.Field) {
	Default().Debug(msg, fields...)
}

func Info(msg string, fields ...zap.Field) {
	Default().Info(msg, fields...)
}

func Warn(msg string, fields ...zap.Field) {
	Default().Warn(msg, fields...)
}

func Error(msg string, fields ...zap.Field) {
	Default().Error(msg, fields...)
}

func Panic(msg string, fields ...zap.Field) {
	Default().Panic(msg, fields...)
}

func Debugf(format string, args ...any) {
	Default().Sugar().Debugf(format, args...)
}

func Infof(format string, args ...any) {
	Default().Sugar().Infof(format, args...)
}

func Warnf(format string, args ...any) {
	Default().Sugar().Warnf(format, args...)
}

func Errorf(format string, args ...any) {
	Default().Sugar().Errorf(format, args...)
}

func Fatalf(format string, args ...any) {
	Default().Sugar().Fatalf(format, args...)
}
