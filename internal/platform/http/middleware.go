package httpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type ctxKey int

const (
	routeKey ctxKey = iota
	requestIDKey
)

// routeInfo is shared through the request context so the metrics middleware can learn the matched
// route pattern even when inner middlewares replace the *http.Request (r.WithContext).
type routeInfo struct{ pattern string }

// statusWriter records the status code and size of a response.
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func newRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// requestID assigns an id to every request and exposes it as X-Request-Id.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

// recoverer turns a handler panic into a 500 and logs it, so one bad request cannot take the server down.
func recoverer(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer recoverPanic(r.Context(), log, w)
			next.ServeHTTP(w, r)
		})
	}
}

// recoverPanic must be deferred directly: recover only works when called by the deferred function itself.
func recoverPanic(ctx context.Context, log *slog.Logger, w http.ResponseWriter) {
	v := recover()
	if v == nil {
		return
	}
	if v == http.ErrAbortHandler { //nolint:errorlint // sentinel panic value, not an error chain
		panic(v) //nolint:forbidigo // net/http'nin bağlantıyı bilerek kesme sözleşmesi; yeniden fırlatılmalı
	}
	log.ErrorContext(ctx, "istek işlenirken beklenmeyen hata", "panic", v, "stack", string(debug.Stack()))
	http.Error(w, "Beklenmeyen bir hata oluştu.", http.StatusInternalServerError)
}

// securityHeaders sets conservative browser security headers.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		// Alpine.js standart derlemesi ifadeleri değerlendirmek için 'unsafe-eval' ister;
		// HTMX gösterge stilleri için 'unsafe-inline' style gerekir. Dış kaynak yok.
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; frame-ancestors 'none'; form-action 'self'; base-uri 'self'")
		next.ServeHTTP(w, r)
	})
}

// metrics holds the Prometheus HTTP instruments.
type metrics struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

func newMetrics(reg prometheus.Registerer) *metrics {
	m := &metrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total", Help: "İşlenen HTTP istekleri.",
		}, []string{"method", "route", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "http_request_duration_seconds", Help: "HTTP istek süresi.", Buckets: prometheus.DefBuckets,
		}, []string{"method", "route"}),
	}
	reg.MustRegister(m.requests, m.duration)
	return m
}

// observe logs and measures every request. The query string is never logged: it may carry secrets.
func observe(log *slog.Logger, m *metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ri := &routeInfo{}
			sw := &statusWriter{ResponseWriter: w}
			next.ServeHTTP(sw, r.WithContext(context.WithValue(r.Context(), routeKey, ri)))

			if sw.status == 0 {
				sw.status = http.StatusOK
			}
			route := ri.pattern
			if route == "" {
				route = "unmatched"
			}
			elapsed := time.Since(start)
			m.requests.WithLabelValues(r.Method, route, strconv.Itoa(sw.status)).Inc()
			m.duration.WithLabelValues(r.Method, route).Observe(elapsed.Seconds())

			level := slog.LevelInfo
			if route == "GET /healthz" || route == "GET /readyz" || route == "GET /metrics" {
				level = slog.LevelDebug
			}
			log.Log(r.Context(), level, "http isteği",
				"request_id", requestIDFrom(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
				"route", route,
				"status", sw.status,
				"bytes", sw.bytes,
				"duration_ms", elapsed.Milliseconds(),
			)
		})
	}
}

func requestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// route registers a handler and records its pattern for metrics and logs.
func route(mux *http.ServeMux, pattern string, h http.Handler) {
	mux.Handle(pattern, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ri, ok := r.Context().Value(routeKey).(*routeInfo); ok {
			ri.pattern = pattern
		}
		h.ServeHTTP(w, r)
	}))
}
