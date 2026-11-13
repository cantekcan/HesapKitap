package httpserver

import (
	"context"
	"net/http"

	"hesapkitap/internal/cheque"
	"hesapkitap/internal/platform/http/views"
)

// cashflowMonths is how many future months the cash-flow table shows.
const cashflowMonths = 6

func (s *server) reportCashflow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	in, err := s.Cheques.PortfolioDues(ctx)
	if err != nil {
		s.serverError(w, r, "portföy çekleri okunamadı", err)
		return
	}
	pending, err := s.Suppliers.PendingOwnDues(ctx)
	if err != nil {
		s.serverError(w, r, "verilen çekler okunamadı", err)
		return
	}
	out := make([]cheque.Due, len(pending))
	var ownPending int64
	for i, d := range pending {
		out[i] = cheque.Due{Date: d.Date, Amount: d.Amount}
		ownPending += d.Amount
	}
	all, err := s.Ledger.All(ctx)
	if err != nil {
		s.serverError(w, r, "alacaklar okunamadı", err)
		return
	}
	var receivable int64
	for _, c := range all {
		receivable += max(c.Balance(), 0)
	}
	debt, err := s.Suppliers.DebtTotal(ctx)
	if err != nil {
		s.serverError(w, r, "tedarikçi borcu okunamadı", err)
		return
	}
	sum, err := s.Cheques.DueSummary(ctx)
	if err != nil {
		s.serverError(w, r, "çek özeti okunamadı", err)
		return
	}
	v := views.CashflowView{Months: cheque.BuildFlow(today(), in, out, cashflowMonths), Receivable: receivable,
		Portfolio: sum.All.Amount, SupplierDue: debt, OwnPending: ownPending}
	render(w, r, http.StatusOK, views.CashflowPage(s.pageData(r, "reports", "Çek takvimi ve nakit akışı"), v))
}

func (s *server) reportHabits(w http.ResponseWriter, r *http.Request) {
	hs, err := s.Cheques.Habits(r.Context())
	if err != nil {
		s.serverError(w, r, "ödeme alışkanlıkları okunamadı", err)
		return
	}
	render(w, r, http.StatusOK, views.HabitsPage(s.pageData(r, "reports", "Müşteri ödeme alışkanlığı"), hs))
}

// chequePanel feeds the dashboard block; a failure hides the block instead of breaking the page.
func (s *server) chequePanel(ctx context.Context) views.ChequePanel {
	sum, err := s.Cheques.DueSummary(ctx)
	if err != nil {
		s.Logger.WarnContext(ctx, "çek özeti okunamadı", "error", err)
		return views.ChequePanel{}
	}
	debt, err := s.Suppliers.DebtTotal(ctx)
	if err != nil {
		s.Logger.WarnContext(ctx, "tedarikçi borcu okunamadı", "error", err)
		return views.ChequePanel{}
	}
	return views.ChequePanel{Summary: sum, SupplierDue: debt, Show: true}
}
