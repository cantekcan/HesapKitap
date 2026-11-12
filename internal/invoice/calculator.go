package invoice

import (
	"errors"
	"fmt"
	"sort"

	"github.com/shopspring/decimal"
)

// ALTIN KURAL 1: para int64 kuruş. Ara hesap decimal; yuvarlama YALNIZCA bu dosyadaki round() ile,
// yarımı sıfırdan uzağa (shopspring Round bunu yapar: 0,5 → 1; -0,5 → -1).

// LineInput is one invoice line as typed by the user. Quantity may have decimals (1,5 kg); prices are kuruş.
type LineInput struct {
	Quantity   decimal.Decimal
	UnitPrice  int64 // kuruş, KDV hariç
	Discount   int64 // satır indirimi, kuruş
	TaxPercent int
}

// LineResult is the computed line. All amounts are kuruş.
type LineResult struct {
	Gross    int64 // yuvarla(miktar × birim fiyat)
	Discount int64
	Taxable  int64 // matrah = brüt − indirim
	Tax      int64 // yuvarla(matrah × oran / 100)
	// Total = Taxable + Tax.
	// ⚠️ Trendyol'un invoiceLines.totalAmount alanı KDV dahil mi hariç mi: stage'de doğrulanınca (Faz 5) eşlenir.
	Total int64
}

// TaxGroup is the sum of lines with the same KDV rate (API: subTotalTaxes).
type TaxGroup struct {
	Percent int
	Taxable int64
	Tax     int64
}

// Totals is the computed invoice.
type Totals struct {
	Lines         []LineResult
	TaxGroups     []TaxGroup // oran artan sırada
	LineExtension int64      // Σ brüt (mal/hizmet toplamı)
	Allowance     int64      // Σ indirim
	TaxExclusive  int64      // Σ matrah
	TotalTax      int64      // Σ KDV
	TaxInclusive  int64      // TaxExclusive + TotalTax
	Payable       int64      // = TaxInclusive
}

// InvoiceCalculator is the single place that computes invoice amounts and the single place that rounds.
type InvoiceCalculator struct{}

var hundred = decimal.NewFromInt(100)

// round converts a decimal kuruş value to int64, half away from zero.
func round(d decimal.Decimal) int64 { return d.Round(0).IntPart() }

// Line computes one line.
func (InvoiceCalculator) Line(in LineInput) (LineResult, error) {
	switch {
	case !in.Quantity.IsPositive():
		return LineResult{}, errors.New("miktar sıfırdan büyük olmalı")
	case in.UnitPrice < 0:
		return LineResult{}, errors.New("birim fiyat eksi olamaz")
	case in.Discount < 0:
		return LineResult{}, errors.New("indirim eksi olamaz")
	case in.TaxPercent < 0 || in.TaxPercent > 100:
		return LineResult{}, fmt.Errorf("KDV oranı geçersiz: %d", in.TaxPercent)
	}
	gross := round(in.Quantity.Mul(decimal.NewFromInt(in.UnitPrice)))
	if in.Discount > gross {
		return LineResult{}, errors.New("indirim satır tutarından büyük olamaz")
	}
	taxable := gross - in.Discount
	tax := round(decimal.NewFromInt(taxable).Mul(decimal.NewFromInt(int64(in.TaxPercent))).Div(hundred))
	return LineResult{Gross: gross, Discount: in.Discount, Taxable: taxable, Tax: tax, Total: taxable + tax}, nil
}

// Calculate computes all lines and the invoice totals. An invoice needs at least one line.
func (c InvoiceCalculator) Calculate(lines []LineInput) (Totals, error) {
	if len(lines) == 0 {
		return Totals{}, errors.New("fatura en az bir kalem içermeli")
	}
	t := Totals{Lines: make([]LineResult, len(lines))}
	groups := map[int]*TaxGroup{}
	for i, in := range lines {
		r, err := c.Line(in)
		if err != nil {
			return Totals{}, fmt.Errorf("%d. kalem: %w", i+1, err)
		}
		t.Lines[i] = r
		t.LineExtension += r.Gross
		t.Allowance += r.Discount
		t.TaxExclusive += r.Taxable
		t.TotalTax += r.Tax
		g := groups[in.TaxPercent]
		if g == nil {
			g = &TaxGroup{Percent: in.TaxPercent}
			groups[in.TaxPercent] = g
		}
		g.Taxable += r.Taxable
		g.Tax += r.Tax
	}
	for _, g := range groups {
		t.TaxGroups = append(t.TaxGroups, *g)
	}
	sort.Slice(t.TaxGroups, func(i, j int) bool { return t.TaxGroups[i].Percent < t.TaxGroups[j].Percent })
	t.TaxInclusive = t.TaxExclusive + t.TotalTax
	t.Payable = t.TaxInclusive
	return t, nil
}
