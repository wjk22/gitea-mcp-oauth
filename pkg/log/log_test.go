package log

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/exp/zapslog"
	"go.uber.org/zap/zapcore"
)

// TestLogRedactionLOG1 verifies LOG-1 redaction across zap and slog.
func TestLogRedactionLOG1(t *testing.T) {
	var buf bytes.Buffer
	encCfg := zap.NewProductionEncoderConfig()
	core := zapcore.NewCore(zapcore.NewConsoleEncoder(encCfg), zapcore.AddSync(&buf), zapcore.DebugLevel)
	logger := zap.New(NewRedactingCore(core))

	secrets := []string{
		"super-secret-bearer-token",
		"super-secret-cookie-value",
		"super-secret-set-cookie-val",
		"secret_auth_code_12345",
		"secret_state_67890",
		"secret_client_secret_xyz",
	}

	// 1. Message-level logging via Zap
	logger.Info("Incoming request with Authorization: Bearer super-secret-bearer-token")
	logger.Info("Cookie header: Cookie: session=super-secret-cookie-value")
	logger.Info("Set-Cookie header: Set-Cookie: id=super-secret-set-cookie-val; Path=/")
	logger.Info("Redirecting to /oauth/authorize?response_type=code&code=secret_auth_code_12345&client_secret=secret_client_secret_xyz")
	logger.Info("Callback received /oauth/callback?code=secret_auth_code_12345&state=secret_state_67890")

	// 2. Field-level logging via Zap
	logger.Info("Fields test",
		zap.String("Authorization", "Bearer super-secret-bearer-token"),
		zap.String("Cookie", "session=super-secret-cookie-value"),
		zap.String("Set-Cookie", "id=super-secret-set-cookie-val"),
		zap.String("path", "/oauth/token?code=secret_auth_code_12345"),
	)

	// 3. Structured logging via Slog
	slogLogger := slog.New(zapslog.NewHandler(logger.Core()))
	slogLogger.Info("Slog message /oauth/authorize?state=secret_state_67890",
		slog.String("Authorization", "Bearer super-secret-bearer-token"),
	)

	out := buf.String()

	for _, secret := range secrets {
		if strings.Contains(out, secret) {
			t.Errorf("LOG-1 violation: log output leaked secret %q:\n%s", secret, out)
		}
	}

	if !strings.Contains(out, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in log output, got:\n%s", out)
	}
}
