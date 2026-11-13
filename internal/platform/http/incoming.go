package httpserver

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"hesapkitap/internal/incoming"
	"hesapkitap/internal/invoice"
	"hesapkitap/internal/platform/auth"
	"hesapkitap/internal/platform/http/views"
	"hesapkitap/internal/trendyol"
)

const (
	incomingPageSize = 20
	incomingTimeout  = 20 * time.Second
)

// incomingMessage turns an error into a Turkish message for the screen.
func incomingMessage(err error) string {
	var apiErr *trendyol.APIError
	switch {
	case errors.Is(err, incoming.ErrNoProvider), errors.Is(err, incoming.ErrBadAnswer), errors.Is(err, incoming.ErrNoteNeeded):
		return err.Error() + "."
	case errors.Is(err, trendyol.ErrIPNotWhitelisted), errors.Is(err, trendyol.ErrInvalidCredentials),
		errors.Is(err, trendyol.ErrUnauthorized), errors.As(err, &apiErr), errors.Is(err, context.DeadlineExceeded):
		return trendyol.UserMessage(err)
	}
	return "Gelen faturalar şu anda alınamadı. Lütfen tekrar deneyin."
}

func (s *server) incomingConnected() bool { return s.Incoming != nil && s.Incoming.Provider != nil }

// incomingFilter maps the tab key to a provider filter.
func incomingFilter(key string) (f incoming.Filter, awaiting bool, norm string) {
	switch key {
	case "unread":
		return incoming.Filter{Unread: true}, false, key
	case "awaiting":
		return incoming.Filter{}, true, key
	case "commercial":
		return incoming.Filter{Scenario: incoming.ScenarioCommercial}, false, key
	case "basic":
		return incoming.Filter{Scenario: incoming.ScenarioBasic}, false, key
	}
	return incoming.Filter{}, false, "awaiting"
}

func (s *server) incoming(w http.ResponseWriter, r *http.Request) {
	s.renderIncoming(w, r, http.StatusOK, "")
}

func (s *server) renderIncoming(w http.ResponseWriter, r *http.Request, status int, message string) {
	q := r.URL.Query()
	key := q.Get("f")
	if key == "" {
		key = "awaiting"
	}
	f, awaiting, key := incomingFilter(key)
	me, _ := auth.CurrentUser(r.Context())
	v := views.IncomingView{
		Filter: key, Query: strings.TrimSpace(q.Get("q")), Page: 1, Message: message,
		CanReply: me.Can(auth.PermIncomingReply), Connected: s.incomingConnected(),
	}
	switch q.Get("flash") {
	case "approved":
		v.Flash = "Fatura kabul edildi."
	case "denied":
		v.Flash = "Fatura reddedildi."
	}
	if p, err := strconv.Atoi(q.Get("page")); err == nil && p > 1 {
		v.Page = p
	}
	if v.Connected {
		ctx, cancel := context.WithTimeout(r.Context(), incomingTimeout)
		defer cancel()
		f.Sender, f.Page, f.Size = v.Query, v.Page-1, incomingPageSize
		pg, err := s.Incoming.List(ctx, f, awaiting)
		if err != nil {
			s.Logger.WarnContext(r.Context(), "gelen faturalar alınamadı", "err", err)
			if v.Message == "" {
				v.Message, status = incomingMessage(err), http.StatusBadGateway
			}
		} else {
			v.Items, v.Total, v.PageCount = pg.Items, pg.Total, max(pg.Pages, 1)
		}
	}
	render(w, r, status, views.IncomingPage(s.pageData(r, "incoming", "Gelen faturalar"), v))
}

// incomingTiles is the HTMX fragment with the counts (dashboard block, or the row of the incoming page).
func (s *server) incomingTiles(w http.ResponseWriter, r *http.Request) {
	wide := r.URL.Query().Get("wide") != ""
	if !s.incomingConnected() {
		render(w, r, http.StatusOK, views.IncomingTiles(incoming.Counts{}, "Trendyol bağlantısı ayarlı değil", wide))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), incomingTimeout)
	defer cancel()
	c, err := s.Incoming.Counts(ctx)
	if err != nil {
		s.Logger.WarnContext(r.Context(), "gelen fatura sayıları alınamadı", "err", err)
		render(w, r, http.StatusOK, views.IncomingTiles(incoming.Counts{}, "Sayılar şu anda alınamadı", wide))
		return
	}
	render(w, r, http.StatusOK, views.IncomingTiles(c, "", wide))
}

// incomingPDF redirects to the temporary link of the incoming invoice's PDF.
func (s *server) incomingPDF(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), incomingTimeout)
	defer cancel()
	if s.Incoming == nil {
		s.notFound(w, r)
		return
	}
	link, err := s.Incoming.DocumentURL(ctx, id)
	if err != nil || (!strings.HasPrefix(link, "https://") && !strings.HasPrefix(link, "http://")) {
		if err != nil {
			s.Logger.WarnContext(r.Context(), "gelen fatura PDF'i alınamadı", "err", err, "invoice_id", id.String())
		}
		s.renderIncoming(w, r, http.StatusBadGateway, "PDF şu anda açılamadı. Lütfen tekrar deneyin.")
		return
	}
	http.Redirect(w, r, link, http.StatusSeeOther)
}

// incomingReply answers a commercial invoice (kabul / ret).
func (s *server) incomingReply(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "geçersiz form", http.StatusBadRequest)
		return
	}
	answer := invoice.ReplyType(r.PostFormValue("answer"))
	ctx, cancel := context.WithTimeout(r.Context(), incomingTimeout)
	defer cancel()
	if s.Incoming == nil {
		s.notFound(w, r)
		return
	}
	if err := s.Incoming.Reply(ctx, id, answer, r.PostFormValue("note")); err != nil {
		s.Logger.WarnContext(r.Context(), "gelen faturaya yanıt verilemedi", "err", err, "invoice_id", id.String())
		s.renderIncoming(w, r, http.StatusUnprocessableEntity, incomingMessage(err))
		return
	}
	flash := "approved"
	if answer == invoice.ReplyDenied {
		flash = "denied"
	}
	s.record(r, "incoming.reply", "incoming", id.String(), "Gelen ticari fatura: "+answer.Label())
	http.Redirect(w, r, "/incoming?f=awaiting&flash="+flash, http.StatusSeeOther)
}
