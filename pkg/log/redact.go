package log

import (
	"regexp"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	oauthQueryRegex = regexp.MustCompile(`(?i)(/oauth/[a-zA-Z0-9_\-]+)\?[^\s"'>]+`)
	authHeaderRegex = regexp.MustCompile(`(?i)\b(authorization|cookie|set-cookie)\s*[:=]\s*[^\r\n]+`)
	bearerRegex     = regexp.MustCompile(`(?i)\b(bearer\s+)[a-zA-Z0-9_\-\.]+`)
	queryParamRegex = regexp.MustCompile(`(?i)(code|client_secret|code_verifier|refresh_token)=([^&\s"'>]+)`)
)

// RedactString sanitizes sensitive credentials and /oauth/* query strings (LOG-1).
func RedactString(s string) string {
	s = oauthQueryRegex.ReplaceAllString(s, "$1?[REDACTED]")
	s = authHeaderRegex.ReplaceAllString(s, "$1: [REDACTED]")
	s = bearerRegex.ReplaceAllString(s, "$1[REDACTED]")
	s = queryParamRegex.ReplaceAllString(s, "$1=[REDACTED]")
	return s
}

func isSensitiveKey(key string) bool {
	switch key {
	case "authorization", "cookie", "set-cookie",
		"code", "client_secret", "code_verifier",
		"token", "refresh_token", "access_token":
		return true
	}
	return false
}

func redactFields(fields []zapcore.Field) []zapcore.Field {
	if len(fields) == 0 {
		return fields
	}
	out := make([]zapcore.Field, len(fields))
	for i, f := range fields {
		key := strings.ToLower(f.Key)
		if isSensitiveKey(key) {
			out[i] = zap.String(f.Key, "[REDACTED]")
			continue
		}
		if f.Type == zapcore.StringType {
			out[i] = zap.String(f.Key, RedactString(f.String))
			continue
		}
		out[i] = f
	}
	return out
}

type redactingCore struct {
	zapcore.Core
}

// NewRedactingCore wraps a zapcore.Core to redact sensitive fields and patterns (LOG-1).
func NewRedactingCore(core zapcore.Core) zapcore.Core {
	return &redactingCore{Core: core}
}

func (r *redactingCore) With(fields []zapcore.Field) zapcore.Core {
	return &redactingCore{Core: r.Core.With(redactFields(fields))}
}

func (r *redactingCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if r.Enabled(ent.Level) {
		return ce.AddCore(ent, r)
	}
	return ce
}

func (r *redactingCore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	ent.Message = RedactString(ent.Message)
	return r.Core.Write(ent, redactFields(fields))
}
