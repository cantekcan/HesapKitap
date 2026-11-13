package pdf

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hesapkitap/internal/customer"
	"hesapkitap/internal/invoice"
)

func TestAmountInWords(t *testing.T) {
	cases := map[int64]string{
		0:            "SIFIR TL",
		100:          "BİR TL",
		303004:       "ÜÇ BİN OTUZ TL DÖRT Kr.",
		100000:       "BİN TL",
		123456:       "BİN İKİ YÜZ OTUZ DÖRT TL ELLİ ALTI Kr.",
		250000_00:    "İKİ YÜZ ELLİ BİN TL",
		1_000_000_0:  "YÜZ BİN TL",
		5:            "BEŞ Kr.",
		1_000_000_00: "BİR MİLYON TL",
	}
	for in, want := range cases {
		assert.Equal(t, want, amountInWords(in), in)
	}
}

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

	for _, seller := range []Company{{}, {Name: "Örnek Ticaret A.Ş.", TaxID: "9999999999", Address: "Atatürk Cd. No:1", City: "İstanbul"}} {
		out, err := DraftInvoice(DraftData{Draft: d, Customer: c, Totals: totals, Seller: seller, Now: time.Now()})
		require.NoError(t, err)
		assert.Greater(t, len(out), 1000)
		assert.Equal(t, "%PDF", string(out[:4]))
	}
}
