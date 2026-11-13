package supplier

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func d(y int, m time.Month, day int) time.Time { return time.Date(y, m, day, 0, 0, 0, 0, time.UTC) }

func ptr[T any](v T) *T { return &v }

func TestStatementRunningBalance(t *testing.T) {
	entries := []Entry{{Date: d(2026, 1, 10), Amount: 5000, DocNo: "F-1"}, {Date: d(2026, 1, 20), Amount: 3000}}
	pays := []Payment{
		{Date: d(2026, 1, 10), Amount: 1000, Method: MethodCash}, // aynı gün: alım önce
		{Date: d(2026, 1, 25), Amount: 2000, Method: MethodCheque, ChequeDue: ptr(d(2026, 5, 1))},
	}
	rows := Statement(500, entries, pays)

	assert.Len(t, rows, 5)
	assert.Equal(t, "Açılış bakiyesi", rows[0].Ref)
	assert.Equal(t, int64(500), rows[0].Balance)
	assert.Equal(t, "F-1", rows[1].Ref)
	assert.Equal(t, int64(5500), rows[1].Balance)
	assert.True(t, rows[2].Payment)
	assert.Equal(t, int64(4500), rows[2].Balance)
	assert.Equal(t, "Alım", rows[3].Ref)
	assert.Equal(t, int64(7500), rows[3].Balance)
	assert.Equal(t, int64(5500), rows[4].Balance)
	assert.Contains(t, rows[4].Note, "vade 01.05.2026")
}

func TestStatementWithoutOpeningHasNoOpeningRow(t *testing.T) {
	assert.Empty(t, Statement(0, nil, nil))
}

func TestPaymentValidation(t *testing.T) {
	today := d(2026, 6, 1)
	ok := Payment{Date: today, Amount: 100, Method: MethodTransfer}
	assert.Empty(t, ok.Validate(today))

	cheque := Payment{Date: today, Amount: 100, Method: MethodCheque}
	assert.Contains(t, cheque.Validate(today), "cheque_due")
	cheque.ChequeDue = ptr(d(2026, 5, 1))
	assert.Contains(t, cheque.Validate(today), "cheque_due") // vade ödeme tarihinden önce
	cheque.ChequeDue = ptr(d(2026, 9, 1))
	assert.Empty(t, cheque.Validate(today))

	bad := Payment{Date: d(2027, 1, 1), Amount: 0, Method: Method("X")}
	errs := bad.Validate(today)
	assert.Contains(t, errs, "paid_at")
	assert.Contains(t, errs, "amount")
	assert.Contains(t, errs, "method")
}

func TestEndorsedIsNotAFormMethod(t *testing.T) {
	for _, m := range Methods {
		assert.NotEqual(t, MethodEndorsed, m)
	}
}

func TestSupplierValidation(t *testing.T) {
	assert.Contains(t, Supplier{}.Validate(), "name")
	assert.Contains(t, Supplier{Name: "A", TaxID: "123"}.Validate(), "tax_id")
	assert.Empty(t, Supplier{Name: "A", TaxID: "1234567890"}.Validate())
}

func TestEntryValidation(t *testing.T) {
	today := d(2026, 6, 1)
	assert.Empty(t, Entry{Date: today, Amount: 1}.Validate(today))
	assert.Contains(t, Entry{Date: today, Amount: 0}.Validate(today), "amount")
	assert.Contains(t, Entry{Date: today, Amount: 1, Due: ptr(d(2026, 5, 1))}.Validate(today), "due_date")
}
