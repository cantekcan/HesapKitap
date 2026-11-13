package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"hesapkitap/internal/invoice"
	"hesapkitap/internal/platform/db/dbgen"
	"hesapkitap/internal/report"
)

// ReportStore implements report.Source.
type ReportStore struct{ q *dbgen.Queries }

func NewReportStore(q *dbgen.Queries) *ReportStore { return &ReportStore{q: q} }

var _ report.Source = (*ReportStore)(nil)

func (s *ReportStore) Sales(ctx context.Context, from, to time.Time) ([]invoice.Draft, error) {
	heads, err := s.q.ReportInvoices(ctx, dbgen.ReportInvoicesParams{FromDate: toDate(from), ToDate: toDate(to)})
	if err != nil {
		return nil, err
	}
	lines, err := s.q.ReportInvoiceLines(ctx, dbgen.ReportInvoiceLinesParams{FromDate: toDate(from), ToDate: toDate(to)})
	if err != nil {
		return nil, err
	}
	byID := make(map[uuid.UUID]int, len(heads))
	out := make([]invoice.Draft, len(heads))
	for i, h := range heads {
		byID[h.ID] = i
		out[i] = invoice.Draft{ID: h.ID, CustomerID: h.CustomerID, CustomerName: h.CustomerName, Number: h.InvoiceNumber,
			IssueDate: fromDate(h.IssueDate), DueDate: fromDatePtr(h.DueDate), Payable: h.Payable}
	}
	for _, l := range lines {
		if i, ok := byID[l.InvoiceID]; ok {
			out[i].Lines = append(out[i].Lines, invoice.Line{ItemName: l.ItemName, Unit: l.Unit, Quantity: decimal.New(l.QuantityMilli, -3),
				UnitPrice: l.UnitPrice, Discount: l.Discount, TaxPercent: int(l.TaxPercent)})
		}
	}
	return out, nil
}

func (s *ReportStore) Attention(ctx context.Context) ([]report.Attention, error) {
	rows, err := s.q.AttentionInvoices(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]report.Attention, len(rows))
	for i, r := range rows {
		reason := r.LastError
		if reason == "" && r.Status == string(invoice.StatusSending) {
			reason = "Gönderim uzun süredir tamamlanmadı"
		}
		out[i] = report.Attention{ID: r.ID, Status: invoice.Status(r.Status), Customer: r.CustomerName, Number: r.InvoiceNumber,
			Reason: reason, Since: r.UpdatedAt.Time, Payable: r.Payable}
	}
	return out, nil
}

func (s *ReportStore) InvoiceCounts(ctx context.Context, monthStart time.Time) (report.InvoiceCounts, error) {
	rows, err := s.q.InvoiceCountsByKind(ctx, toDate(monthStart))
	if err != nil {
		return report.InvoiceCounts{}, err
	}
	out := report.InvoiceCounts{Rows: make([]report.CountRow, len(rows))}
	for i, r := range rows {
		out.Rows[i] = report.CountRow{Kind: invoice.Kind(r.Kind), Status: invoice.Status(r.Status), ThisMonth: r.ThisMonth, N: int(r.N)}
	}
	return out, nil
}
