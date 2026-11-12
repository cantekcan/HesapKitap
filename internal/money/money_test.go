package money_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hesapkitap/internal/money"
)

func TestParse(t *testing.T) {
	for in, want := range map[string]int64{
		"": 0, "0": 0, "1234": 123400, "1.234,56": 123456, "1234,5": 123450, "1234.56": 123456,
		"1.234.567": 123456700, "-50": -5000, "12,00 ₺": 1200, " 7,05 ": 705,
	} {
		got, err := money.Parse(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for _, bad := range []string{"abc", "1,234", "1,2,3", "12,345"} {
		_, err := money.Parse(bad)
		assert.Error(t, err, bad)
	}
}

func TestFormat(t *testing.T) {
	for in, want := range map[int64]string{0: "0,00", 5: "0,05", 123456: "1.234,56", 100000000: "1.000.000,00", -1050: "-10,50"} {
		assert.Equal(t, want, money.Format(in))
	}
	assert.Equal(t, "1.234,56 ₺", money.FormatTL(123456))
}
