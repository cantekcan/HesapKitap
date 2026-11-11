package auth_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hesapkitap/internal/platform/auth"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := auth.HashPassword("doğru-şifre-123")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(hash, "$argon2id$v=19$"))

	ok, err := auth.VerifyPassword("doğru-şifre-123", hash)
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = auth.VerifyPassword("yanlış-şifre-123", hash)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestHashPasswordIsSalted(t *testing.T) {
	a, err := auth.HashPassword("aynı-şifre-123")
	require.NoError(t, err)
	b, err := auth.HashPassword("aynı-şifre-123")
	require.NoError(t, err)
	assert.NotEqual(t, a, b)
}

func TestVerifyPasswordRejectsMalformedHash(t *testing.T) {
	for _, h := range []string{"", "düz-metin", "$bcrypt$x$y$z$w", "$argon2id$v=19$m=1,t=1,p=1$!!$!!"} {
		ok, err := auth.VerifyPassword("x", h)
		assert.False(t, ok, h)
		assert.Error(t, err, h)
	}
}
