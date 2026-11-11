package auth_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hesapkitap/internal/platform/auth"
	"hesapkitap/internal/platform/db/dbgen"
	"hesapkitap/internal/platform/db/dbtest"
)

func newService(t *testing.T) *auth.Service {
	t.Helper()
	pool := dbtest.NewPool(t)
	svc, err := auth.NewService(dbgen.New(pool))
	require.NoError(t, err)
	return svc
}

func TestServiceCreateAndAuthenticate(t *testing.T) {
	svc := newService(t)
	ctx := t.Context()

	u, err := svc.CreateUser(ctx, "  Sahip@Ornek.COM ", "çok-güçlü-şifre")
	require.NoError(t, err)
	assert.Equal(t, "sahip@ornek.com", u.Email, "e-posta normalize edilir")

	got, err := svc.Authenticate(ctx, "SAHIP@ornek.com", "çok-güçlü-şifre")
	require.NoError(t, err)
	assert.Equal(t, u.ID, got.ID)

	_, err = svc.Authenticate(ctx, "sahip@ornek.com", "yanlış-şifre-123")
	assert.ErrorIs(t, err, auth.ErrInvalidCredentials)

	_, err = svc.Authenticate(ctx, "yok@ornek.com", "çok-güçlü-şifre")
	assert.ErrorIs(t, err, auth.ErrInvalidCredentials, "bilinmeyen e-posta ile yanlış şifre aynı hatayı verir")
}

func TestServiceCreateUserValidation(t *testing.T) {
	svc := newService(t)
	ctx := t.Context()

	_, err := svc.CreateUser(ctx, "gecersiz", "çok-güçlü-şifre")
	assert.Error(t, err)

	_, err = svc.CreateUser(ctx, "a@b.co", "kisa")
	assert.Error(t, err)

	_, err = svc.CreateUser(ctx, "a@b.co", "çok-güçlü-şifre")
	require.NoError(t, err)
	_, err = svc.CreateUser(ctx, "A@B.co", "çok-güçlü-şifre")
	assert.ErrorIs(t, err, auth.ErrEmailTaken)
}

func TestServiceLocksAfterRepeatedFailures(t *testing.T) {
	svc := newService(t)
	ctx := t.Context()

	_, err := svc.CreateUser(ctx, "kilit@ornek.com", "çok-güçlü-şifre")
	require.NoError(t, err)

	for range 5 {
		_, err = svc.Authenticate(ctx, "kilit@ornek.com", "yanlış-şifre-123")
		assert.ErrorIs(t, err, auth.ErrInvalidCredentials)
	}
	// 5. hatadan sonra doğru şifre bile kilitli hesapta çalışmaz.
	_, err = svc.Authenticate(ctx, "kilit@ornek.com", "çok-güçlü-şifre")
	assert.ErrorIs(t, err, auth.ErrLocked)
}
