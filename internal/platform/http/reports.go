package httpserver

import (
	"net/http"
	"time"

	"hesapkitap/internal/platform/auth"
	"hesapkitap/internal/platform/http/views"
	"hesapkitap/internal/report"
)

func (s *server) dashboard(w http.ResponseWriter, r *http.Request) {
	if u, _ := auth.CurrentUser(r.Context()); !u.Can(auth.PermReportView) {
		http.Redirect(w, r, "/invoices", http.StatusSeeOther)
		return
	}
	all, err := s.Ledger.All(r.Context())
	if err != nil {
		s.serverError(w, r, "pano verisi okunamadı", err)
		return
	}
	attention, err := s.Reports.Attention(r.Context())
	if err != nil {
		s.serverError(w, r, "dikkat listesi okunamadı", err)
		return
	}
	counts, err := s.Reports.InvoiceCounts(r.Context(), time.Date(today().Year(), today().Month(), 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		s.serverError(w, r, "fatura sayıları okunamadı", err)
		return
	}
	d := report.BuildDashboard(all, today(), attention)
	hasData := false
	for _, c := range all {
		if len(c.Invoices) > 0 || len(c.Payments) > 0 {
			hasData = true
			break
		}
	}
	render(w, r, http.StatusOK, views.Dashboard(s.pageData(r, "dashboard", "Pano"), d, counts, s.Incoming != nil && s.Incoming.Provider != nil, s.chequePanel(r.Context()), hasData))
}

func (s *server) reportsHub(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusOK, views.ReportsHub(s.pageData(r, "reports", "Raporlar")))
}

// salesRange reads from/to; the default is the current month.
func salesRange(r *http.Request) (time.Time, time.Time) {
	now := today()
	from, to := parseDate(r.URL.Query().Get("from")), parseDate(r.URL.Query().Get("to"))
	if from.IsZero() || to.IsZero() || to.Before(from) {
		from = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		to = from.AddDate(0, 1, -1)
	}
	return from, to
}

func presets(from, to time.Time) []views.Preset {
	now := today()
	m := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	y := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	mk := func(label string, f, t time.Time) views.Preset {
		return views.Preset{Label: label, From: formatDate(f), To: formatDate(t), Active: f.Equal(from) && t.Equal(to)}
	}
	return []views.Preset{
		mk("Bu ay", m, m.AddDate(0, 1, -1)),
		mk("Geçen ay", m.AddDate(0, -1, 0), m.AddDate(0, 0, -1)),
		mk("Son 3 ay", m.AddDate(0, -2, 0), m.AddDate(0, 1, -1)),
		mk("Bu yıl", y, y.AddDate(1, 0, -1)),
		mk("Geçen yıl", y.AddDate(-1, 0, 0), y.AddDate(0, 0, -1)),
	}
}

func (s *server) salesReport(w http.ResponseWriter, r *http.Request) (report.SalesReport, time.Time, time.Time, bool) {
	from, to := salesRange(r)
	drafts, err := s.Reports.Sales(r.Context(), from, to)
	if err != nil {
		s.serverError(w, r, "satış raporu okunamadı", err)
		return report.SalesReport{}, from, to, false
	}
	rep, err := report.BuildSales(from, to, drafts)
	if err != nil {
		s.serverError(w, r, "satış raporu hesaplanamadı", err)
		return report.SalesReport{}, from, to, false
	}
	return rep, from, to, true
}

func (s *server) reportSales(w http.ResponseWriter, r *http.Request) {
	rep, from, to, ok := s.salesReport(w, r)
	if !ok {
		return
	}
	page := views.SalesPage{From: formatDate(from), To: formatDate(to), Presets: presets(from, to), Report: rep}
	render(w, r, http.StatusOK, views.SalesReportPage(s.pageData(r, "reports", "Dönemsel satış raporu"), page))
}

func (s *server) reportAging(w http.ResponseWriter, r *http.Request) {
	all, err := s.Ledger.All(r.Context())
	if err != nil {
		s.serverError(w, r, "yaşlandırma raporu okunamadı", err)
		return
	}
	render(w, r, http.StatusOK, views.AgingReportPage(s.pageData(r, "reports", "Alacak yaşlandırma"), report.BuildAging(all, today())))
}
