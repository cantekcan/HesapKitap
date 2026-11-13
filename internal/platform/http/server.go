// Package httpserver wires the HTTP handler: routing, middlewares, sessions and the templ UI.
package httpserver

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"hesapkitap/internal/customer"
	"hesapkitap/internal/invoice"
	"hesapkitap/internal/ledger"
	"hesapkitap/internal/payment"
	"hesapkitap/internal/platform/auth"
	"hesapkitap/internal/platform/http/views"
	"hesapkitap/internal/product"
	"hesapkitap/internal/report"
)

//go:embed static
var staticFiles embed.FS

// Deps are the collaborators the HTTP layer needs.
type Deps struct {
	Logger   *slog.Logger
	Pool     *pgxpool.Pool
	Sessions *scs.SessionManager
	Auth     *auth.Service

	Customers *customer.Service
	Products  *product.Service
	Invoices  *invoice.Service
	Payments  *payment.Service
	Ledger    ledger.Source
	Reports   report.Source
}

type server struct {
	Deps
}

// New builds the root handler.
//
// CSRF: tarayıcıdan gelen durum değiştiren (POST vb.) isteklerde Origin/Sec-Fetch-Site başlıkları
// http.CrossOriginProtection ile doğrulanır; farklı kaynaktan gelen istekler reddedilir.
func New(d Deps) (http.Handler, error) {
	s := &server{Deps: d}

	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	m := newMetrics(reg)

	static, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return nil, err
	}
	if css, err := fs.ReadFile(static, "app.css"); err == nil {
		sum := sha256.Sum256(css)
		views.AssetVersion = hex.EncodeToString(sum[:4])
	}

	// Oturum gerektirmeyen, DB'ye dokunmayan uç noktalar.
	root := http.NewServeMux()
	route(root, "GET /healthz", http.HandlerFunc(s.healthz))
	route(root, "GET /readyz", http.HandlerFunc(s.readyz))
	route(root, "GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	route(root, "GET /static/", http.StripPrefix("/static/", cacheControl(http.FileServerFS(static))))

	// Uygulama sayfaları: oturum + CSRF koruması.
	app := http.NewServeMux()
	route(app, "GET /login", http.HandlerFunc(s.loginForm))
	route(app, "POST /login", http.HandlerFunc(s.loginSubmit))
	route(app, "POST /logout", s.requireAuth(http.HandlerFunc(s.logout)))
	authed := func(h http.HandlerFunc) http.Handler { return s.requireAuth(h) }
	route(app, "GET /{$}", authed(s.dashboard))
	route(app, "GET /reports", authed(s.reportsHub))
	route(app, "GET /reports/sales", authed(s.reportSales))
	route(app, "GET /reports/sales.xlsx", authed(s.salesXLSX))
	route(app, "GET /reports/aging", authed(s.reportAging))
	route(app, "GET /reports/aging.xlsx", authed(s.agingXLSX))
	route(app, "GET /payments.xlsx", authed(s.paymentsXLSX))
	route(app, "GET /customers.xlsx", authed(s.customersXLSX))
	route(app, "GET /customers/{id}/statement.xlsx", authed(s.statementXLSX))
	route(app, "GET /customers", authed(s.customerList))
	route(app, "GET /customers/new", authed(s.customerNew))
	route(app, "GET /customers/import", authed(s.customerImportForm))
	route(app, "GET /customers/import-template.xlsx", authed(s.customerImportTemplate))
	route(app, "POST /customers/import", authed(s.customerImport))
	route(app, "POST /customers/lookup", authed(s.customerLookup))
	route(app, "POST /customers", authed(s.customerCreate))
	route(app, "GET /customers/{id}", authed(s.customerShow))
	route(app, "POST /customers/{id}", authed(s.customerUpdate))
	route(app, "POST /customers/{id}/taxpayer", authed(s.customerTaxpayer))
	route(app, "POST /customers/{id}/taxpayer/select", authed(s.customerSelectAlias))
	route(app, "GET /products", authed(s.productList))
	route(app, "GET /products/new", authed(s.productNew))
	route(app, "POST /products", authed(s.productCreate))
	route(app, "GET /products/{id}", authed(s.productShow))
	route(app, "POST /products/{id}", authed(s.productUpdate))
	route(app, "GET /invoices", authed(s.invoiceList))
	route(app, "GET /invoices/new", authed(s.invoiceNew))
	route(app, "POST /invoices/preview", authed(s.invoicePreview))
	route(app, "POST /invoices", authed(s.invoiceSave))
	route(app, "GET /invoices/{id}", authed(s.invoiceShow))
	route(app, "POST /invoices/{id}", authed(s.invoiceSave))
	route(app, "POST /invoices/{id}/delete", authed(s.invoiceDelete))
	route(app, "POST /invoices/{id}/issue", authed(s.invoiceIssue))
	route(app, "GET /invoices/{id}/status", authed(s.invoiceStatus))
	route(app, "GET /invoices/{id}/pdf", authed(s.invoicePDF))
	route(app, "POST /invoices/{id}/share", authed(s.invoiceShare))
	route(app, "POST /invoices/{id}/cancel", authed(s.invoiceCancel))
	route(app, "GET /payments", authed(s.paymentList))
	route(app, "GET /payments/new", authed(s.paymentNew))
	route(app, "GET /payments/open-items", authed(s.paymentOpenItems))
	route(app, "POST /payments", authed(s.paymentCreate))
	route(app, "POST /payments/{id}/delete", authed(s.paymentDelete))
	route(app, "GET /customers/{id}/statement", authed(s.customerStatement))
	route(app, "/", s.requireAuth(s.page("", "Sayfa bulunamadı", func(p views.PageData) templComponent { return views.NotFound(p) }, http.StatusNotFound)))

	cop := http.NewCrossOriginProtection()
	root.Handle("/", s.Sessions.LoadAndSave(cop.Handler(app)))

	// Dıştan içe: panic yakalama → istek kimliği → güvenlik başlıkları → log/metrik.
	var h http.Handler = root
	h = observe(s.Logger, m)(h)
	h = securityHeaders(h)
	h = requestID(h)
	h = recoverer(s.Logger)(h)
	return h, nil
}

func cacheControl(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		next.ServeHTTP(w, r)
	})
}

func (s *server) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}

func (s *server) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.Pool.Ping(ctx); err != nil {
		s.Logger.ErrorContext(r.Context(), "readyz: veritabanı yanıt vermiyor", "err", err)
		http.Error(w, "veritabanı hazır değil", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}
