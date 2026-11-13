package httpserver

import (
	"net/http"
	"strconv"
	"strings"

	"hesapkitap/internal/audit"
	"hesapkitap/internal/platform/auth"
	"hesapkitap/internal/platform/http/views"
)

const auditPerPage = 50

// record writes one history entry. A failure is logged but never blocks the user's action.
func (s *server) record(r *http.Request, action, entity, entityID, summary string) {
	if s.Audit == nil {
		return
	}
	u, _ := auth.SessionUser(r.Context(), s.Sessions)
	s.recordAs(r, u.Email, action, entity, entityID, summary)
}

func (s *server) recordAs(r *http.Request, actor, action, entity, entityID, summary string) {
	if s.Audit == nil {
		return
	}
	if err := s.Audit.Add(r.Context(), audit.Entry{Actor: actor, Action: action, Entity: entity, EntityID: entityID, Summary: summary}); err != nil {
		s.Logger.WarnContext(r.Context(), "işlem geçmişi yazılamadı", "err", err, "action", action)
	}
}

func (s *server) auditList(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	items, total, err := s.Audit.List(r.Context(), q, auditPerPage, (page-1)*auditPerPage)
	if err != nil {
		s.serverError(w, r, "işlem geçmişi okunamadı", err)
		return
	}
	render(w, r, http.StatusOK, views.AuditPage(s.pageData(r, "audit", "İşlem geçmişi"),
		views.AuditList{Items: items, Query: q, Page: page, PageCount: max(1, (total+auditPerPage-1)/auditPerPage), Total: total}))
}
