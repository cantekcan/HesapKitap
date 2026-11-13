package auth

import (
	"context"
	"net/http"
	"time"

	"github.com/alexedwards/scs/pgxstore"
	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	sessionUserIDKey = "user_id"
	sessionEmailKey  = "user_email"
)

// NewSessionManager returns a scs session manager backed by the Postgres sessions table.
func NewSessionManager(pool *pgxpool.Pool, secureCookie bool) *scs.SessionManager {
	m := scs.New()
	m.Store = pgxstore.New(pool)
	m.Lifetime = 14 * 24 * time.Hour
	m.IdleTimeout = 7 * 24 * time.Hour
	m.Cookie.Name = "hesapkitap_session"
	m.Cookie.HttpOnly = true
	m.Cookie.SameSite = http.SameSiteLaxMode
	m.Cookie.Secure = secureCookie
	m.Cookie.Persist = true
	return m
}

// Login stores the user in a fresh session (the token is renewed to prevent session fixation).
func Login(ctx context.Context, m *scs.SessionManager, u User) error {
	if err := m.RenewToken(ctx); err != nil {
		return err
	}
	m.Put(ctx, sessionUserIDKey, u.ID.String())
	m.Put(ctx, sessionEmailKey, u.Email)
	return nil
}

// Logout destroys the session.
func Logout(ctx context.Context, m *scs.SessionManager) error {
	return m.Destroy(ctx)
}

// SessionUser returns the logged-in user, if any.
func SessionUser(ctx context.Context, m *scs.SessionManager) (User, bool) {
	id, err := uuid.Parse(m.GetString(ctx, sessionUserIDKey))
	if err != nil {
		return User{}, false
	}
	return User{ID: id, Email: m.GetString(ctx, sessionEmailKey)}, true
}

type ctxUserKey struct{}

// WithUser attaches the freshly loaded user (with current permissions) to the request context.
func WithUser(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, ctxUserKey{}, u)
}

// CurrentUser returns the user attached by the auth middleware.
func CurrentUser(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(ctxUserKey{}).(User)
	return u, ok
}
