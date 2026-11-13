package httpserver_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hesapkitap/internal/customer"
	"hesapkitap/internal/invoice"
	"hesapkitap/internal/payment"
	"hesapkitap/internal/platform/auth"
	"hesapkitap/internal/platform/db"
	"hesapkitap/internal/platform/db/dbgen"
	"hesapkitap/internal/platform/db/dbtest"
	httpserver "hesapkitap/internal/platform/http"
	"hesapkitap/internal/product"
)

const (
	testEmail    = "sahip@ornek.com"
	testPassword = "çok-güçlü-şifre"
)

// result is a fully-read HTTP response.
type result struct {
	Status int
	Header http.Header
	Body   string
}

type testApp struct {
	t        *testing.T
	srv      *httptest.Server
	client   *http.Client
	provider *fakeProvider
	jobs     *fakeEnqueuer
	invoices *invoice.Service
	authSvc  *auth.Service
}

// other returns a second browser (own cookie jar) talking to the same server.
func (a *testApp) other() *testApp {
	jar, err := cookiejar.New(nil)
	require.NoError(a.t, err)
	b := *a
	b.client = &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &b
}

func (a *testApp) loginAs(email, password string) result {
	a.t.Helper()
	return a.do(http.MethodPost, "/login", url.Values{"email": {email}, "password": {password}}, nil)
}

func newTestApp(t *testing.T) *testApp {
	t.Helper()
	pool := dbtest.NewPool(t)
	svc, err := auth.NewService(dbgen.New(pool))
	require.NoError(t, err)
	_, err = svc.CreateUser(t.Context(), testEmail, testPassword)
	require.NoError(t, err)

	provider, jobs := newFakeProvider(), &fakeEnqueuer{}
	customers := &customer.Service{Store: db.NewCustomerStore(dbgen.New(pool)), Provider: provider}
	invoiceStore := db.NewInvoiceStore(pool, jobs)
	invoices := &invoice.Service{Store: invoiceStore, Issues: invoiceStore, Provider: provider, Recipients: customers}
	h, err := httpserver.New(httpserver.Deps{
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Pool:      pool,
		Sessions:  auth.NewSessionManager(pool, false),
		Auth:      svc,
		Customers: customers,
		Products:  &product.Service{Store: db.NewProductStore(dbgen.New(pool))},
		Invoices:  invoices,
		Payments:  &payment.Service{Store: db.NewPaymentStore(dbgen.New(pool))},
		Ledger:    db.NewLedgerStore(dbgen.New(pool)),
		Reports:   db.NewReportStore(dbgen.New(pool)),
		Audit:     db.NewAuditStore(dbgen.New(pool)),
	})
	require.NoError(t, err)

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	return &testApp{
		t: t, srv: srv, provider: provider, jobs: jobs, invoices: invoices, authSvc: svc,
		client: &http.Client{
			Jar:           jar,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// do sends a request; form (if non-nil) is sent as application/x-www-form-urlencoded.
func (a *testApp) do(method, path string, form url.Values, headers map[string]string) result {
	a.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(a.t.Context(), method, a.srv.URL+path, body)
	require.NoError(a.t, err)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := a.client.Do(req)
	require.NoError(a.t, err)
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	require.NoError(a.t, err)
	return result{Status: resp.StatusCode, Header: resp.Header, Body: string(b)}
}

func (a *testApp) get(path string) result { a.t.Helper(); return a.do(http.MethodGet, path, nil, nil) }

func (a *testApp) login(password string) result {
	a.t.Helper()
	return a.do(http.MethodPost, "/login", url.Values{"email": {testEmail}, "password": {password}}, nil)
}

func TestHealthEndpoints(t *testing.T) {
	a := newTestApp(t)

	assert.Equal(t, http.StatusOK, a.get("/healthz").Status)
	assert.Equal(t, http.StatusOK, a.get("/readyz").Status)

	m := a.get("/metrics")
	assert.Equal(t, http.StatusOK, m.Status)
	assert.Contains(t, m.Body, "http_requests_total")
}

func TestAnonymousIsRedirectedToLogin(t *testing.T) {
	a := newTestApp(t)

	r := a.get("/customers")
	assert.Equal(t, http.StatusSeeOther, r.Status)
	assert.Equal(t, "/login", r.Header.Get("Location"))

	// HTMX istekleri tam sayfa yönlendirmesi yerine HX-Redirect alır.
	r = a.do(http.MethodGet, "/customers", nil, map[string]string{"HX-Request": "true"})
	assert.Equal(t, http.StatusUnauthorized, r.Status)
	assert.Equal(t, "/login", r.Header.Get("HX-Redirect"))
}

func TestLoginLogoutFlow(t *testing.T) {
	a := newTestApp(t)

	r := a.login("yanlış-şifre-123")
	assert.Equal(t, http.StatusUnauthorized, r.Status)
	assert.Contains(t, r.Body, "E-posta veya şifre hatalı.")

	r = a.login(testPassword)
	assert.Equal(t, http.StatusSeeOther, r.Status)
	assert.Equal(t, "/", r.Header.Get("Location"))

	for _, path := range []string{"/", "/customers", "/products", "/invoices", "/payments"} {
		page := a.get(path)
		assert.Equal(t, http.StatusOK, page.Status, path)
		assert.Contains(t, page.Body, testEmail, path)
		assert.Contains(t, page.Body, "Müşteriler", path)
	}

	assert.Equal(t, http.StatusNotFound, a.get("/yok-boyle-bir-sayfa").Status)

	r = a.do(http.MethodPost, "/logout", url.Values{}, nil)
	assert.Equal(t, http.StatusSeeOther, r.Status)

	r = a.get("/")
	assert.Equal(t, http.StatusSeeOther, r.Status, "çıkıştan sonra oturum geçersiz")
}

func TestCrossOriginPostIsRejected(t *testing.T) {
	a := newTestApp(t)

	r := a.do(http.MethodPost, "/login",
		url.Values{"email": {testEmail}, "password": {testPassword}},
		map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://kotu-site.example"})
	assert.Equal(t, http.StatusForbidden, r.Status)
}

func TestSecurityHeaders(t *testing.T) {
	a := newTestApp(t)

	r := a.get("/login")
	assert.Equal(t, "nosniff", r.Header.Get("X-Content-Type-Options"))
	assert.Contains(t, r.Header.Get("Content-Security-Policy"), "default-src 'self'")
	assert.NotEmpty(t, r.Header.Get("X-Request-Id"))
}
