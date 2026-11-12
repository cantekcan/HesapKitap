package invoice_test

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hesapkitap/internal/invoice"
)

func qty(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestLine(t *testing.T) {
	var c invoice.InvoiceCalculator
	for _, tc := range []struct {
		name string
		in   invoice.LineInput
		want invoice.LineResult
	}{
		{"tek satır %20", invoice.LineInput{Quantity: qty("1"), UnitPrice: 10000, TaxPercent: 20},
			invoice.LineResult{Gross: 10000, Taxable: 10000, Tax: 2000, Total: 12000}},
		{"ondalık miktar: 1,5 × 33,33 = 49,995 → 50,00 (yarım yukarı)", invoice.LineInput{Quantity: qty("1.5"), UnitPrice: 3333, TaxPercent: 20},
			invoice.LineResult{Gross: 5000, Taxable: 5000, Tax: 1000, Total: 6000}},
		{"indirimli", invoice.LineInput{Quantity: qty("2"), UnitPrice: 5000, Discount: 1000, TaxPercent: 10},
			invoice.LineResult{Gross: 10000, Discount: 1000, Taxable: 9000, Tax: 900, Total: 9900}},
		{"KDV 0,3 kuruş → 0", invoice.LineInput{Quantity: qty("1"), UnitPrice: 3, TaxPercent: 10},
			invoice.LineResult{Gross: 3, Taxable: 3, Tax: 0, Total: 3}},
		{"KDV 0,5 kuruş → 1 (sıfırdan uzağa)", invoice.LineInput{Quantity: qty("1"), UnitPrice: 5, TaxPercent: 10},
			invoice.LineResult{Gross: 5, Taxable: 5, Tax: 1, Total: 6}},
		{"KDV 2,5 kuruş → 3", invoice.LineInput{Quantity: qty("1"), UnitPrice: 25, TaxPercent: 10},
			invoice.LineResult{Gross: 25, Taxable: 25, Tax: 3, Total: 28}},
		{"KDV 2,4 kuruş → 2", invoice.LineInput{Quantity: qty("1"), UnitPrice: 24, TaxPercent: 10},
			invoice.LineResult{Gross: 24, Taxable: 24, Tax: 2, Total: 26}},
		{"üç ondalıklı miktar", invoice.LineInput{Quantity: qty("0.333"), UnitPrice: 10000, TaxPercent: 20},
			invoice.LineResult{Gross: 3330, Taxable: 3330, Tax: 666, Total: 3996}},
		{"%1 KDV", invoice.LineInput{Quantity: qty("3"), UnitPrice: 1999, TaxPercent: 1},
			invoice.LineResult{Gross: 5997, Taxable: 5997, Tax: 60, Total: 6057}},
		{"fiyat sıfır", invoice.LineInput{Quantity: qty("1"), UnitPrice: 0, TaxPercent: 20},
			invoice.LineResult{}},
		{"indirim tüm tutar", invoice.LineInput{Quantity: qty("1"), UnitPrice: 1000, Discount: 1000, TaxPercent: 20},
			invoice.LineResult{Gross: 1000, Discount: 1000}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := c.Line(tc.in)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestLineErrors(t *testing.T) {
	var c invoice.InvoiceCalculator
	for name, in := range map[string]invoice.LineInput{
		"miktar sıfır":    {Quantity: qty("0"), UnitPrice: 100, TaxPercent: 20},
		"miktar eksi":     {Quantity: qty("-1"), UnitPrice: 100, TaxPercent: 20},
		"fiyat eksi":      {Quantity: qty("1"), UnitPrice: -1, TaxPercent: 20},
		"indirim eksi":    {Quantity: qty("1"), UnitPrice: 100, Discount: -1, TaxPercent: 20},
		"indirim > brüt":  {Quantity: qty("1"), UnitPrice: 100, Discount: 101, TaxPercent: 20},
		"KDV oranı büyük": {Quantity: qty("1"), UnitPrice: 100, TaxPercent: 101},
	} {
		_, err := c.Line(in)
		assert.Error(t, err, name)
	}
}

func TestCalculateMultipleRates(t *testing.T) {
	var c invoice.InvoiceCalculator
	got, err := c.Calculate([]invoice.LineInput{
		{Quantity: qty("1"), UnitPrice: 10000, TaxPercent: 20},
		{Quantity: qty("2"), UnitPrice: 2500, TaxPercent: 10},
		{Quantity: qty("1"), UnitPrice: 5000, Discount: 500, TaxPercent: 20},
	})
	require.NoError(t, err)

	// 20%: 10000 + 4500 = 14500 matrah, 2000 + 900 = 2900 KDV; 10%: 5000 matrah, 500 KDV.
	assert.Equal(t, []invoice.TaxGroup{
		{Percent: 10, Taxable: 5000, Tax: 500},
		{Percent: 20, Taxable: 14500, Tax: 2900},
	}, got.TaxGroups)
	assert.EqualValues(t, 20000, got.LineExtension)
	assert.EqualValues(t, 500, got.Allowance)
	assert.EqualValues(t, 19500, got.TaxExclusive)
	assert.EqualValues(t, 3400, got.TotalTax)
	assert.EqualValues(t, 22900, got.TaxInclusive)
	assert.EqualValues(t, 22900, got.Payable)
	assert.Len(t, got.Lines, 3)
}

// KDV satır bazında yuvarlanır; grup toplamı satır KDV'lerinin toplamıdır (satır ile fatura toplamı tutarlı kalır).
func TestGroupTaxIsSumOfLineTaxes(t *testing.T) {
	var c invoice.InvoiceCalculator
	got, err := c.Calculate([]invoice.LineInput{
		{Quantity: qty("1"), UnitPrice: 5, TaxPercent: 10},
		{Quantity: qty("1"), UnitPrice: 5, TaxPercent: 10},
	})
	require.NoError(t, err)
	assert.Equal(t, []invoice.TaxGroup{{Percent: 10, Taxable: 10, Tax: 2}}, got.TaxGroups)
	assert.EqualValues(t, 12, got.Payable)
}

func TestCalculateErrors(t *testing.T) {
	var c invoice.InvoiceCalculator
	_, err := c.Calculate(nil)
	assert.Error(t, err)

	_, err = c.Calculate([]invoice.LineInput{
		{Quantity: qty("1"), UnitPrice: 100, TaxPercent: 20},
		{Quantity: qty("0"), UnitPrice: 100, TaxPercent: 20},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "2. kalem")
}

// Toplamlar her zaman: matrah + KDV = ödenecek ve Σ satır toplamı = ödenecek.
func TestTotalsAreInternallyConsistent(t *testing.T) {
	var c invoice.InvoiceCalculator
	got, err := c.Calculate([]invoice.LineInput{
		{Quantity: qty("1.333"), UnitPrice: 1234, TaxPercent: 20},
		{Quantity: qty("0.5"), UnitPrice: 999, Discount: 7, TaxPercent: 10},
		{Quantity: qty("7"), UnitPrice: 15, TaxPercent: 1},
	})
	require.NoError(t, err)
	var sumTotal, sumGroupTax, sumGroupTaxable int64
	for _, l := range got.Lines {
		sumTotal += l.Total
	}
	for _, g := range got.TaxGroups {
		sumGroupTax += g.Tax
		sumGroupTaxable += g.Taxable
	}
	assert.Equal(t, got.Payable, sumTotal)
	assert.Equal(t, got.TotalTax, sumGroupTax)
	assert.Equal(t, got.TaxExclusive, sumGroupTaxable)
	assert.Equal(t, got.TaxExclusive+got.TotalTax, got.Payable)
}
