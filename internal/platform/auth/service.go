// Package auth implements password hashing, user lookup and server-side sessions.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"hesapkitap/internal/platform/db/dbgen"
)

var (
	// ErrInvalidCredentials is returned for an unknown e-mail or a wrong password.
	ErrInvalidCredentials = errors.New("e-posta veya şifre hatalı")
	// ErrLocked is returned while the account is temporarily locked after repeated failures.
	ErrLocked = errors.New("çok fazla hatalı deneme, hesap geçici olarak kilitlendi")
	// ErrEmailTaken is returned when a user with that e-mail already exists.
	ErrEmailTaken = errors.New("bu e-posta ile kayıtlı bir kullanıcı var")
)

// User is the authenticated user as seen by the rest of the application.
type User struct {
	ID      uuid.UUID
	Email   string
	IsAdmin bool
	Active  bool
	Perms   map[Permission]bool
}

func toUser(u dbgen.User) User {
	perms := make(map[Permission]bool, len(u.Permissions))
	for _, p := range u.Permissions {
		perms[Permission(p)] = true
	}
	return User{ID: u.ID, Email: u.Email, IsAdmin: u.IsAdmin, Active: u.Active, Perms: perms}
}

// Service authenticates users against the database.
type Service struct {
	q   *dbgen.Queries
	now func() time.Time
	// dummyHash is verified when the e-mail is unknown so response time does not reveal which e-mails exist.
	dummyHash string
}

// NewService creates a Service.
func NewService(q *dbgen.Queries) (*Service, error) {
	dummy, err := HashPassword("zamanlama-eşitleme")
	if err != nil {
		return nil, err
	}
	return &Service{q: q, now: time.Now, dummyHash: dummy}, nil
}

// NormalizeEmail trims and lower-cases an e-mail address. strings.ToLower is locale independent,
// so "I" never becomes a Turkish dotless "ı".
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// Authenticate checks the credentials and applies the lockout policy.
func (s *Service) Authenticate(ctx context.Context, email, password string) (User, error) {
	u, err := s.q.GetUserByEmail(ctx, NormalizeEmail(email))
	if errors.Is(err, pgx.ErrNoRows) {
		_, _ = VerifyPassword(password, s.dummyHash)
		return User{}, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, fmt.Errorf("kullanıcı aranamadı: %w", err)
	}

	if !u.Active {
		_, _ = VerifyPassword(password, s.dummyHash)
		return User{}, ErrInvalidCredentials
	}

	if u.LockedUntil.Valid && u.LockedUntil.Time.After(s.now()) {
		return User{}, ErrLocked
	}

	ok, err := VerifyPassword(password, u.PasswordHash)
	if err != nil {
		return User{}, fmt.Errorf("şifre doğrulanamadı: %w", err)
	}
	if !ok {
		if err := s.q.RecordFailedLogin(ctx, u.ID); err != nil {
			return User{}, fmt.Errorf("hatalı giriş kaydedilemedi: %w", err)
		}
		return User{}, ErrInvalidCredentials
	}

	if err := s.q.ResetLoginFailures(ctx, u.ID); err != nil {
		return User{}, fmt.Errorf("giriş sayacı sıfırlanamadı: %w", err)
	}
	return toUser(u), nil
}

// UserByID loads a user, e.g. to validate a session.
func (s *Service) UserByID(ctx context.Context, id uuid.UUID) (User, error) {
	u, err := s.q.GetUserByID(ctx, id)
	if err != nil {
		return User{}, fmt.Errorf("kullanıcı getirilemedi: %w", err)
	}
	return toUser(u), nil
}

// CreateUser adds a user (the first user ever created is an admin). There is no sign-up screen.
func (s *Service) CreateUser(ctx context.Context, email, password string) (User, error) {
	n, err := s.q.CountUsers(ctx)
	if err != nil {
		return User{}, fmt.Errorf("kullanıcılar sayılamadı: %w", err)
	}
	return s.CreateUserWithAccess(ctx, email, password, n == 0, nil)
}

func permStrings(perms []Permission) []string {
	out := []string{}
	for _, p := range perms {
		if ValidPermission(string(p)) {
			out = append(out, string(p))
		}
	}
	return out
}

// ErrLastAdmin is returned when a change would leave the system without an active admin.
var ErrLastAdmin = errors.New("en az bir etkin yönetici kalmalı")

// ListUsers returns all users, admins first.
func (s *Service) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.q.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("kullanıcılar okunamadı: %w", err)
	}
	out := make([]User, len(rows))
	for i, r := range rows {
		out[i] = toUser(r)
	}
	return out, nil
}

// UpdateAccess changes role, active flag and permissions. The last active admin can not be demoted or disabled.
func (s *Service) UpdateAccess(ctx context.Context, id uuid.UUID, admin, active bool, perms []Permission) error {
	cur, err := s.q.GetUserByID(ctx, id)
	if err != nil {
		return fmt.Errorf("kullanıcı getirilemedi: %w", err)
	}
	if cur.IsAdmin && cur.Active && (!admin || !active) {
		n, err := s.q.CountActiveAdmins(ctx)
		if err != nil {
			return err
		}
		if n <= 1 {
			return ErrLastAdmin
		}
	}
	return s.q.UpdateUserAccess(ctx, dbgen.UpdateUserAccessParams{ID: id, IsAdmin: admin, Active: active, Permissions: permStrings(perms)})
}

// SetPassword replaces a user's password (admin action) and clears any lockout.
func (s *Service) SetPassword(ctx context.Context, id uuid.UUID, password string) error {
	if len([]rune(password)) < MinPasswordLength {
		return fmt.Errorf("şifre en az %d karakter olmalı", MinPasswordLength)
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	return s.q.SetUserPassword(ctx, dbgen.SetUserPasswordParams{ID: id, PasswordHash: hash})
}

// CreateUserWithAccess adds a user with a role and explicit permissions (unknown keys are ignored).
func (s *Service) CreateUserWithAccess(ctx context.Context, email, password string, admin bool, perms []Permission) (User, error) {
	email = NormalizeEmail(email)
	if _, err := mail.ParseAddress(email); err != nil {
		return User{}, fmt.Errorf("e-posta geçersiz: %w", err)
	}
	if len([]rune(password)) < MinPasswordLength {
		return User{}, fmt.Errorf("şifre en az %d karakter olmalı", MinPasswordLength)
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	u, err := s.q.CreateUserWithAccess(ctx, dbgen.CreateUserWithAccessParams{Email: email, PasswordHash: hash, IsAdmin: admin, Permissions: permStrings(perms)})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return User{}, ErrEmailTaken
	}
	if err != nil {
		return User{}, fmt.Errorf("kullanıcı oluşturulamadı: %w", err)
	}
	return toUser(u), nil
}
