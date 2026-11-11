package logging_test

import (
	"bytes"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"

	"hesapkitap/internal/platform/logging"
)

const sampleJWT = "eyJhbGciOiJIUzI1NiJ9.eyJjb21wYW55SWQiOjF9.c2lnbmF0dXJl"

func TestLoggerRedactsSecrets(t *testing.T) {
	var buf bytes.Buffer
	log := logging.New(&buf, slog.LevelInfo)

	log.Info("giriş yapıldı password=hunter2 token "+sampleJWT,
		"password", "hunter2",
		"x-access-token", sampleJWT,
		"Authorization", "Bearer "+sampleJWT,
		"detail", "x-access-token: "+sampleJWT,
		"err", errors.New("istek başarısız: authorization=Bearer abc.def"),
		"email", "a@b.c",
	)

	out := buf.String()
	assert.NotContains(t, out, "hunter2")
	assert.NotContains(t, out, sampleJWT)
	assert.NotContains(t, out, "abc.def")
	assert.Contains(t, out, logging.Redacted)
	assert.Contains(t, out, "a@b.c", "hassas olmayan alanlar korunmalı")
}
