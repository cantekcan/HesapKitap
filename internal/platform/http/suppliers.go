package httpserver

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"hesapkitap/internal/attachment"
	"hesapkitap/internal/cheque"
	"hesapkitap/internal/money"
	"hesapkitap/internal/platform/auth"
	"hesapkitap/internal/platform/http/views"
	"hesapkitap/internal/supplier"
)

func (s *server) supplierList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	items, err := s.Suppliers.List(r.Context(), q)
	if err != nil {
		s.serverError(w, r, "tedarikçiler okunamadı", err)
		return
	}
	debt, err := s.Suppliers.DebtTotal(r.Context())
	if err != nil {
		s.serverError(w, r, "toplam borç okunamadı", err)
		return
	}
	me, _ := auth.CurrentUser(r.Context())
	l := views.SupplierList{Items: items, Query: q, Debt: debt, CanWrite: me.Can(auth.PermSupplierWrite)}
	for _, sp := range items {
		l.Pending += sp.PendingCheques
	}
	render(w, r, http.StatusOK, views.SuppliersPage(s.pageData(r, "suppliers", "Tedarikçiler"), l))
}

func (s *server) supplierNew(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusOK, views.SupplierNewPage(s.pageData(r, "suppliers", "Yeni tedarikçi"), views.SupplierForm{Active: true}))
}

// supplierFromForm reads the supplier card form.
func supplierFromForm(r *http.Request) (supplier.Supplier, views.SupplierForm, supplier.FieldErrors) {
	v := r.PostForm.Get
	f := views.SupplierForm{Name: v("name"), TaxID: v("tax_id"), Phone: v("phone"), Note: v("note"), Opening: v("opening_balance"), Active: v("active") == "1"}
	errs := supplier.FieldErrors{}
	opening, err := money.Parse(f.Opening)
	if err != nil {
		errs["opening_balance"] = "Tutarı 1.234,56 biçiminde girin."
	}
	return supplier.Supplier{Name: f.Name, TaxID: f.TaxID, Phone: f.Phone, Note: f.Note, Opening: opening, Active: f.Active}, f, errs
}

func (s *server) supplierCreate(w http.ResponseWriter, r *http.Request) {
	if !parseForm(w, r) {
		return
	}
	sp, f, errs := supplierFromForm(r)
	sp.Active = true
	if len(errs) == 0 {
		id, verrs, err := s.Suppliers.CreateSupplier(r.Context(), sp)
		if err != nil {
			s.serverError(w, r, "tedarikçi kaydedilemedi", err)
			return
		}
		if len(verrs) == 0 {
			s.record(r, "supplier.create", "supplier", id.String(), sp.Name)
			http.Redirect(w, r, "/suppliers/"+id.String()+"?flash=created", http.StatusSeeOther)
			return
		}
		errs = verrs
	} else {
		for k, m := range sp.Validate() {
			if _, has := errs[k]; !has {
				errs[k] = m
			}
		}
	}
	f.Errors = errs
	render(w, r, http.StatusUnprocessableEntity, views.SupplierNewPage(s.pageData(r, "suppliers", "Yeni tedarikçi"), f))
}

func (s *server) supplierID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return uuid.Nil, false
	}
	return id, true
}

func supplierFlash(code string) string {
	switch code {
	case "created":
		return "Tedarikçi eklendi. Şimdi mal aldıkça “Alım / borç ekle” ile borcunu girebilirsiniz."
	case "saved":
		return "Bilgiler kaydedildi."
	case "entry":
		return "Alım kaydedildi; borcumuz arttı."
	case "payment":
		return "Ödeme kaydedildi; borcumuz düştü."
	case "deleted":
		return "Kayıt silindi."
	case "endorsed":
		return "Çek ciro edildi; tedarikçi borcumuz düştü."
	}
	return ""
}

// renderSupplier builds the supplier page; entry and pay hold what the user typed in the two forms.
func (s *server) renderSupplier(w http.ResponseWriter, r *http.Request, id uuid.UUID, status int, entry views.EntryForm, pay views.PayForm, form *views.SupplierForm, message, flash string) {
	sp, err := s.Suppliers.Get(r.Context(), id)
	if errors.Is(err, supplier.ErrNotFound) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, "tedarikçi okunamadı", err)
		return
	}
	entries, err := s.Suppliers.Entries(r.Context(), id)
	if err != nil {
		s.serverError(w, r, "alımlar okunamadı", err)
		return
	}
	pays, err := s.Suppliers.Payments(r.Context(), id)
	if err != nil {
		s.serverError(w, r, "ödemeler okunamadı", err)
		return
	}
	me, _ := auth.CurrentUser(r.Context())
	v := views.SupplierView{S: sp, Rows: supplier.Statement(sp.Opening, entries, pays), Entry: entry, Pay: pay,
		CanWrite: me.Can(auth.PermSupplierWrite), CanCheque: me.Can(auth.PermChequeManage), Message: message, Flash: flash, Today: today()}
	if form != nil {
		v.Form = *form
	} else {
		v.Form = views.SupplierForm{ID: id.String(), Name: sp.Name, TaxID: sp.TaxID, Phone: sp.Phone, Note: sp.Note, Opening: money.Format(sp.Opening), Active: sp.Active}
	}
	if v.Entry.Date == "" {
		v.Entry.Date = formatDate(today())
	}
	if v.Pay.Date == "" {
		v.Pay.Date = formatDate(today())
	}
	if v.Pay.Method == "" {
		v.Pay.Method = string(supplier.MethodTransfer)
	}
	for _, e := range entries {
		if e.Paid < e.Amount {
			v.OpenItems = append(v.OpenItems, e)
		}
	}
	if v.CanCheque {
		cs, _, err := s.Cheques.List(r.Context(), cheque.ListFilter{Status: cheque.StatusPortfolio, Limit: 200})
		if err != nil {
			s.serverError(w, r, "çekler okunamadı", err)
			return
		}
		v.Portfolio = cs
	}
	ids := make([]uuid.UUID, 0, len(entries)+len(pays))
	for _, e := range entries {
		ids = append(ids, e.ID)
	}
	counts, err := s.Attachments.Counts(r.Context(), attachment.OwnerSupplierEntry, ids)
	if err != nil {
		s.serverError(w, r, "ekler okunamadı", err)
		return
	}
	pids := make([]uuid.UUID, len(pays))
	for i, p := range pays {
		pids[i] = p.ID
	}
	pc, err := s.Attachments.Counts(r.Context(), attachment.OwnerSupplierPayment, pids)
	if err != nil {
		s.serverError(w, r, "ekler okunamadı", err)
		return
	}
	for k, n := range pc {
		counts[k] = n
	}
	v.Attach = counts
	render(w, r, status, views.SupplierPage(s.pageData(r, "suppliers", sp.Name), v))
}

func (s *server) supplierShow(w http.ResponseWriter, r *http.Request) {
	id, ok := s.supplierID(w, r)
	if !ok {
		return
	}
	s.renderSupplier(w, r, id, http.StatusOK, views.EntryForm{}, views.PayForm{}, nil, "", supplierFlash(r.URL.Query().Get("flash")))
}

func (s *server) supplierUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := s.supplierID(w, r)
	if !ok {
		return
	}
	if !parseForm(w, r) {
		return
	}
	sp, f, errs := supplierFromForm(r)
	sp.ID, f.ID = id, id.String()
	if len(errs) == 0 {
		verrs, err := s.Suppliers.UpdateSupplier(r.Context(), sp)
		switch {
		case errors.Is(err, supplier.ErrNotFound):
			s.notFound(w, r)
			return
		case err != nil:
			s.serverError(w, r, "tedarikçi güncellenemedi", err)
			return
		case len(verrs) == 0:
			s.record(r, "supplier.update", "supplier", id.String(), sp.Name)
			http.Redirect(w, r, "/suppliers/"+id.String()+"?flash=saved", http.StatusSeeOther)
			return
		}
		errs = verrs
	}
	f.Errors = errs
	s.renderSupplier(w, r, id, http.StatusUnprocessableEntity, views.EntryForm{}, views.PayForm{}, &f, "Bilgiler kaydedilemedi; “Bilgileri düzenle” bölümündeki hataları düzeltin.", "")
}

func (s *server) supplierEntryAdd(w http.ResponseWriter, r *http.Request) {
	id, ok := s.supplierID(w, r)
	if !ok {
		return
	}
	if !parseForm(w, r) {
		return
	}
	v := r.PostForm.Get
	f := views.EntryForm{Date: v("entry_date"), Amount: v("amount"), DocNo: v("doc_no"), Description: v("description"), Due: v("due_date")}
	e := supplier.Entry{SupplierID: id, Date: parseDate(f.Date), DocNo: f.DocNo, Description: f.Description}
	if d := parseDate(f.Due); !d.IsZero() {
		e.Due = &d
	}
	errs := supplier.FieldErrors{}
	amount, err := money.Parse(f.Amount)
	if err != nil {
		errs["amount"] = "Tutarı 1.234,56 biçiminde girin."
	}
	e.Amount = amount
	if len(errs) == 0 {
		_, verrs, serr := s.Suppliers.AddEntry(r.Context(), e)
		if serr != nil {
			s.serverError(w, r, "alım kaydedilemedi", serr)
			return
		}
		if len(verrs) == 0 {
			s.record(r, "supplier.entry", "supplier", id.String(), money.FormatTL(e.Amount)+" · "+e.DocNo)
			http.Redirect(w, r, "/suppliers/"+id.String()+"?flash=entry", http.StatusSeeOther)
			return
		}
		errs = verrs
	} else {
		for k, m := range e.Validate(today()) {
			if _, has := errs[k]; !has && k != "amount" {
				errs[k] = m
			}
		}
	}
	f.Errors = errs
	s.renderSupplier(w, r, id, http.StatusUnprocessableEntity, f, views.PayForm{}, nil, "", "")
}

func (s *server) supplierPaymentAdd(w http.ResponseWriter, r *http.Request) {
	id, ok := s.supplierID(w, r)
	if !ok {
		return
	}
	if !parseForm(w, r) {
		return
	}
	v := r.PostForm.Get
	f := views.PayForm{Date: v("paid_at"), Amount: v("amount"), Method: v("method"), ChequeDue: v("cheque_due"), ChequeNo: v("cheque_no"),
		Bank: v("bank"), EntryID: v("entry_id"), Note: v("note")}
	p := supplier.Payment{SupplierID: id, Date: parseDate(f.Date), Method: supplier.Method(f.Method), ChequeNo: f.ChequeNo, Bank: f.Bank, Note: f.Note}
	if d := parseDate(f.ChequeDue); !d.IsZero() {
		p.ChequeDue = &d
	}
	if e, err := uuid.Parse(f.EntryID); err == nil {
		p.EntryID = &e
	}
	errs := supplier.FieldErrors{}
	amount, err := money.Parse(f.Amount)
	if err != nil {
		errs["amount"] = "Tutarı 1.234,56 biçiminde girin."
	}
	p.Amount = amount
	if len(errs) == 0 {
		_, verrs, serr := s.Suppliers.AddPayment(r.Context(), p)
		if serr != nil {
			s.serverError(w, r, "ödeme kaydedilemedi", serr)
			return
		}
		if len(verrs) == 0 {
			s.record(r, "supplier.payment", "supplier", id.String(), money.FormatTL(p.Amount)+" · "+p.Method.Label())
			http.Redirect(w, r, "/suppliers/"+id.String()+"?flash=payment", http.StatusSeeOther)
			return
		}
		errs = verrs
	} else {
		for k, m := range p.Validate(today()) {
			if _, has := errs[k]; !has && k != "amount" {
				errs[k] = m
			}
		}
	}
	f.Errors = errs
	s.renderSupplier(w, r, id, http.StatusUnprocessableEntity, views.EntryForm{}, f, nil, "", "")
}

// supplierEndorse is the endorsement form of the supplier page.
func (s *server) supplierEndorse(w http.ResponseWriter, r *http.Request) {
	id, ok := s.supplierID(w, r)
	if !ok {
		return
	}
	if !parseForm(w, r) {
		return
	}
	cid, err := uuid.Parse(r.PostForm.Get("cheque_id"))
	if err != nil {
		s.renderSupplier(w, r, id, http.StatusUnprocessableEntity, views.EntryForm{}, views.PayForm{}, nil, "Ciro edilecek çeki seçin.", "")
		return
	}
	s.endorse(w, r, id, cid, time.Time{}, r.PostForm.Get("note"), "/suppliers/"+id.String(), func(msg string) {
		s.renderSupplier(w, r, id, http.StatusUnprocessableEntity, views.EntryForm{}, views.PayForm{}, nil, msg, "")
	})
}

// backToSupplier redirects to the supplier named in the form (only used to come back to the same page).
func backToSupplier(w http.ResponseWriter, r *http.Request, flash string) {
	if sid, err := uuid.Parse(r.PostForm.Get("supplier_id")); err == nil {
		http.Redirect(w, r, "/suppliers/"+sid.String()+"?flash="+flash, http.StatusSeeOther) //nolint:gosec // sid uuid, flash sabit
		return
	}
	http.Redirect(w, r, "/suppliers", http.StatusSeeOther)
}

func (s *server) supplierEntryDelete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	if !parseForm(w, r) {
		return
	}
	if err := s.Suppliers.DeleteEntry(r.Context(), id); err != nil && !errors.Is(err, supplier.ErrNotFound) {
		s.serverError(w, r, "alım silinemedi", err)
		return
	}
	s.record(r, "supplier.delete", "supplier_entry", id.String(), "Alım silindi")
	backToSupplier(w, r, "deleted")
}

func (s *server) supplierPaymentDelete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	if !parseForm(w, r) {
		return
	}
	if err := s.Suppliers.DeletePayment(r.Context(), id); err != nil && !errors.Is(err, supplier.ErrNotFound) {
		s.serverError(w, r, "ödeme silinemedi", err)
		return
	}
	s.record(r, "supplier.delete", "supplier_payment", id.String(), "Ödeme silindi")
	backToSupplier(w, r, "deleted")
}

// supplierCleared marks one of our own cheques as paid out of our account (or undoes it).
func (s *server) supplierCleared(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	if !parseForm(w, r) {
		return
	}
	cleared := r.PostForm.Get("cleared") == "1"
	if err := s.Suppliers.SetCleared(r.Context(), id, cleared); err != nil {
		if errors.Is(err, supplier.ErrNotFound) {
			s.notFound(w, r)
			return
		}
		s.serverError(w, r, "çek durumu değiştirilemedi", err)
		return
	}
	s.record(r, "supplier.cleared", "supplier_payment", id.String(), map[bool]string{true: "Hesaptan çıktı", false: "Geri alındı"}[cleared])
	http.Redirect(w, r, "/cheques/own", http.StatusSeeOther)
}
