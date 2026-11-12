package trendyol_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"hesapkitap/internal/trendyol"
)

const (
	testEmail    = "firma@ornek.com"
	testPassword = "s3cret-stage-sifre"
)

// fakeJWT builds an unsigned JWT-shaped token carrying the given claims.
func fakeJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	enc := base64.RawURLEncoding
	return enc.EncodeToString([]byte(`{"alg":"none"}`)) + "." + enc.EncodeToString(payload) + ".c2ln"
}

func defaultToken(t *testing.T) string {
	return fakeJWT(t, map[string]any{"companyId": 42, "userId": 7, "n": "A"})
}

// fakeAPI is a fake Trendyol gateway. signinHandler may be nil: then every signin succeeds with a default token.
type fakeAPI struct {
	t      *testing.T
	mux    *http.ServeMux
	srv    *httptest.Server
	logins atomic.Int32
	calls  atomic.Int32 // non-signin requests
}

func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	f := &fakeAPI{t: t, mux: http.NewServeMux()}
	f.srv = httptest.NewServer(f.mux)
	t.Cleanup(f.srv.Close)
	return f
}

// signin registers the signin endpoint; token(n) is called with the 1-based signin count.
func (f *fakeAPI) signin(token func(n int32) string) {
	f.mux.HandleFunc("POST /api/auth/signin", func(w http.ResponseWriter, r *http.Request) {
		n := f.logins.Add(1)
		var body map[string]string
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &body); err != nil || body["email"] != testEmail || body["password"] != testPassword {
			problem(w, http.StatusUnauthorized, "Unauthorized", "bad credentials")
			return
		}
		w.Header().Set("x-access-token", token(n))
		w.WriteHeader(http.StatusOK)
	})
}

// handle registers an API endpoint and counts the call.
func (f *fakeAPI) handle(pattern string, h http.HandlerFunc) {
	f.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		h(w, r)
	})
}

func (f *fakeAPI) client(cfg ...func(*trendyol.Config)) *trendyol.Client {
	f.t.Helper()
	c := trendyol.Config{BaseURL: f.srv.URL, Email: testEmail, Password: testPassword}
	for _, fn := range cfg {
		fn(&c)
	}
	cl, err := trendyol.New(c)
	require.NoError(f.t, err)
	return cl
}

func problem(w http.ResponseWriter, status int, title, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"type":"about:blank","title":%q,"status":%d,"detail":%q,"instance":"/x"}`, title, status, detail)
}

func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, body)
}
