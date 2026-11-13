package httpserver

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"hesapkitap/internal/customer"
	"hesapkitap/internal/money"
	"hesapkitap/internal/platform/auth"
	"hesapkitap/internal/platform/http/views"
	"hesapkitap/internal/trendyol"
)

const customersPerPage = 20

func (s *server) pageData(r *http.Request, active, title string) views.PageData {
	u, _ := auth.CurrentUser(r.Context())
	pd := views.PageData{Title: title, Active: active, Email: u.Email, IsAdmin: u.IsAdmin, Perms: map[string]bool{}}
	for _, p := range auth.AllPermissions {
		pd.Perms[string(p.Key)] = u.Can(p.Key)
	}
	return pd
}

func (s *server) serverError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	s.Logger.ErrorContext(r.Context(), msg, "err", err)
	render(w, r, http.StatusInternalServerError, views.ErrorPage(s.pageData(r, "", "Hata"), "Bir hata oluştu. Lütfen tekrar deneyin."))
}

func (s *server) notFound(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusNotFound, views.NotFound(s.pageData(r, "", "Sayfa bulunamadı")))
}

func (s *server) customerList(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	status := r.URL.Query().Get("status")
	if status != "active" && status != "passive" {
		status = ""
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	items, total, err := s.Customers.List(r.Context(), customer.ListFilter{
		Query: q, Status: status, Limit: customersPerPage, Offset: (page - 1) * customersPerPage,
	})
	if err != nil {
		s.serverError(w, r, "müşteri listesi okunamadı", err)
		return
	}
	pages := max(1, (total+customersPerPage-1)/customersPerPage)
	l := views.CustomerList{Items: items, Query: q, Status: status, Page: page, PageCount: pages, Total: total}
	if r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-Target") == "customer-list" {
		render(w, r, http.StatusOK, views.CustomerListFragment(l))
		return
	}
	render(w, r, http.StatusOK, views.CustomersPage(s.pageData(r, "customers", "Müşteriler"), l))
}

func (s *server) customerNew(w http.ResponseWriter, r *http.Request) {
	f := views.CustomerForm{IsNew: true, Customer: customer.Customer{Kind: customer.KindCompany, Active: true}}
	render(w, r, http.StatusOK, views.CustomerFormPage(s.pageData(r, "customers", "Yeni müşteri"), f))
}

func (s *server) customerID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return uuid.Nil, false
	}
	return id, true
}

func (s *server) customerShow(w http.ResponseWriter, r *http.Request) {
	id, ok := s.customerID(w, r)
	if !ok {
		return
	}
	c, err := s.Customers.Get(r.Context(), id)
	if errors.Is(err, customer.ErrNotFound) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, "müşteri okunamadı", err)
		return
	}
	f := views.CustomerForm{
		Customer: c, OpeningBalance: money.Format(c.OpeningBalance),
		Saved: r.URL.Query().Get("saved") == "1", ProviderReady: s.Customers.Provider != nil,
		Paid: r.URL.Query().Get("paid") == "1",
	}
	if s.Ledger != nil {
		d, err := s.Ledger.Customer(r.Context(), id)
		if err != nil {
			s.serverError(w, r, "cari durum okunamadı", err)
			return
		}
		me, _ := auth.CurrentUser(r.Context())
		f.Ledger = &views.CustomerLedger{CustomerID: id.String(), Position: d.Position(today()), CanPay: me.Can(auth.PermPaymentCreate)}
	}
	render(w, r, http.StatusOK, views.CustomerFormPage(s.pageData(r, "customers", c.Name), f))
}

// customerFromForm reads the form. A bad opening balance is reported as a field error.
func customerFromForm(r *http.Request) (customer.Customer, string, customer.FieldErrors) {
	v := r.PostForm.Get
	c := customer.Customer{
		Kind: customer.Kind(v("kind")), TaxID: v("tax_id"), Name: v("name"), TaxOffice: v("tax_office"),
		Address: v("address"), District: v("district"), City: v("city"), PostalCode: v("postal_code"),
		Email: v("email"), Phone: v("phone"), Note: v("note"), Active: v("active") == "1",
	}
	errs := customer.FieldErrors{}
	bal := v("opening_balance")
	kurus, err := money.Parse(bal)
	if err != nil {
		errs["opening_balance"] = "Tutarı 1.234,56 biçiminde girin."
	}
	c.OpeningBalance = kurus
	return c, bal, errs
}

func (s *server) customerCreate(w http.ResponseWriter, r *http.Request) {
	if !parseForm(w, r) {
		return
	}
	c, bal, errs := customerFromForm(r)
	if len(errs) == 0 {
		created, verrs, err := s.Customers.Create(r.Context(), c)
		if err != nil {
			s.serverError(w, r, "müşteri kaydedilemedi", err)
			return
		}
		if len(verrs) == 0 {
			s.record(r, "customer.create", "customer", created.ID.String(), created.Name)
			http.Redirect(w, r, "/customers/"+created.ID.String()+"?saved=1", http.StatusSeeOther)
			return
		}
		errs = verrs
	} else {
		c.Normalize()
		for k, m := range c.Validate() {
			if _, has := errs[k]; !has {
				errs[k] = m
			}
		}
	}
	f := views.CustomerForm{IsNew: true, Customer: c, Errors: errs, OpeningBalance: bal}
	render(w, r, http.StatusUnprocessableEntity, views.CustomerFormPage(s.pageData(r, "customers", "Yeni müşteri"), f))
}

func (s *server) customerUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := s.customerID(w, r)
	if !ok || !parseForm(w, r) {
		return
	}
	c, bal, errs := customerFromForm(r)
	c.ID = id
	if len(errs) == 0 {
		verrs, err := s.Customers.Update(r.Context(), c)
		if errors.Is(err, customer.ErrNotFound) {
			s.notFound(w, r)
			return
		}
		if err != nil {
			s.serverError(w, r, "müşteri güncellenemedi", err)
			return
		}
		if len(verrs) == 0 {
			s.record(r, "customer.update", "customer", id.String(), c.Name)
			http.Redirect(w, r, "/customers/"+id.String()+"?saved=1", http.StatusSeeOther) //nolint:gosec // id uuid olarak ayrıştırıldı
			return
		}
		errs = verrs
	}
	// Doğrulama hatası: mükellef bilgilerini kaybetmemek için kayıtlı kartı da oku.
	if stored, err := s.Customers.Get(r.Context(), id); err == nil {
		c.IsEInvoiceUser, c.EInvoiceAlias, c.TaxpayerCheckedAt = stored.IsEInvoiceUser, stored.EInvoiceAlias, stored.TaxpayerCheckedAt
	}
	f := views.CustomerForm{Customer: c, Errors: errs, OpeningBalance: bal, ProviderReady: s.Customers.Provider != nil}
	render(w, r, http.StatusUnprocessableEntity, views.CustomerFormPage(s.pageData(r, "customers", c.Name), f))
}

func (s *server) customerTaxpayer(w http.ResponseWriter, r *http.Request) {
	id, ok := s.customerID(w, r)
	if !ok {
		return
	}
	res, err := s.Customers.LookupTaxpayer(r.Context(), id)
	msg := ""
	switch {
	case errors.Is(err, customer.ErrNotFound):
		s.notFound(w, r)
		return
	case errors.Is(err, customer.ErrNoProvider):
		msg = "Trendyol bağlantısı ayarlı değil."
	case err != nil:
		s.Logger.ErrorContext(r.Context(), "mükellef sorgusu başarısız", "err", err)
		msg = trendyol.UserMessage(err)
	}
	c, gerr := s.Customers.Get(r.Context(), id)
	if gerr != nil {
		s.serverError(w, r, "müşteri okunamadı", gerr)
		return
	}
	render(w, r, http.StatusOK, views.TaxpayerPanel(c, &res, msg, true))
}

func (s *server) customerSelectAlias(w http.ResponseWriter, r *http.Request) {
	id, ok := s.customerID(w, r)
	if !ok || !parseForm(w, r) {
		return
	}
	msg := ""
	if err := s.Customers.SelectAlias(r.Context(), id, r.PostForm.Get("alias")); err != nil {
		msg = "Etiket kaydedilemedi."
	}
	c, err := s.Customers.Get(r.Context(), id)
	if err != nil {
		s.serverError(w, r, "müşteri okunamadı", err)
		return
	}
	render(w, r, http.StatusOK, views.TaxpayerPanel(c, nil, msg, true))
}

func parseForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<18)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Form okunamadı", http.StatusBadRequest)
		return false
	}
	return true
}

// customerLookup fills the new-customer form from the provider: only e-Fatura taxpayers are known, and
// only their title is returned (no address, no tax office).
func (s *server) customerLookup(w http.ResponseWriter, r *http.Request) {
	if !parseForm(w, r) {
		return
	}
	taxID := strings.TrimSpace(r.PostForm.Get("tax_id"))
	aliases, err := s.Customers.Preview(r.Context(), taxID)
	switch {
	case errors.Is(err, customer.ErrNoProvider):
		render(w, r, http.StatusOK, views.CustomerLookup("Trendyol bağlantısı ayarlı değil.", "", ""))
		return
	case errors.Is(err, customer.ErrInvalidTaxID):
		render(w, r, http.StatusOK, views.CustomerLookup(err.Error()+".", "", ""))
		return
	case err != nil:
		s.Logger.ErrorContext(r.Context(), "mükellef önizleme başarısız", "err", err)
		render(w, r, http.StatusOK, views.CustomerLookup(trendyol.UserMessage(err), "", ""))
		return
	case len(aliases) == 0:
		render(w, r, http.StatusOK, views.CustomerLookup("Bu numara e-Fatura mükellefi değil; ünvan Trendyol'dan gelmez. Bilgileri elle girin (e-Arşiv kesilir).", "", ""))
		return
	}
	kind := customer.KindCompany
	if len(taxID) == 11 {
		kind = customer.KindPerson
	}
	render(w, r, http.StatusOK, views.CustomerLookup("", aliases[0].Title, string(kind)))
}
