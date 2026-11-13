package httpserver

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hesapkitap/internal/platform/auth"
	"hesapkitap/internal/platform/http/views"
)

func (s *server) userList(w http.ResponseWriter, r *http.Request) {
	users, err := s.Auth.ListUsers(r.Context())
	if err != nil {
		s.serverError(w, r, "kullanıcılar okunamadı", err)
		return
	}
	me, _ := auth.CurrentUser(r.Context())
	flash := ""
	if r.URL.Query().Get("saved") == "1" {
		flash = "Kaydedildi."
	}
	render(w, r, http.StatusOK, views.UsersPage(s.pageData(r, "users", "Kullanıcılar"), views.UserList{Users: users, Self: me.Email, Flash: flash}))
}

func defaultPerms() map[auth.Permission]bool {
	m := map[auth.Permission]bool{}
	for _, p := range auth.AllPermissions {
		if p.Default {
			m[p.Key] = true
		}
	}
	return m
}

func (s *server) userNew(w http.ResponseWriter, r *http.Request) {
	f := views.UserForm{Active: true, Perms: defaultPerms()}
	render(w, r, http.StatusOK, views.UserFormPage(s.pageData(r, "users", "Yeni kullanıcı"), f))
}

func formPerms(r *http.Request) []auth.Permission {
	var out []auth.Permission
	for _, k := range r.PostForm["perm"] {
		if auth.ValidPermission(k) {
			out = append(out, auth.Permission(k))
		}
	}
	return out
}

func permSet(perms []auth.Permission) map[auth.Permission]bool {
	m := map[auth.Permission]bool{}
	for _, p := range perms {
		m[p] = true
	}
	return m
}

func (s *server) userCreate(w http.ResponseWriter, r *http.Request) {
	if !parseForm(w, r) {
		return
	}
	email, perms := r.PostForm.Get("email"), formPerms(r)
	admin := r.PostForm.Get("is_admin") == "1"
	f := views.UserForm{Email: email, IsAdmin: admin, Active: true, Perms: permSet(perms)}
	u, err := s.Auth.CreateUserWithAccess(r.Context(), email, r.PostForm.Get("password"), admin, perms)
	if err != nil {
		f.Error = err.Error()
		render(w, r, http.StatusUnprocessableEntity, views.UserFormPage(s.pageData(r, "users", "Yeni kullanıcı"), f))
		return
	}
	s.record(r, "user.create", "user", u.ID.String(), u.Email+roleText(admin))
	http.Redirect(w, r, "/users?saved=1", http.StatusSeeOther)
}

func roleText(admin bool) string {
	if admin {
		return " · yönetici"
	}
	return " · kullanıcı"
}

func (s *server) userForm(r *http.Request, id uuid.UUID) (views.UserForm, error) {
	users, err := s.Auth.ListUsers(r.Context())
	if err != nil {
		return views.UserForm{}, err
	}
	me, _ := auth.CurrentUser(r.Context())
	for _, u := range users {
		if u.ID == id {
			return views.UserForm{ID: id.String(), Email: u.Email, IsAdmin: u.IsAdmin, Active: u.Active, Perms: u.Perms, IsSelf: u.ID == me.ID}, nil
		}
	}
	return views.UserForm{}, pgx.ErrNoRows
}

func (s *server) userShow(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	f, err := s.userForm(r, id)
	if errors.Is(err, pgx.ErrNoRows) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, "kullanıcı okunamadı", err)
		return
	}
	render(w, r, http.StatusOK, views.UserFormPage(s.pageData(r, "users", f.Email), f))
}

func (s *server) userUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	if !parseForm(w, r) {
		return
	}
	f, err := s.userForm(r, id)
	if errors.Is(err, pgx.ErrNoRows) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, "kullanıcı okunamadı", err)
		return
	}
	admin, active, perms := r.PostForm.Get("is_admin") == "1", r.PostForm.Get("active") == "1", formPerms(r)
	if f.IsSelf { // kendini kilitleyemez
		admin, active = true, true
	}
	f.IsAdmin, f.Active, f.Perms = admin, active, permSet(perms)
	fail := func(msg string) {
		f.Error = msg
		render(w, r, http.StatusUnprocessableEntity, views.UserFormPage(s.pageData(r, "users", f.Email), f))
	}
	if pw := r.PostForm.Get("password"); pw != "" {
		if err := s.Auth.SetPassword(r.Context(), id, pw); err != nil {
			fail(err.Error())
			return
		}
		s.record(r, "user.password", "user", id.String(), f.Email)
	}
	switch err := s.Auth.UpdateAccess(r.Context(), id, admin, active, perms); {
	case errors.Is(err, auth.ErrLastAdmin):
		fail("En az bir etkin yönetici kalmalı.")
		return
	case err != nil:
		s.serverError(w, r, "kullanıcı güncellenemedi", err)
		return
	}
	s.record(r, "user.update", "user", id.String(), f.Email+roleText(admin)+activeText(active))
	http.Redirect(w, r, "/users?saved=1", http.StatusSeeOther)
}

func activeText(active bool) string {
	if active {
		return ""
	}
	return " · pasif"
}
