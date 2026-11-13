// Package report builds the dashboard and report figures from invoices, payments and the ledger.
// Everything here is pure computation over already-loaded data; money is int64 kuruş.
package report

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"

	"hesapkitap/internal/invoice"
	"hesapkitap/internal/ledger"
)

// Attention is an invoice that needs a human look (error, rejected, stuck in sending).
type Attention struct {
	ID       uuid.UUID
	Status   invoice.Status
	Customer string
	Number   string
	Reason   string
	Since    time.Time
	Payable  int64
}

// Source loads what the reports need; implemented in internal/platform/db.
type Source interface {
	// Sales returns the invoices counted on the account whose issue date is in [from, to], with their lines.
	Sales(ctx context.Context, from, to time.Time) ([]invoice.Draft, error)
	Attention(ctx context.Context) ([]Attention, error)
}

func month(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC) }

// ---- Satış raporu (mali müşavire) ----

// MonthRow is one month of the sales report.
type MonthRow struct {
	Month    time.Time
	Count    int
	Taxable  int64
	Tax      int64
	Total    int64
	Invoices []InvoiceRow
}

// InvoiceRow is one invoice of the sales report.
type InvoiceRow struct {
	Number    string
	Customer  string
	IssueDate time.Time
	Taxable   int64
	Tax       int64
	Total     int64
}

// RateRow is the KDV breakdown for one rate.
type RateRow struct {
	Percent int
	Taxable int64
	Tax     int64
}

// SalesReport is the period report: month by month, by KDV rate, and the invoice list.
type SalesReport struct {
	From, To time.Time
	Months   []MonthRow
	Rates    []RateRow
	Invoices []InvoiceRow
	Count    int
	Taxable  int64
	Tax      int64
	Total    int64
}

// BuildSales totals the invoices with the InvoiceCalculator (the only place that rounds).
func BuildSales(from, to time.Time, drafts []invoice.Draft) (SalesReport, error) {
	rep := SalesReport{From: from, To: to}
	byMonth := map[time.Time]*MonthRow{}
	byRate := map[int]*RateRow{}
	for _, d := range drafts {
		t, err := d.Totals()
		if err != nil {
			return SalesReport{}, err
		}
		row := InvoiceRow{Number: d.Number, Customer: d.CustomerName, IssueDate: d.IssueDate, Taxable: t.TaxExclusive, Tax: t.TotalTax, Total: t.Payable}
		rep.Invoices = append(rep.Invoices, row)
		m := month(d.IssueDate)
		mr := byMonth[m]
		if mr == nil {
			mr = &MonthRow{Month: m}
			byMonth[m] = mr
		}
		mr.Count++
		mr.Taxable += row.Taxable
		mr.Tax += row.Tax
		mr.Total += row.Total
		mr.Invoices = append(mr.Invoices, row)
		for _, g := range t.TaxGroups {
			rr := byRate[g.Percent]
			if rr == nil {
				rr = &RateRow{Percent: g.Percent}
				byRate[g.Percent] = rr
			}
			rr.Taxable += g.Taxable
			rr.Tax += g.Tax
		}
		rep.Count++
		rep.Taxable += row.Taxable
		rep.Tax += row.Tax
		rep.Total += row.Total
	}
	for _, mr := range byMonth {
		rep.Months = append(rep.Months, *mr)
	}
	sort.Slice(rep.Months, func(a, b int) bool { return rep.Months[a].Month.Before(rep.Months[b].Month) })
	for _, rr := range byRate {
		rep.Rates = append(rep.Rates, *rr)
	}
	sort.Slice(rep.Rates, func(a, b int) bool { return rep.Rates[a].Percent < rep.Rates[b].Percent })
	return rep, nil
}

// ---- Alacak yaşlandırma ----

// Bucket indexes of the aging report.
const (
	BucketCurrent = iota // vadesi gelmemiş
	Bucket1to30          // 1–30 gün gecikmiş
	Bucket31to60         // 31–60
	Bucket61to90         // 61–90
	Bucket90Plus         // 90+
	BucketOpening        // devreden açılış bakiyesi
	bucketCount
)

// BucketLabels are the Turkish column headings, in bucket order.
var BucketLabels = [bucketCount]string{"Vadesi gelmemiş", "1–30 gün", "31–60 gün", "61–90 gün", "90+ gün", "Açılış bakiyesi"}

func bucketOf(it ledger.OpenItem) int {
	switch {
	case it.IsOpening:
		return BucketOpening
	case it.DaysOverdue <= 0:
		return BucketCurrent
	case it.DaysOverdue <= 30:
		return Bucket1to30
	case it.DaysOverdue <= 60:
		return Bucket31to60
	case it.DaysOverdue <= 90:
		return Bucket61to90
	}
	return Bucket90Plus
}

// AgingRow is one customer in the aging report.
type AgingRow struct {
	CustomerID uuid.UUID
	Name       string
	Buckets    [bucketCount]int64
	Total      int64 // açık kalemlerin toplamı
	Overdue    int64
	Credit     int64 // fazla ödeme
}

// AgingReport lists customers with open items, largest first.
type AgingReport struct {
	Rows    []AgingRow
	Buckets [bucketCount]int64
	Total   int64
	Overdue int64
}

// BuildAging allocates payments FIFO for every customer and buckets what is still open.
func BuildAging(all []ledger.Data, today time.Time) AgingReport {
	var rep AgingReport
	for _, d := range all {
		pos := d.Position(today)
		if len(pos.OpenItems) == 0 {
			continue
		}
		row := AgingRow{CustomerID: d.CustomerID, Name: d.Name, Overdue: pos.Overdue, Credit: pos.Credit}
		for _, it := range pos.OpenItems {
			b := bucketOf(it)
			row.Buckets[b] += it.Remaining
			row.Total += it.Remaining
			rep.Buckets[b] += it.Remaining
		}
		rep.Total += row.Total
		rep.Overdue += row.Overdue
		rep.Rows = append(rep.Rows, row)
	}
	sort.SliceStable(rep.Rows, func(a, b int) bool { return rep.Rows[a].Total > rep.Rows[b].Total })
	return rep
}

// ---- Pano ----

// MonthPoint is one bar pair of the dashboard chart.
type MonthPoint struct {
	Month     time.Time
	Invoiced  int64
	Collected int64
}

// Debtor is a customer on the "most owing" list.
type Debtor struct {
	CustomerID uuid.UUID
	Name       string
	Balance    int64
	Overdue    int64
}

// Dashboard holds the home page figures.
type Dashboard struct {
	Receivable      int64 // müşterilerin toplam borcu (yalnızca pozitif bakiyeler)
	Overdue         int64
	OverdueCount    int // vadesi geçmiş borcu olan müşteri sayısı
	InvoicedMonth   int64
	InvoicedPrev    int64
	CollectedMonth  int64
	CollectedPrev   int64
	Series          []MonthPoint // son 12 ay, eskiden yeniye
	TopDebtors      []Debtor
	Attention       []Attention
	CustomerCount   int
	CollectionRatio int // son 12 ayda tahsilat / fatura (%), fatura yoksa 0
	Aging           AgingReport
}

// BuildDashboard computes everything from the ledger data (one pass over customers).
func BuildDashboard(all []ledger.Data, today time.Time, attention []Attention) Dashboard {
	cur := month(today)
	dash := Dashboard{Attention: attention}
	idx := map[time.Time]int{}
	for i := 11; i >= 0; i-- {
		m := cur.AddDate(0, -i, 0)
		idx[m] = len(dash.Series)
		dash.Series = append(dash.Series, MonthPoint{Month: m})
	}
	prev := cur.AddDate(0, -1, 0)
	var debtors []Debtor
	for _, d := range all {
		if d.Active {
			dash.CustomerCount++
		}
		pos := d.Position(today)
		if pos.Balance > 0 {
			dash.Receivable += pos.Balance
			debtors = append(debtors, Debtor{CustomerID: d.CustomerID, Name: d.Name, Balance: pos.Balance, Overdue: pos.Overdue})
		}
		if pos.Overdue > 0 {
			dash.Overdue += pos.Overdue
			dash.OverdueCount++
		}
		for _, in := range d.Invoices {
			m := month(in.IssueDate)
			if i, ok := idx[m]; ok {
				dash.Series[i].Invoiced += in.Amount
			}
			switch m {
			case cur:
				dash.InvoicedMonth += in.Amount
			case prev:
				dash.InvoicedPrev += in.Amount
			}
		}
		for _, p := range d.Payments {
			m := month(p.Date)
			if i, ok := idx[m]; ok {
				dash.Series[i].Collected += p.Amount
			}
			switch m {
			case cur:
				dash.CollectedMonth += p.Amount
			case prev:
				dash.CollectedPrev += p.Amount
			}
		}
	}
	sort.SliceStable(debtors, func(a, b int) bool { return debtors[a].Balance > debtors[b].Balance })
	if len(debtors) > 10 {
		debtors = debtors[:10]
	}
	dash.TopDebtors = debtors
	dash.Aging = BuildAging(all, today)
	var inv, col int64
	for _, p := range dash.Series {
		inv += p.Invoiced
		col += p.Collected
	}
	if inv > 0 {
		dash.CollectionRatio = int(col * 100 / inv)
	}
	return dash
}
