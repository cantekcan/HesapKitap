package trendyol

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hesapkitap/internal/invoice"
)

func TestBuildRequestMapsInvoice(t *testing.T) {
	lines := []invoice.Line{
		{ItemName: "Danışmanlık", Unit: "HUR", Quantity: decimal.RequireFromString("1.5"), UnitPrice: 10000, TaxPercent: 20},
		{ItemName: "Kâğıt", Unit: "PA", Quantity: decimal.RequireFromString("2"), UnitPrice: 2500, Discount: 500, TaxPercent: 10},
	}
	in := make([]invoice.LineInput, len(lines))
	for i, l := range lines {
		in[i] = l.Input()
	}
	totals, err := invoice.InvoiceCalculator{}.Calculate(in)
	require.NoError(t, err)
	ettn := uuid.New()

	req := buildRequest(invoice.IssueRequest{
		ETTN: ettn, LocalRef: "yerel-1", Kind: invoice.KindEArchive, Scenario: "EARSIVFATURA",
		IssueDate: time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC), Notes: " not ",
		Recipient: invoice.Recipient{Person: true, TaxID: "10000000146", Name: "Ali Can Veli", City: "İstanbul", Email: "ALI@Ornek.COM"},
		Lines:     lines, Totals: totals,
	})

	assert.Equal(t, ettn.String(), req.InvoiceInfo.InvoiceUUID, "ETTN bizim ürettiğimiz")
	assert.Equal(t, InvoiceTypeEArchive, req.InvoiceInfo.InvoiceType)
	assert.Equal(t, InvoiceTypeCodeSale, req.InvoiceInfo.InvoiceTypeCode)
	assert.Equal(t, "yerel-1", req.LocalReferenceID)
	assert.Equal(t, "Ali Can", req.RecipientInfo.Name)
	assert.Equal(t, "Veli", req.RecipientInfo.Surname)
	assert.Equal(t, "ali@ornek.com", req.RecipientInfo.Email)
	assert.Equal(t, []string{"not"}, req.Notes)
	require.NotNil(t, req.IssuedAt, "geçmiş tarih açıkça gönderilir")

	assert.Len(t, req.InvoiceLines, 2)
	assert.Equal(t, UnitCode("HUR"), req.InvoiceLines[0].UnitCode)
	assert.EqualValues(t, 15000, req.InvoiceLines[0].TaxableAmount)
	assert.EqualValues(t, 3000, req.InvoiceLines[0].TaxAmount)
	assert.EqualValues(t, 500, req.InvoiceLines[1].TotalDiscountAmount)
	assert.EqualValues(t, 4500, req.InvoiceLines[1].TaxableAmount)

	assert.Len(t, req.TotalTax.SubTotalTaxes, 2)
	assert.EqualValues(t, 3450, req.TotalTax.TotalTaxAmount)
	assert.EqualValues(t, 20000, req.InvoiceTotal.LineExtensionAmount)
	assert.EqualValues(t, 500, req.InvoiceTotal.AllowanceTotalAmount)
	assert.EqualValues(t, 19500, req.InvoiceTotal.TaxExclusiveAmount)
	assert.EqualValues(t, 22950, req.InvoiceTotal.PayableAmount)

	// Miktar ve birim fiyat JSON'da float64'ten geçmeden sayı olarak gider.
	body, err := json.Marshal(req)
	require.NoError(t, err)
	assert.Contains(t, string(body), `"quantity":1.5`)
	assert.Contains(t, string(body), `"unitPriceAmount":10000`)
}

func TestIssuedAtIsNilForToday(t *testing.T) {
	assert.Nil(t, issuedAt(time.Now().In(istanbul)))
	assert.Nil(t, issuedAt(time.Time{}))
}

func TestSingleWordPersonNameStaysInName(t *testing.T) {
	info := recipientInfo(invoice.Recipient{Person: true, Name: "Madonna"})
	assert.Equal(t, "Madonna", info.Name)
	assert.Empty(t, info.Surname)
}
