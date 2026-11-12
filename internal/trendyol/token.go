package trendyol

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// tokenHeader is the signin answer header that carries the JWT. Later requests send it as
// "Authorization: Bearer <JWT>" (stage'de doğrulandı 2026-11-13; x-access-token ile gönderilince 401 döner).
const tokenHeader = "x-access-token"

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
	company, user, err := parseClaims(token)
	if err != nil {
		return session{}, fmt.Errorf("signin: %w", err)
	}
	return session{token: token, companyID: company, userID: user}, nil
}

// parseClaims reads the ids from the JWT payload (stage'de doğrulandı): userId = "sub" claim'i,
// companyId = "privs" nesnesinin anahtarı. Birden fazla şirket varsa belirsizlik olur ve hata verilir.
// The signature is not verified: this is our own token. The token is never included in an error.
func parseClaims(token string) (companyID, userID int64, err error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 0, 0, errors.New("JWT üç parçalı değil")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return 0, 0, fmt.Errorf("JWT gövdesi çözülemedi: %w", err)
	}
	var claims struct {
		Sub   json.Number                `json:"sub"`
		Privs map[string]json.RawMessage `json:"privs"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		return 0, 0, fmt.Errorf("JWT gövdesi okunamadı: %w", err)
	}
	if userID, err = strconv.ParseInt(claims.Sub.String(), 10, 64); err != nil {
		return 0, 0, errors.New(`JWT "sub" claim'i (kullanıcı kimliği) yok veya sayı değil`)
	}
	if len(claims.Privs) != 1 {
		return 0, 0, fmt.Errorf(`JWT "privs" claim'inde %d şirket var; tam olarak 1 beklenir`, len(claims.Privs))
	}
	for k := range claims.Privs {
		if companyID, err = strconv.ParseInt(k, 10, 64); err != nil {
			return 0, 0, errors.New(`JWT "privs" anahtarı (şirket kimliği) sayı değil`)
		}
	}
	return companyID, userID, nil
}
