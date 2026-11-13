package ledger

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hesapkitap/internal/payment"
)

func d(y int, m time.Month, dd int) time.Time { return time.Date(y, m, dd, 0, 0, 0, 0, time.UTC) }
func ptr(t time.Time) *time.Time              { return &t }

func inv(n string, issue time.Time, due *time.Time, amt int64) Invoice {
	return Invoice{ID: uuid.New(), Number: n, IssueDate: issue, DueDate: due, Amount: amt}
}
func pay(date time.Time, amt int64) payment.Payment {
	return payment.Payment{ID: uuid.New(), Date: date, Amount: amt, Method: payment.MethodCash}
}

func TestBalance(t *testing.T) {
	data := Data{Opening: 5000, Invoices: []Invoice{inv("A", d(2026, 1, 1), nil, 10000)}, Payments: []payment.Payment{pay(d(2026, 1, 5), 3000)}}
	assert.Equal(t, int64(12000), data.Balance())
}

func TestPositionPartialPaymentFIFO(t *testing.T) {
	a := inv("A", d(2026, 1, 1), ptr(d(2026, 1, 31)), 10000)
	b := inv("B", d(2026, 2, 1), ptr(d(2026, 3, 1)), 20000)
	data := Data{Invoices: []Invoice{b, a}, Payments: []payment.Payment{pay(d(2026, 2, 10), 12000)}}
	pos := data.Position(d(2026, 2, 15))
	require.Len(t, pos.OpenItems, 1)
	assert.Equal(t, "B", pos.OpenItems[0].Number)
	assert.Equal(t, int64(18000), pos.OpenItems[0].Remaining)
	assert.Equal(t, int64(0), pos.Overdue) // B vadesi gelmedi
	assert.Equal(t, int64(18000), pos.Balance)
	require.NotNil(t, pos.LastPayment)
	assert.Equal(t, d(2026, 2, 10), *pos.LastPayment)
}

func TestPositionOverdue(t *testing.T) {
	a := inv("A", d(2026, 1, 1), ptr(d(2026, 1, 31)), 10000)
	data := Data{Invoices: []Invoice{a}, Payments: []payment.Payment{pay(d(2026, 2, 1), 4000)}}
	pos := data.Position(d(2026, 2, 10))
	assert.Equal(t, int64(6000), pos.Overdue)
	assert.Equal(t, 10, pos.OpenItems[0].DaysOverdue)
}

func TestNoDueDateIsDueOnIssueDate(t *testing.T) {
	data := Data{Invoices: []Invoice{inv("A", d(2026, 1, 1), nil, 100)}}
	assert.Equal(t, int64(100), data.Position(d(2026, 1, 2)).Overdue)
	assert.Equal(t, int64(0), data.Position(d(2026, 1, 1)).Overdue)
}

func TestLinkedPaymentGoesToItsInvoice(t *testing.T) {
	a := inv("A", d(2026, 1, 1), nil, 10000)
	b := inv("B", d(2026, 2, 1), nil, 10000)
	p := pay(d(2026, 2, 2), 10000)
	p.InvoiceID = &b.ID
	pos := Data{Invoices: []Invoice{a, b}, Payments: []payment.Payment{p}}.Position(d(2026, 3, 1))
	require.Len(t, pos.OpenItems, 1)
	assert.Equal(t, "A", pos.OpenItems[0].Number)
}

func TestOpeningBalance(t *testing.T) {
	data := Data{Opening: 5000, Invoices: []Invoice{inv("A", d(2026, 1, 1), ptr(d(2026, 6, 1)), 1000)}, Payments: []payment.Payment{pay(d(2026, 1, 2), 3000)}}
	pos := data.Position(d(2026, 1, 3))
	require.Len(t, pos.OpenItems, 2)
	assert.True(t, pos.OpenItems[0].IsOpening)
	assert.Equal(t, int64(2000), pos.OpenItems[0].Remaining)
	assert.Equal(t, int64(2000), pos.Overdue)

	credit := Data{Opening: -5000, Invoices: []Invoice{inv("A", d(2026, 1, 1), nil, 3000)}}.Position(d(2026, 1, 5))
	assert.Empty(t, credit.OpenItems)
	assert.Equal(t, int64(2000), credit.Credit)
	assert.Equal(t, int64(-2000), credit.Balance)
}

func TestOverpaymentBecomesCredit(t *testing.T) {
	pos := Data{Invoices: []Invoice{inv("A", d(2026, 1, 1), nil, 1000)}, Payments: []payment.Payment{pay(d(2026, 1, 2), 1500)}}.Position(d(2026, 1, 3))
	assert.Equal(t, int64(500), pos.Credit)
	assert.Equal(t, int64(-500), pos.Balance)
}

func TestStatementRunningBalance(t *testing.T) {
	data := Data{Opening: 1000, Invoices: []Invoice{inv("A", d(2026, 1, 10), nil, 5000)}, Payments: []payment.Payment{pay(d(2026, 1, 10), 2000), pay(d(2026, 2, 1), 1000)}}
	rows := Statement(data, time.Time{}, time.Time{})
	require.Len(t, rows, 4)
	assert.Equal(t, RowOpening, rows[0].Kind)
	assert.Equal(t, int64(6000), rows[1].Balance)
	assert.Equal(t, int64(4000), rows[2].Balance) // aynı gün önce fatura, sonra tahsilat
	assert.Equal(t, int64(3000), rows[3].Balance)

	ranged := Statement(data, d(2026, 2, 1), time.Time{})
	require.Len(t, ranged, 2)
	assert.Equal(t, RowCarried, ranged[0].Kind)
	assert.Equal(t, int64(4000), ranged[0].Balance)
	assert.Equal(t, int64(3000), ranged[1].Balance)

	empty := Statement(data, d(2026, 3, 1), time.Time{})
	require.Len(t, empty, 1)
	assert.Equal(t, int64(3000), empty[0].Balance)
}
