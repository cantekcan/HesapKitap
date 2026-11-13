package report

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hesapkitap/internal/invoice"
	"hesapkitap/internal/ledger"
	"hesapkitap/internal/payment"
)

func d(y int, m time.Month, dd int) time.Time { return time.Date(y, m, dd, 0, 0, 0, 0, time.UTC) }

func TestBuildSalesGroupsByMonthAndRate(t *testing.T) {
	line := func(price int64, tax int) invoice.Line {
		return invoice.Line{ItemName: "x", Quantity: decimal.NewFromInt(1), UnitPrice: price, TaxPercent: tax}
	}
	drafts := []invoice.Draft{
		{Number: "A1", CustomerName: "Ali", IssueDate: d(2026, 1, 5), Lines: []invoice.Line{line(10000, 20), line(10000, 1)}},
		{Number: "A2", CustomerName: "Veli", IssueDate: d(2026, 2, 5), Lines: []invoice.Line{line(5000, 20)}},
	}
	rep, err := BuildSales(d(2026, 1, 1), d(2026, 2, 28), drafts)
	require.NoError(t, err)
	require.Len(t, rep.Months, 2)
	require.Len(t, rep.Rates, 2)
	assert.Equal(t, 1, rep.Rates[0].Percent)
	assert.Equal(t, int64(100), rep.Rates[0].Tax)
	assert.Equal(t, int64(3000), rep.Rates[1].Tax) // 2000 + 1000
	assert.Equal(t, int64(25000), rep.Taxable)
	assert.Equal(t, int64(28100), rep.Total)
	assert.Equal(t, 2, rep.Count)
}

func TestBuildAging(t *testing.T) {
	due := d(2026, 1, 31)
	a := ledger.Invoice{ID: uuid.New(), Number: "A", IssueDate: d(2026, 1, 1), DueDate: &due, Amount: 10000}
	data := []ledger.Data{
		{CustomerID: uuid.New(), Name: "Ali", Invoices: []ledger.Invoice{a}, Payments: []payment.Payment{{Date: d(2026, 2, 1), Amount: 4000}}},
		{CustomerID: uuid.New(), Name: "Kapalı", Invoices: []ledger.Invoice{{ID: uuid.New(), IssueDate: d(2026, 1, 1), Amount: 100}}, Payments: []payment.Payment{{Date: d(2026, 1, 2), Amount: 100}}},
	}
	rep := BuildAging(data, d(2026, 3, 15)) // 43 gün gecikmiş
	require.Len(t, rep.Rows, 1)
	assert.Equal(t, int64(6000), rep.Buckets[Bucket31to60])
	assert.Equal(t, int64(6000), rep.Overdue)
}

func TestBuildDashboard(t *testing.T) {
	now := d(2026, 3, 15)
	inv := ledger.Invoice{ID: uuid.New(), Number: "A", IssueDate: d(2026, 3, 2), Amount: 10000}
	old := ledger.Invoice{ID: uuid.New(), Number: "B", IssueDate: d(2026, 2, 2), Amount: 5000}
	data := []ledger.Data{
		{CustomerID: uuid.New(), Name: "Ali", Active: true, Invoices: []ledger.Invoice{inv, old}, Payments: []payment.Payment{{Date: d(2026, 3, 10), Amount: 2000}}},
	}
	dash := BuildDashboard(data, now, nil)
	assert.Equal(t, int64(13000), dash.Receivable)
	assert.Equal(t, int64(10000), dash.InvoicedMonth)
	assert.Equal(t, int64(5000), dash.InvoicedPrev)
	assert.Equal(t, int64(2000), dash.CollectedMonth)
	require.Len(t, dash.Series, 12)
	assert.Equal(t, d(2026, 3, 1), dash.Series[11].Month)
	assert.Equal(t, int64(10000), dash.Series[11].Invoiced)
	assert.Equal(t, 1, dash.CustomerCount)
	require.Len(t, dash.TopDebtors, 1)
	assert.Equal(t, 13, dash.CollectionRatio) // 2000 / 15000
}
