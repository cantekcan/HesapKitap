package httpserver

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"hesapkitap/internal/customer"
	"hesapkitap/internal/invoice"
	"hesapkitap/internal/money"
	"hesapkitap/internal/platform/http/views"
	"hesapkitap/internal/trendyol"
)

const dateLayout = "2006-01-02"

func (s *server) invoiceList(w http.ResponseWriter, r *http.Request) {
	q, status, page := listParams(r)
	if status != string(invoice.StatusDraft) {
		status = ""
	}
	items, total, err := s.Invoices.List(r.Context(), invoice.ListFilter{Query: q, Status: status, Limit: listPerPage, Offset: (page - 1) * listPerPage})
	if err != nil {
		s.serverError(w, r, "fatura listesi okunamadı", err)
		return
	}
	l := views.InvoiceList{Items: items, Query: q, Status: status, Page: page, PageCount: pageCount(total), Total: total}
	if isFragment(r, "invoice-list") {
		render(w, r, http.StatusOK, views.InvoiceListFragment(l))
		return
	}
	render(w, r, http.StatusOK, views.InvoicesPage(s.pageData(r, "invoices", "Faturalar"), l))
}

// formCatalogues loads what the draft form needs for its dropdowns.
func (s *server) formCatalogues(r *http.Request, f *views.InvoiceForm) error {
	custs, _, err := s.Customers.List(r.Context(), customer.ListFilter{Status: "active", Limit: 1000})
	if err != nil {
		return err
	}
	prods, err := s.Products.ListActive(r.Context())
	if err != nil {
		return err
	}
	f.Customers, f.Products = custs, prods
	return nil
}

func (s *server) invoiceNew(w http.ResponseWriter, r *http.Request) {
	f := views.InvoiceForm{
		IssueDate: time.Now().Format(dateLayout),
		Rows:      []views.LineRow{{Key: 1, Unit: "C62", Qty: "1", Tax: "20"}},
	}
	if err := s.formCatalogues(r, &f); err != nil {
		s.serverError(w, r, "form verileri okunamadı", err)
		return
	}
	render(w, r, http.StatusOK, views.InvoiceFormPage(s.pageData(r, "invoices", "Yeni fatura"), f))
}

func (s *server) invoiceShow(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	d, err := s.Invoices.Get(r.Context(), id)
	if errors.Is(err, invoice.ErrNotFound) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, "fatura okunamadı", err)
		return
	}
	if d.Status != invoice.StatusDraft {
		s.invoiceView(w, r, d, "", flashFor(r), false)
		return
	}
	s.renderDraft(w, r, d, d.LastError)
}

func flashFor(r *http.Request) string {
	if r.URL.Query().Get("saved") == "1" {
		return "Kaydedildi."
	}
	return ""
}

// invoiceShowWithIssueError shows the draft again with the reason the invoice could not be issued.
func (s *server) invoiceShowWithIssueError(w http.ResponseWriter, r *http.Request, id uuid.UUID, msg string) {
	d, err := s.Invoices.Get(r.Context(), id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	s.renderDraft(w, r, d, msg)
}

func (s *server) renderDraft(w http.ResponseWriter, r *http.Request, d invoice.Draft, issueErr string) {
	f := views.InvoiceForm{
		ID: d.ID.String(), CustomerID: d.CustomerID.String(), IssueDate: d.IssueDate.Format(dateLayout),
		Notes: d.Notes, Saved: r.URL.Query().Get("saved") == "1",
	}
	if issueErr != "" {
		f.Errors = invoice.FieldErrors{"issue": issueErr}
	}
	if d.DueDate != nil {
		f.DueDate = d.DueDate.Format(dateLayout)
	}
	for i, l := range d.Lines {
		row := views.LineRow{
			Key: i + 1, Name: l.ItemName, Unit: l.Unit, Qty: l.Quantity.String(),
			Price: money.Format(l.UnitPrice), Tax: strconv.Itoa(l.TaxPercent),
		}
		if l.Discount > 0 {
			row.Discount = money.Format(l.Discount)
		}
		if l.ProductID != nil {
			row.Product = l.ProductID.String()
		}
		f.Rows = append(f.Rows, row)
	}
	if t, err := d.Totals(); err == nil {
		f.Totals = &t
	}
	if err := s.formCatalogues(r, &f); err != nil {
		s.serverError(w, r, "form verileri okunamadı", err)
		return
	}
	render(w, r, http.StatusOK, views.InvoiceFormPage(s.pageData(r, "invoices", "Fatura taslağı"), f))
}

// draftFromForm reads the draft form. Unparsable numbers are reported per line; the rows are returned
// exactly as typed so the form can be shown again.
func draftFromForm(r *http.Request) (invoice.Draft, []views.LineRow, invoice.FieldErrors) {
	v := r.PostForm
	errs := invoice.FieldErrors{}
	d := invoice.Draft{Notes: strings.TrimSpace(v.Get("notes"))}

	if id, err := uuid.Parse(v.Get("customer_id")); err == nil {
		d.CustomerID = id
	}
	if t, err := time.Parse(dateLayout, v.Get("issue_date")); err == nil {
		d.IssueDate = t
	}
	if s := v.Get("due_date"); s != "" {
		if t, err := time.Parse(dateLayout, s); err == nil {
			d.DueDate = &t
		} else {
			errs["due_date"] = "Vade tarihi geçersiz."
		}
	}

	names, prods, units := v["line_name"], v["line_product"], v["line_unit"]
	qtys, prices, discs, taxes := v["line_qty"], v["line_price"], v["line_discount"], v["line_tax"]
	at := func(s []string, i int) string {
		if i < len(s) {
			return strings.TrimSpace(s[i])
		}
		return ""
	}
	var rows []views.LineRow
	for i := range names {
		row := views.LineRow{
			Key: i + 1, Product: at(prods, i), Name: at(names, i), Unit: at(units, i),
			Qty: at(qtys, i), Price: at(prices, i), Discount: at(discs, i), Tax: at(taxes, i),
		}
		if row.Name == "" && row.Price == "" && row.Product == "" { // boş satır
			continue
		}
		rows = append(rows, row)
		n := len(rows) - 1
		line, lerr := parseLine(row)
		if lerr != "" {
			errs["line_"+strconv.Itoa(n)] = strconv.Itoa(n+1) + ". kalem: " + lerr
			continue
		}
		d.Lines = append(d.Lines, line)
	}
	return d, rows, errs
}

func parseLine(row views.LineRow) (invoice.Line, string) {
	l := invoice.Line{ItemName: row.Name, Unit: row.Unit}
	if id, err := uuid.Parse(row.Product); err == nil {
		l.ProductID = &id
	}
	q, err := decimal.NewFromString(strings.Replace(row.Qty, ",", ".", 1))
	if err != nil {
		return l, "miktar sayı olmalı."
	}
	l.Quantity = q
	if l.UnitPrice, err = money.Parse(row.Price); err != nil {
		return l, "birim fiyatı 1.234,56 biçiminde girin."
	}
	if l.Discount, err = money.Parse(row.Discount); err != nil {
		return l, "indirimi 1.234,56 biçiminde girin."
	}
	if l.TaxPercent, err = strconv.Atoi(row.Tax); err != nil {
		return l, "KDV oranı geçersiz."
	}
	return l, ""
}

// invoicePreview returns the live totals box. The amounts come from the same InvoiceCalculator that Save uses.
func (s *server) invoicePreview(w http.ResponseWriter, r *http.Request) {
	if !parseForm(w, r) {
		return
	}
	d, _, errs := draftFromForm(r)
	if len(errs) > 0 || len(d.Lines) == 0 {
		msg := "Kalem ekleyin."
		for _, m := range errs {
			msg = m
			break
		}
		render(w, r, http.StatusOK, views.InvoiceTotals(nil, msg))
		return
	}
	t, err := d.Totals()
	if err != nil {
		render(w, r, http.StatusOK, views.InvoiceTotals(nil, err.Error()+"."))
		return
	}
	render(w, r, http.StatusOK, views.InvoiceTotals(&t, ""))
}

func (s *server) invoiceSave(w http.ResponseWriter, r *http.Request) {
	var id uuid.UUID
	if raw := r.PathValue("id"); raw != "" {
		var err error
		if id, err = uuid.Parse(raw); err != nil {
			s.notFound(w, r)
			return
		}
	}
	if !parseForm(w, r) {
		return
	}
	d, rows, errs := draftFromForm(r)
	d.ID = id
	if len(errs) == 0 {
		savedID, verrs, err := s.Invoices.Save(r.Context(), d)
		switch {
		case errors.Is(err, invoice.ErrNotEditable):
			errs = invoice.FieldErrors{"lines": invoice.ErrNotEditable.Error() + "."}
		case err != nil:
			s.serverError(w, r, "fatura kaydedilemedi", err)
			return
		case len(verrs) > 0:
			errs = verrs
		default:
			http.Redirect(w, r, "/invoices/"+savedID.String()+"?saved=1", http.StatusSeeOther) //nolint:gosec // id uuid
			return
		}
	} else {
		// Sayısal ayrıştırma hatası olan satırlar d.Lines'a girmedi; kalan kurallar yine de gösterilsin.
		for k, m := range d.Validate() {
			if _, has := errs[k]; !has && k != "lines" {
				errs[k] = m
			}
		}
	}

	f := views.InvoiceForm{
		CustomerID: d.CustomerID.String(), Notes: d.Notes, Rows: rows, Errors: errs,
	}
	if id != uuid.Nil {
		f.ID = id.String()
	}
	if d.CustomerID == uuid.Nil {
		f.CustomerID = ""
	}
	if !d.IssueDate.IsZero() {
		f.IssueDate = d.IssueDate.Format(dateLayout)
	}
	if d.DueDate != nil {
		f.DueDate = d.DueDate.Format(dateLayout)
	}
	if len(f.Rows) == 0 {
		f.Rows = []views.LineRow{{Key: 1, Unit: "C62", Qty: "1", Tax: "20"}}
	}
	if err := s.formCatalogues(r, &f); err != nil {
		s.serverError(w, r, "form verileri okunamadı", err)
		return
	}
	render(w, r, http.StatusUnprocessableEntity, views.InvoiceFormPage(s.pageData(r, "invoices", "Fatura taslağı"), f))
}

func (s *server) invoiceDelete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	switch err := s.Invoices.Delete(r.Context(), id); {
	case errors.Is(err, invoice.ErrNotEditable):
		s.notFound(w, r)
	case err != nil:
		s.serverError(w, r, "taslak silinemedi", err)
	default:
		http.Redirect(w, r, "/invoices", http.StatusSeeOther)
	}
}

// issueMessage turns an issue-flow error into a Turkish sentence for the screen.
func issueMessage(err error) string {
	var apiErr *trendyol.APIError
	switch {
	case errors.Is(err, invoice.ErrNoProvider), errors.Is(err, invoice.ErrAliasNotChosen), errors.Is(err, invoice.ErrNotEArchive),
		errors.Is(err, invoice.ErrCannotCancel), errors.Is(err, invoice.ErrNotIssued), errors.Is(err, invoice.ErrNotEditable):
		return err.Error() + "."
	case errors.Is(err, trendyol.ErrIPNotWhitelisted), errors.Is(err, trendyol.ErrInvalidCredentials),
		errors.Is(err, trendyol.ErrUnauthorized), errors.As(err, &apiErr):
		return trendyol.UserMessage(err)
	}
	return "İşlem şu anda yapılamadı. Lütfen tekrar deneyin."
}

// invoiceView renders the read-only page (or just the status box for HTMX polling).
func (s *server) invoiceView(w http.ResponseWriter, r *http.Request, d invoice.Draft, message, flash string, boxOnly bool) {
	t, err := d.Totals()
	if err != nil {
		s.serverError(w, r, "fatura toplamı hesaplanamadı", err)
		return
	}
	v := views.InvoiceView{Draft: d, Totals: t, Message: message, Flash: flash}
	if boxOnly {
		render(w, r, http.StatusOK, views.InvoiceStatusBox(v))
		return
	}
	render(w, r, http.StatusOK, views.InvoiceViewPage(s.pageData(r, "invoices", "Fatura"), v))
}

func (s *server) invoiceIssue(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := s.Invoices.Issue(r.Context(), id); err != nil {
		if !errors.Is(err, invoice.ErrNotEditable) && !errors.Is(err, invoice.ErrNoProvider) && !errors.Is(err, invoice.ErrAliasNotChosen) {
			s.Logger.WarnContext(r.Context(), "fatura kesilemedi", "err", err, "invoice_id", id.String())
		}
		msg := issueMessage(err)
		if errors.Is(err, invoice.ErrNotEditable) {
			http.Redirect(w, r, "/invoices/"+id.String(), http.StatusSeeOther) //nolint:gosec // id uuid
			return
		}
		if msg == "İşlem şu anda yapılamadı. Lütfen tekrar deneyin." {
			msg = err.Error() // doğrulama metinleri (kalem hatası vb.) zaten Türkçe
		}
		s.invoiceShowWithIssueError(w, r, id, msg)
		return
	}
	http.Redirect(w, r, "/invoices/"+id.String(), http.StatusSeeOther) //nolint:gosec // id uuid
}

func (s *server) invoiceStatus(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := s.Invoices.RefreshOne(r.Context(), id); err != nil {
		s.Logger.WarnContext(r.Context(), "fatura durumu yenilenemedi", "err", err, "invoice_id", id.String())
	}
	d, err := s.Invoices.Get(r.Context(), id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	s.invoiceView(w, r, d, "", "", true)
}

func (s *server) invoicePDF(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	link, err := s.Invoices.DocumentURL(r.Context(), id, false)
	if err != nil {
		s.Logger.WarnContext(r.Context(), "pdf bağlantısı alınamadı", "err", err)
		http.Error(w, issueMessage(err), http.StatusBadGateway)
		return
	}
	http.Redirect(w, r, link, http.StatusSeeOther) //nolint:gosec // bağlantı Trendyol'dan geldi (kullanıcı girdisi değil)
}

func (s *server) invoiceShare(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	link, err := s.Invoices.DocumentURL(r.Context(), id, true)
	if err != nil {
		s.Logger.WarnContext(r.Context(), "paylaşım bağlantısı alınamadı", "err", err)
		render(w, r, http.StatusOK, views.ShareLink("", issueMessage(err)))
		return
	}
	render(w, r, http.StatusOK, views.ShareLink(link, ""))
}

func (s *server) invoiceCancel(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := s.Invoices.Cancel(r.Context(), id); err != nil {
		s.Logger.WarnContext(r.Context(), "fatura iptal edilemedi", "err", err, "invoice_id", id.String())
		d, gerr := s.Invoices.Get(r.Context(), id)
		if gerr != nil {
			s.notFound(w, r)
			return
		}
		s.invoiceView(w, r, d, issueMessage(err), "", false)
		return
	}
	http.Redirect(w, r, "/invoices/"+id.String(), http.StatusSeeOther) //nolint:gosec // id uuid
}
