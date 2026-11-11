// Package httpserver wires the HTTP handler: routing, middlewares, sessions and the templ UI.
package httpserver

import (
	"context"
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"hesapkitap/internal/platform/auth"
	"hesapkitap/internal/platform/http/views"
)

//go:embed static
var staticFiles embed.FS

// Deps are the collaborators the HTTP layer needs.
type Deps struct {
	Logger   *slog.Logger
	Pool     *pgxpool.Pool
	Sessions *scs.SessionManager
	Auth     *auth.Service
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
	route(app, "GET /{$}", s.requireAuth(s.page("dashboard", "Pano", func(p views.PageData) templComponent { return views.Dashboard(p) })))
	for _, pg := range []struct{ path, key, title, phase string }{
		{"/customers", "customers", "Müşteriler", "Faz 3"},
		{"/products", "products", "Ürünler", "Faz 4"},
		{"/invoices", "invoices", "Faturalar", "Faz 4–5"},
		{"/payments", "payments", "Tahsilatlar", "Faz 6"},
	} {
		phase := pg.phase
		route(app, "GET "+pg.path, s.requireAuth(s.page(pg.key, pg.title, func(p views.PageData) templComponent {
			return views.Placeholder(p, phase)
		})))
	}
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
