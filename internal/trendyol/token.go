package trendyol

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// tokenHeader carries the JWT, both in the signin answer and on every later request.
// ⚠️ DOĞRULA (docs/trendyol-efaturam-api.md, Açık Sorular 1): bireysel entegrasyonda stage'de teyit edilecek.
const tokenHeader = "x-access-token"

// Default JWT claim names. ⚠️ DOĞRULA (Açık Sorular 2): Config ile değiştirilebilir.
const (
	defaultCompanyIDClaim = "companyId"
	defaultUserIDClaim    = "userId"
)

// session is the signed-in state: the JWT and the ids read from it.
type session struct {
	token     string
	companyID int64
	userID    int64
}

type signinRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// ensureSession returns the current session, signing in first if there is none.
// The mutex is held during signin on purpose: concurrent callers wait for one login instead of racing.
func (c *Client) ensureSession(ctx context.Context) (session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sess.token != "" {
		return c.sess, nil
	}
	s, err := c.signIn(ctx)
	if err != nil {
		return session{}, err
	}
	c.sess = s
	return s, nil
}

// invalidate drops the session, but only if it is still the one that just got a 401.
// If another goroutine already logged in again, its fresh token is kept.
func (c *Client) invalidate(stale string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sess.token == stale {
		c.sess = session{}
	}
}

func (c *Client) signIn(ctx context.Context) (session, error) {
	body, err := json.Marshal(signinRequest{Email: c.cfg.Email, Password: c.cfg.Password}) //nolint:gosec // signin gövdesi şifreyi taşımak zorunda
	if err != nil {
		return session{}, fmt.Errorf("signin isteği hazırlanamadı: %w", err)
	}
	resp, err := c.send(ctx, http.MethodPost, "/api/auth/signin", nil, body, "")
	if err != nil {
		return session{}, fmt.Errorf("signin: %w", err)
	}
	if resp.status == http.StatusUnauthorized {
		return session{}, fmt.Errorf("signin: %w", ErrInvalidCredentials)
	}
	if err := resp.err(); err != nil {
		return session{}, fmt.Errorf("signin: %w", err)
	}

	token := resp.header.Get(tokenHeader)
	if token == "" {
		return session{}, fmt.Errorf("signin: cevapta %q header'ı yok", tokenHeader)
	}
	company, user, err := parseClaims(token, c.cfg.CompanyIDClaim, c.cfg.UserIDClaim)
	if err != nil {
		return session{}, fmt.Errorf("signin: %w", err)
	}
	return session{token: token, companyID: company, userID: user}, nil
}

// parseClaims reads companyId and userId from the JWT payload. The signature is not verified:
// this is our own token, only the ids are needed. The token is never included in an error.
func parseClaims(token, companyClaim, userClaim string) (companyID, userID int64, err error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 0, 0, errors.New("JWT üç parçalı değil")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return 0, 0, fmt.Errorf("JWT gövdesi çözülemedi: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var claims map[string]any
	if err := dec.Decode(&claims); err != nil {
		return 0, 0, fmt.Errorf("JWT gövdesi okunamadı: %w", err)
	}
	if companyID, err = int64Claim(claims, companyClaim); err != nil {
		return 0, 0, err
	}
	if userID, err = int64Claim(claims, userClaim); err != nil {
		return 0, 0, err
	}
	return companyID, userID, nil
}

func int64Claim(claims map[string]any, name string) (int64, error) {
	v, ok := claims[name]
	if !ok {
		return 0, fmt.Errorf("JWT içinde %q claim'i yok (claim adını stage'de doğrulayıp Config'e verin)", name)
	}
	var s string
	switch t := v.(type) {
	case json.Number:
		s = t.String()
	case string:
		s = t
	default:
		return 0, fmt.Errorf("JWT %q claim'i sayı değil", name)
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("JWT %q claim'i sayıya çevrilemedi: %w", name, err)
	}
	return n, nil
}
