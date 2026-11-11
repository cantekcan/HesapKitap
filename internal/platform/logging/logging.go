// Package logging builds the application's slog logger with secret redaction.
package logging

import (
	"io"
	"log/slog"
	"regexp"
	"strings"
)

// Redacted replaces any sensitive value in log output.
const Redacted = "[REDACTED]"

var (
	sensitiveKeys = []string{"password", "passwd", "secret", "token", "authorization", "cookie", "api_key", "apikey"}

	// key=value / key: value / "key":"value" biçimleri ve Bearer değerleri.
	keyValue = regexp.MustCompile(`(?i)((?:x-access-token|x-refresh-token|password|passwd|secret|authorization)["']?\s*[:=]\s*["']?)(Bearer\s+\S+|[^\s,;"'}]+)`)
	// JWT: üç parçalı base64url, ilk parça "eyJ" ile başlar.
	jwt = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]*`)
)

// New returns a JSON logger that redacts tokens and passwords.
func New(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: ReplaceAttr,
	}))
}

// ReplaceAttr redacts sensitive attribute keys and secret-looking substrings in string values,
// error messages and the log message itself.
func ReplaceAttr(_ []string, a slog.Attr) slog.Attr {
	if a.Key == slog.MessageKey {
		return slog.String(a.Key, MaskText(a.Value.String()))
	}
	if isSensitiveKey(a.Key) {
		return slog.String(a.Key, Redacted)
	}
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return slog.String(a.Key, MaskText(v.String()))
	case slog.KindAny:
		if err, ok := v.Any().(error); ok {
			return slog.String(a.Key, MaskText(err.Error()))
		}
	default:
	}
	return a
}

// MaskText hides secrets embedded in free text.
func MaskText(s string) string {
	s = keyValue.ReplaceAllString(s, "${1}"+Redacted)
	return jwt.ReplaceAllString(s, Redacted)
}

func isSensitiveKey(key string) bool {
	k := strings.ToLower(key)
	for _, s := range sensitiveKeys {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}
