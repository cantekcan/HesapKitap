package pdf_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hesapkitap/internal/customer"
	"hesapkitap/internal/invoice"
	"hesapkitap/internal/platform/pdf"
)

func TestDraftInvoiceRendersPDFWithTurkishText(t *testing.T) {
	d := invoice.Draft{
		IssueDate: time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC),
		Notes:     "ğüşiöçİ Ğ Ü Ş Ö Ç deneme notu",
		Lines: []invoice.Line{{
			ItemName: "Danışmanlık hizmeti ğüşıöç", Unit: "HUR", Quantity: decimal.NewFromInt(2),
			UnitPrice: 50000, TaxPercent: 20,
		}},
	}
	totals, err := d.Totals()
	require.NoError(t, err)
	c := customer.Customer{Name: "Çağrı Şişli Ltd.", Kind: customer.KindCompany, TaxID: "1234567890", City: "İstanbul"}

	out, err := pdf.DraftInvoice(d, c, totals, time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.True(t, len(out) > 1000)
	assert.Equal(t, "%PDF", string(out[:4]))
}
