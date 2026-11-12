// Package money converts between kuruş (int64) and Turkish lira text. Money is always int64 kuruş.
package money

import (
	"errors"
	"strings"

	"github.com/shopspring/decimal"
)

// ErrInvalid is returned for text that is not a valid lira amount.
var ErrInvalid = errors.New("geçersiz tutar")

// Parse reads a user-typed lira amount ("1.234,56", "1234,5", "1234.56", "-50") into kuruş.
// More than two decimals are rejected instead of silently rounded.
func Parse(s string) (int64, error) {
	s = strings.TrimSpace(strings.NewReplacer("₺", "", "TL", "", " ", "").Replace(s))
	if s == "" {
		return 0, nil
	}
	if strings.Contains(s, ",") {
		s = strings.ReplaceAll(s, ".", "") // binlik ayırıcı
		s = strings.Replace(s, ",", ".", 1)
	} else if strings.Count(s, ".") > 1 {
		s = strings.ReplaceAll(s, ".", "")
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return 0, ErrInvalid
	}
	if d.Exponent() < -2 {
		return 0, ErrInvalid
	}
	return d.Shift(2).IntPart(), nil
}

// Format renders kuruş as "1.234,56" (tr-TR, no currency sign).
func Format(kurus int64) string {
	neg := kurus < 0
	if neg {
		kurus = -kurus
	}
	whole, frac := kurus/100, kurus%100
	digits := decimal.NewFromInt(whole).String()
	var b strings.Builder
	for i, c := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	out := b.String() + "," + string(rune('0'+frac/10)) + string(rune('0'+frac%10))
	if neg {
		return "-" + out
	}
	return out
}

// FormatTL renders kuruş as "1.234,56 ₺".
func FormatTL(kurus int64) string { return Format(kurus) + " ₺" }
