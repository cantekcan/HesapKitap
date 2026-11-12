// Package trendyol is the only code that talks to the Trendyol E-Faturam API (ALTIN KURAL 2).
// It implements invoice.EInvoiceProvider. The API reference is docs/trendyol-efaturam-api.md.
package trendyol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"hesapkitap/internal/invoice"
)

var _ invoice.EInvoiceProvider = (*Client)(nil)

// Config configures a Client.
type Config struct {
	BaseURL  string
	Email    string
	Password string

	Logger *slog.Logger
}

// Client is a Trendyol E-Faturam API client. It is safe for concurrent use.
type Client struct {
	cfg  Config
	http *http.Client
	log  *slog.Logger

	mu   sync.Mutex
	sess session
}

// New creates a client. No network call happens until the first request (lazy signin).
func New(cfg Config) (*Client, error) {
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("trendyol: BaseURL geçersiz: %q", cfg.BaseURL)
	}
	if cfg.Email == "" || cfg.Password == "" {
		return nil, errors.New("trendyol: e-posta ve şifre zorunlu")
	}
	log := cfg.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Client{
		cfg:  cfg,
		http: &http.Client{Transport: newTransport(), Timeout: requestTimeout},
		log:  log,
	}, nil
}

// response is a fully read HTTP answer.
type response struct {
	status int
	header http.Header
	body   []byte
}

// problem is the application/problem+json body.
type problem struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail"`
	Instance string `json:"instance"`
}

// err maps a non-2xx answer to an error. 403 wraps ErrIPNotWhitelisted, 401 wraps ErrUnauthorized.
func (r *response) err() error {
	if r.status >= 200 && r.status < 300 {
		return nil
	}
	apiErr := &APIError{Status: r.status}
	var p problem
	// Cloudflare'in 403 sayfası HTML'dir; JSON değilse sadece durum kodu kalır (gövde loga/hataya girmez).
	if json.Unmarshal(r.body, &p) == nil {
		apiErr.Type, apiErr.Title, apiErr.Detail, apiErr.Instance = p.Type, p.Title, p.Detail, p.Instance
	}
	switch r.status {
	case http.StatusForbidden:
		return fmt.Errorf("%w: %w", ErrIPNotWhitelisted, apiErr)
	case http.StatusUnauthorized:
		return fmt.Errorf("%w: %w", ErrUnauthorized, apiErr)
	default:
		return apiErr
	}
}

// send performs one HTTP round trip. token may be empty (signin).
// Neither headers nor bodies are logged: they contain the token and the password.
func (c *Client) send(ctx context.Context, method, path string, query url.Values, body []byte, token string) (*response, error) {
	u := c.cfg.BaseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return nil, fmt.Errorf("istek oluşturulamadı: %w", err)
	}
	req.Header.Set("Accept", "application/json, text/plain")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set(tokenHeader, token)
	}

	start := time.Now()
	httpResp, err := c.http.Do(req)
	if err != nil {
		c.log.WarnContext(ctx, "trendyol isteği başarısız", "method", method, "path", path, "err", err)
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	b, err := io.ReadAll(io.LimitReader(httpResp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("%s %s: cevap okunamadı: %w", method, path, err)
	}
	c.log.DebugContext(ctx, "trendyol isteği", "method", method, "path", path,
		"status", httpResp.StatusCode, "duration_ms", time.Since(start).Milliseconds())
	return &response{status: httpResp.StatusCode, header: httpResp.Header, body: b}, nil
}

// do sends an authenticated request. On 401 it signs in once more and repeats the same request once
// (ALTIN KURAL 8). in is JSON-encoded; out (if not nil) receives the decoded JSON answer, or the trimmed
// text if out is a *string (text/plain endpoints).
func (c *Client) do(ctx context.Context, method, path string, query url.Values, in, out any) error {
	var body []byte
	if in != nil {
		var err error
		if body, err = json.Marshal(in); err != nil {
			return fmt.Errorf("%s %s: istek gövdesi hazırlanamadı: %w", method, path, err)
		}
	}

	var resp *response
	for attempt := 0; attempt < 2; attempt++ {
		s, err := c.ensureSession(ctx)
		if err != nil {
			return err
		}
		if resp, err = c.send(ctx, method, path, query, body, s.token); err != nil {
			return err
		}
		if resp.status != http.StatusUnauthorized {
			break
		}
		c.invalidate(s.token)
	}

	if err := resp.err(); err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	switch o := out.(type) {
	case nil:
		return nil
	case *string:
		*o = strings.TrimSpace(string(resp.body))
		return nil
	default:
		if err := json.Unmarshal(resp.body, out); err != nil {
			return fmt.Errorf("%s %s: cevap çözülemedi: %w", method, path, err)
		}
		return nil
	}
}

// Authenticate signs in if there is no session yet. It checks the credentials, the IP whitelist and the
// JWT claim names without issuing any invoice call (used by stage tests and a future settings screen).
func (c *Client) Authenticate(ctx context.Context) error {
	_, err := c.ensureSession(ctx)
	return err
}

// ids returns the company and user id of the current session (signing in if needed).
func (c *Client) ids(ctx context.Context) (companyID, userID int64, err error) {
	s, err := c.ensureSession(ctx)
	if err != nil {
		return 0, 0, err
	}
	return s.companyID, s.userID, nil
}
