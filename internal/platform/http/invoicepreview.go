package httpserver

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"hesapkitap/internal/invoice"
	"hesapkitap/internal/platform/pdf"
)

// invoicePreviewPDF renders the saved draft as a PDF marked "TASLAK". Issued invoices have the official PDF instead.
func (s *server) invoicePreviewPDF(w http.ResponseWriter, r *http.Request) {
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
		http.Redirect(w, r, "/invoices/"+id.String(), http.StatusSeeOther) //nolint:gosec // id uuid olarak ayrıştırıldı
		return
	}
	totals, err := d.Totals()
	if err != nil || len(d.Lines) == 0 {
		http.Error(w, "Taslakta eksik veya geçersiz kalem var; önce düzeltip kaydedin.", http.StatusUnprocessableEntity)
		return
	}
	c, err := s.Customers.Get(r.Context(), d.CustomerID)
	if err != nil {
		s.serverError(w, r, "müşteri okunamadı", err)
		return
	}
	out, err := pdf.DraftInvoice(d, c, totals, time.Now().In(istanbul))
	if err != nil {
		s.serverError(w, r, "taslak PDF üretilemedi", err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `inline; filename="fatura-taslagi-`+d.IssueDate.Format("2006-01-02")+`.pdf"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(out)
}
