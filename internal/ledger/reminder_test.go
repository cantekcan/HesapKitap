package ledger

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hesapkitap/internal/payment"
)

func TestBuildReminder(t *testing.T) {
	today := d(2026, 3, 10)
	data := Data{
		Opening: 5000,
		Invoices: []Invoice{
			inv("A1", d(2026, 1, 1), ptr(d(2026, 2, 1)), 10000),  // gecikmiş
			inv("A2", d(2026, 3, 1), ptr(d(2026, 3, 15)), 20000), // 5 gün sonra
			inv("A3", d(2026, 3, 1), ptr(d(2026, 6, 1)), 30000),  // uzak
		},
		Payments: []payment.Payment{pay(d(2026, 1, 2), 1000)},
	}
	rem, ok := BuildReminder("Ali Veli", data.Position(today), today)
	require.True(t, ok)
	require.Len(t, rem.Overdue, 2) // açılış (kalan 4000) + A1
	assert.Equal(t, int64(14000), rem.OverdueTotal)
	require.Len(t, rem.Upcoming, 1)
	assert.Equal(t, int64(20000), rem.UpcomingSum)
	text := rem.Text()
	assert.Contains(t, text, "Merhaba Ali Veli")
	assert.Contains(t, text, "140,00 ₺")
	assert.Contains(t, text, "A2: 200,00 ₺ (vade 15.03.2026)")

	_, ok = BuildReminder("X", Data{}.Position(today), today)
	assert.False(t, ok)
}
