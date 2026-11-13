package httpserver

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"hesapkitap/internal/attachment"
	"hesapkitap/internal/cheque"
	"hesapkitap/internal/money"
	"hesapkitap/internal/platform/auth"
	"hesapkitap/internal/platform/http/views"
	"hesapkitap/internal/supplier"
)

const chequesPerPage = 25

// chequeFilter maps a tab key to a list filter; unknown keys fall back to the portfolio tab.
func chequeFilter(key string, today time.Time) (cheque.ListFilter, string) {
	switch key {
	case "overdue":
		return cheque.ListFilter{Status: cheque.StatusPortfolio, To: today.AddDate(0, 0, -1)}, key
	case "collected":
		return cheque.ListFilter{Status: cheque.StatusCollected}, key
	case "bounced":
		return cheque.ListFilter{Status: cheque.StatusBounced}, key
	case "endorsed":
		return cheque.ListFilter{Status: cheque.StatusEndorsed}, key
	case "all":
		return cheque.ListFilter{}, key
	}
	return cheque.ListFilter{Status: cheque.StatusPortfolio}, "portfolio"
}

func (s *server) chequeTabs(r *http.Request, active string) ([]views.ChequeTab, error) {
	totals, err := s.Cheques.Totals(r.Context())
	if err != nil {
		return nil, err
	}
	sum, err := s.Cheques.DueSummary(r.Context())
	if err != nil {
		return nil, err
	}
	var allN int
	var allAmt int64
	for _, t := range totals {
		allN += t.Count
		allAmt += t.Amount
	}
	tab := func(key, label string, n int, amt int64) views.ChequeTab {
		return views.ChequeTab{Key: key, Label: label, N: n, Amount: amt, Active: key == active}
	}
	pf := totals[cheque.StatusPortfolio]
	return []views.ChequeTab{
		tab("portfolio", "Portföyde", pf.Count, pf.Amount),
		tab("overdue", "Vadesi geçmiş", sum.Overdue.Count, sum.Overdue.Amount),
		tab("collected", "Tahsil edildi", totals[cheque.StatusCollected].Count, totals[cheque.StatusCollected].Amount),
		tab("bounced", "Karşılıksız", totals[cheque.StatusBounced].Count, totals[cheque.StatusBounced].Amount),
		tab("endorsed", "Ciro edildi", totals[cheque.StatusEndorsed].Count, totals[cheque.StatusEndorsed].Amount),
		tab("all", "Tümü", allN, allAmt),
	}, nil
}

func (s *server) chequeList(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	f, tab := chequeFilter(r.URL.Query().Get("tab"), today())
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	f.Query, f.Limit, f.Offset = q, chequesPerPage, (page-1)*chequesPerPage
	items, total, err := s.Cheques.List(r.Context(), f)
	if err != nil {
		s.serverError(w, r, "çekler okunamadı", err)
		return
	}
	tabs, err := s.chequeTabs(r, tab)
	if err != nil {
		s.serverError(w, r, "çek sayıları okunamadı", err)
		return
	}
	ids := make([]uuid.UUID, len(items))
	for i, c := range items {
		ids[i] = c.ID
	}
	counts, err := s.Attachments.Counts(r.Context(), attachment.OwnerCheque, ids)
	if err != nil {
		s.serverError(w, r, "ekler okunamadı", err)
		return
	}
	l := views.ChequeList{Items: items, Tabs: tabs, Tab: tab, Query: q, Page: page, PageCount: max(1, (total+chequesPerPage-1)/chequesPerPage),
		Total: total, Today: today(), Attach: counts}
	render(w, r, http.StatusOK, views.ChequesPage(s.pageData(r, "cheques", "Çekler"), l))
}

func (s *server) chequeView(w http.ResponseWriter, r *http.Request, id uuid.UUID, status int, message, flash string) {
	c, err := s.Cheques.Get(r.Context(), id)
	if errors.Is(err, cheque.ErrNotFound) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, "çek okunamadı", err)
		return
	}
	me, _ := auth.CurrentUser(r.Context())
	v := views.ChequeView{C: c, Today: today(), CanManage: me.Can(auth.PermChequeManage), Message: message, Flash: flash, Date: formatDate(today())}
	if v.CanManage && c.Status == cheque.StatusPortfolio {
		ss, err := s.Suppliers.List(r.Context(), "")
		if err != nil {
			s.serverError(w, r, "tedarikçiler okunamadı", err)
			return
		}
		for _, sp := range ss {
			if sp.Active {
				v.Suppliers = append(v.Suppliers, sp)
			}
		}
	}
	items, err := s.Attachments.List(r.Context(), attachment.OwnerCheque, id)
	if err != nil {
		s.serverError(w, r, "ekler okunamadı", err)
		return
	}
	v.Attach = views.AttachmentPanel{OwnerType: string(attachment.OwnerCheque), OwnerID: id.String(), Items: items,
		CanWrite: canWriteAttachment(me, attachment.OwnerCheque)}
	render(w, r, status, views.ChequePage(s.pageData(r, "cheques", "Çek"), v))
}

func (s *server) chequeShow(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	flash := ""
	if r.URL.Query().Get("flash") != "" {
		flash = r.URL.Query().Get("flash")
	}
	s.chequeView(w, r, id, http.StatusOK, "", flashText(flash))
}

func flashText(code string) string {
	switch code {
	case "collected":
		return "Çek tahsil edildi olarak işaretlendi."
	case "bounced":
		return "Çek karşılıksız olarak işaretlendi; müşterinin borcu yeniden açıldı."
	case "portfolio":
		return "Çek portföye geri alındı."
	case "endorsed":
		return "Çek tedarikçiye ciro edildi; tedarikçi borcumuz düştü."
	}
	return ""
}

func (s *server) chequeSetStatus(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	if !parseForm(w, r) {
		return
	}
	to := cheque.Status(r.PostForm.Get("status"))
	if !to.Valid() {
		http.Error(w, "geçersiz durum", http.StatusBadRequest)
		return
	}
	if err := s.Cheques.SetStatus(r.Context(), id, to); err != nil {
		switch {
		case errors.Is(err, cheque.ErrNotFound):
			s.notFound(w, r)
		case errors.Is(err, cheque.ErrBadTransition):
			s.chequeView(w, r, id, http.StatusConflict, "Çekin durumu başka biri tarafından değiştirilmiş olabilir; sayfa yenilendi, tekrar deneyin.", "")
		default:
			s.serverError(w, r, "çek durumu değiştirilemedi", err)
		}
		return
	}
	s.record(r, "cheque.status", "cheque", id.String(), to.Label())
	http.Redirect(w, r, "/cheques/"+id.String()+"?flash="+strings.ToLower(string(to)), http.StatusSeeOther) //nolint:gosec // id uuid
}

func (s *server) chequeEndorse(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	if !parseForm(w, r) {
		return
	}
	sid, err := uuid.Parse(r.PostForm.Get("supplier_id"))
	if err != nil {
		s.chequeView(w, r, id, http.StatusUnprocessableEntity, "Tedarikçi seçin.", "")
		return
	}
	s.endorse(w, r, sid, id, parseDate(r.PostForm.Get("date")), "", "/cheques/"+id.String(), func(msg string) {
		s.chequeView(w, r, id, http.StatusUnprocessableEntity, msg, "")
	})
}

// endorse gives the cheque to the supplier and redirects to next; fail shows an error on the calling page.
func (s *server) endorse(w http.ResponseWriter, r *http.Request, supplierID, chequeID uuid.UUID, date time.Time, note, next string, fail func(string)) {
	var entryID *uuid.UUID
	if e, err := uuid.Parse(r.PostForm.Get("entry_id")); err == nil {
		entryID = &e
	}
	if _, err := s.Suppliers.Endorse(r.Context(), supplierID, chequeID, date, entryID, note); err != nil {
		switch {
		case errors.Is(err, supplier.ErrChequeUnavailable), errors.Is(err, supplier.ErrNotFound):
			fail("Çek artık portföyde değil ya da tedarikçi bulunamadı.")
		default:
			s.serverError(w, r, "çek ciro edilemedi", err)
		}
		return
	}
	c, _ := s.Cheques.Get(r.Context(), chequeID)
	s.record(r, "cheque.endorse", "cheque", chequeID.String(), c.CustomerName+" · "+money.FormatTL(c.Amount))
	http.Redirect(w, r, next+"?flash=endorsed", http.StatusSeeOther)
}

func (s *server) ownChequeList(w http.ResponseWriter, r *http.Request) {
	items, err := s.Suppliers.OwnCheques(r.Context())
	if err != nil {
		s.serverError(w, r, "verilen çekler okunamadı", err)
		return
	}
	me, _ := auth.CurrentUser(r.Context())
	l := views.OwnChequeList{Items: items, Today: today(), CanWrite: me.Can(auth.PermSupplierWrite)}
	ids := make([]uuid.UUID, len(items))
	for i, c := range items {
		ids[i] = c.PaymentID
		if !c.Cleared {
			l.Pending += c.Amount
			l.PendingN++
		}
	}
	if l.Attach, err = s.Attachments.Counts(r.Context(), attachment.OwnerSupplierPayment, ids); err != nil {
		s.serverError(w, r, "ekler okunamadı", err)
		return
	}
	render(w, r, http.StatusOK, views.OwnChequesPage(s.pageData(r, "cheques", "Verdiğimiz çekler"), l))
}
