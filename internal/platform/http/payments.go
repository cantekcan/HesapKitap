package httpserver

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"hesapkitap/internal/customer"
	"hesapkitap/internal/ledger"
	"hesapkitap/internal/money"
	"hesapkitap/internal/payment"
	"hesapkitap/internal/platform/http/views"
)

const paymentsPerPage = 25

var istanbul = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		return time.FixedZone("TRT", 3*3600)
	}
	return loc
}()

// today is the current calendar date in Turkey, as a UTC midnight.
func today() time.Time {
	n := time.Now().In(istanbul)
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

func parseDate(s string) time.Time {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func formatDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(dateLayout)
}

func (s *server) paymentList(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	from, to := parseDate(r.URL.Query().Get("from")), parseDate(r.URL.Query().Get("to"))
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	items, total, err := s.Payments.List(r.Context(), payment.ListFilter{Query: q, From: from, To: to, Limit: paymentsPerPage, Offset: (page - 1) * paymentsPerPage})
	if err != nil {
		s.serverError(w, r, "tahsilatlar okunamadı", err)
		return
	}
	sum, err := s.Payments.Total(r.Context(), from, to)
	if err != nil {
		s.serverError(w, r, "tahsilat toplamı okunamadı", err)
		return
	}
	l := views.PaymentList{Items: items, Query: q, From: formatDate(from), To: formatDate(to), Page: page,
		PageCount: max(1, (total+paymentsPerPage-1)/paymentsPerPage), Total: total, SumAll: sum}
	render(w, r, http.StatusOK, views.PaymentsPage(s.pageData(r, "payments", "Tahsilatlar"), l))
}

func (s *server) paymentFormData(r *http.Request, f views.PaymentForm) (views.PaymentForm, error) {
	cs, _, err := s.Customers.List(r.Context(), customer.ListFilter{Status: "active", Limit: 1000})
	if err != nil {
		return f, err
	}
	f.Customers = cs
	if id, err := uuid.Parse(f.CustomerID); err == nil {
		d, err := s.Ledger.Customer(r.Context(), id)
		if err != nil {
			return f, err
		}
		f.Open = d.Position(today()).OpenItems
	}
	return f, nil
}

func (s *server) paymentNew(w http.ResponseWriter, r *http.Request) {
	f := views.PaymentForm{CustomerID: r.URL.Query().Get("customer"), Date: formatDate(today()), Method: string(payment.MethodTransfer),
		InvoiceID: r.URL.Query().Get("invoice")}
	f, err := s.paymentFormData(r, f)
	if err != nil {
		s.serverError(w, r, "tahsilat formu hazırlanamadı", err)
		return
	}
	render(w, r, http.StatusOK, views.PaymentFormPage(s.pageData(r, "payments", "Tahsilat gir"), f))
}

// paymentOpenItems is the HTMX answer when the customer select changes.
func (s *server) paymentOpenItems(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.URL.Query().Get("customer_id"))
	if err != nil {
		render(w, r, http.StatusOK, views.OpenItemsSelect(nil, ""))
		return
	}
	d, err := s.Ledger.Customer(r.Context(), id)
	if err != nil {
		s.serverError(w, r, "açık kalemler okunamadı", err)
		return
	}
	render(w, r, http.StatusOK, views.OpenItemsSelect(d.Position(today()).OpenItems, ""))
}

func (s *server) paymentCreate(w http.ResponseWriter, r *http.Request) {
	if !parseForm(w, r) {
		return
	}
	v := r.PostForm.Get
	f := views.PaymentForm{CustomerID: v("customer_id"), Date: v("paid_at"), Amount: v("amount"), Method: v("method"), Note: v("note"), InvoiceID: v("invoice_id")}
	p := payment.Payment{Date: parseDate(f.Date), Method: payment.Method(f.Method), Note: f.Note}
	if id, err := uuid.Parse(f.CustomerID); err == nil {
		p.CustomerID = id
	}
	if id, err := uuid.Parse(f.InvoiceID); err == nil {
		p.InvoiceID = &id
	}
	errs := payment.FieldErrors{}
	amount, err := money.Parse(f.Amount)
	if err != nil {
		errs["amount"] = "Tutarı 1.234,56 biçiminde girin."
	}
	p.Amount = amount
	if len(errs) == 0 {
		var verrs payment.FieldErrors
		_, verrs, err = s.Payments.Create(r.Context(), p)
		if err != nil {
			s.serverError(w, r, "tahsilat kaydedilemedi", err)
			return
		}
		if len(verrs) == 0 {
			http.Redirect(w, r, "/customers/"+p.CustomerID.String()+"?paid=1", http.StatusSeeOther)
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
	f, err = s.paymentFormData(r, f)
	if err != nil {
		s.serverError(w, r, "tahsilat formu hazırlanamadı", err)
		return
	}
	render(w, r, http.StatusUnprocessableEntity, views.PaymentFormPage(s.pageData(r, "payments", "Tahsilat gir"), f))
}

func (s *server) paymentDelete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := s.Payments.Delete(r.Context(), id); err != nil && !errors.Is(err, payment.ErrNotFound) {
		s.serverError(w, r, "tahsilat silinemedi", err)
		return
	}
	http.Redirect(w, r, "/payments", http.StatusSeeOther)
}

func (s *server) statementData(r *http.Request, id uuid.UUID) (views.StatementPage, error) {
	d, err := s.Ledger.Customer(r.Context(), id)
	if err != nil {
		return views.StatementPage{}, err
	}
	from, to := parseDate(r.URL.Query().Get("from")), parseDate(r.URL.Query().Get("to"))
	rows := ledger.Statement(d, from, to)
	sp := views.StatementPage{CustomerID: id.String(), CustomerName: d.Name, From: formatDate(from), To: formatDate(to), Rows: rows, Position: d.Position(today())}
	for _, row := range rows {
		if row.Kind != ledger.RowCarried {
			sp.TotalDebit += row.Debit
			sp.TotalCredit += row.Credit
		}
	}
	return sp, nil
}

func (s *server) customerStatement(w http.ResponseWriter, r *http.Request) {
	id, ok := s.customerID(w, r)
	if !ok {
		return
	}
	sp, err := s.statementData(r, id)
	if errors.Is(err, customer.ErrNotFound) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, "ekstre hazırlanamadı", err)
		return
	}
	render(w, r, http.StatusOK, views.StatementPageView(s.pageData(r, "customers", sp.CustomerName+" — Ekstre"), sp))
}
