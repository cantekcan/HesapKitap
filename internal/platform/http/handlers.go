package httpserver

import (
	"errors"
	"net/http"

	"github.com/a-h/templ"

	"hesapkitap/internal/platform/auth"
	"hesapkitap/internal/platform/http/views"
)

type templComponent = templ.Component

func render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = c.Render(r.Context(), w)
}

// requireAuth redirects anonymous users to the login page. HTMX requests get HX-Redirect instead,
// otherwise the login page would be swapped into a fragment.
func (s *server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := auth.SessionUser(r.Context(), s.Sessions); !ok {
			if r.Header.Get("HX-Request") == "true" {
				w.Header().Set("HX-Redirect", "/login")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// page renders an authenticated page built from PageData.
func (s *server) page(active, title string, build func(views.PageData) templComponent, status ...int) http.Handler {
	code := http.StatusOK
	if len(status) > 0 {
		code = status[0]
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _ := auth.SessionUser(r.Context(), s.Sessions)
		render(w, r, code, build(views.PageData{Title: title, Active: active, Email: u.Email}))
	})
}

func (s *server) loginForm(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.SessionUser(r.Context(), s.Sessions); ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	render(w, r, http.StatusOK, views.Login("", ""))
}

func (s *server) loginSubmit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	if err := r.ParseForm(); err != nil {
		render(w, r, http.StatusBadRequest, views.Login("Form okunamadı.", ""))
		return
	}
	email := r.PostForm.Get("email")

	u, err := s.Auth.Authenticate(r.Context(), email, r.PostForm.Get("password"))
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		render(w, r, http.StatusUnauthorized, views.Login("E-posta veya şifre hatalı.", email))
		return
	case errors.Is(err, auth.ErrLocked):
		render(w, r, http.StatusTooManyRequests, views.Login("Çok fazla hatalı deneme yapıldı. Hesap 15 dakika kilitlendi.", email))
		return
	case err != nil:
		s.Logger.ErrorContext(r.Context(), "giriş sırasında hata", "err", err)
		render(w, r, http.StatusInternalServerError, views.Login("Giriş şu anda yapılamıyor. Lütfen tekrar deneyin.", email))
		return
	}

	if err := auth.Login(r.Context(), s.Sessions, u); err != nil {
		s.Logger.ErrorContext(r.Context(), "oturum açılamadı", "err", err)
		render(w, r, http.StatusInternalServerError, views.Login("Oturum açılamadı. Lütfen tekrar deneyin.", email))
		return
	}
	s.Logger.InfoContext(r.Context(), "kullanıcı giriş yaptı", "user_id", u.ID.String())
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if err := auth.Logout(r.Context(), s.Sessions); err != nil {
		s.Logger.ErrorContext(r.Context(), "oturum kapatılamadı", "err", err)
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
