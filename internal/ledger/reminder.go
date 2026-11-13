package ledger

import (
	"fmt"
	"strings"
	"time"

	"hesapkitap/internal/money"
)

// UpcomingDays is how many days ahead a due date counts as "yaklaşan".
const UpcomingDays = 7

// Reminder is what to tell one customer about unpaid items.
type Reminder struct {
	CustomerName string
	Overdue      []OpenItem // vadesi geçmiş (açılış bakiyesi dahil)
	Upcoming     []OpenItem // önümüzdeki UpcomingDays gün içinde vadesi gelecek
	OverdueTotal int64
	UpcomingSum  int64
}

// BuildReminder splits a customer's open items into overdue and upcoming. ok is false when there is nothing to remind.
func BuildReminder(name string, pos Position, today time.Time) (Reminder, bool) {
	today = day(today)
	r := Reminder{CustomerName: name}
	for _, it := range pos.OpenItems {
		switch {
		case it.IsOpening || it.DaysOverdue > 0:
			r.Overdue = append(r.Overdue, it)
			r.OverdueTotal += it.Remaining
		case !it.Due.After(today.AddDate(0, 0, UpcomingDays)):
			r.Upcoming = append(r.Upcoming, it)
			r.UpcomingSum += it.Remaining
		}
	}
	return r, len(r.Overdue)+len(r.Upcoming) > 0
}

// Text is the ready-to-send message (Turkish, polite, plain text so it works in WhatsApp and e-mail).
func (r Reminder) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Merhaba %s,\n\n", r.CustomerName)
	if len(r.Overdue) > 0 {
		fmt.Fprintf(&b, "Vadesi geçmiş toplam %s bakiyeniz bulunmaktadır:\n", money.FormatTL(r.OverdueTotal))
		writeItems(&b, r.Overdue)
	}
	if len(r.Upcoming) > 0 {
		if len(r.Overdue) > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "Önümüzdeki günlerde vadesi gelecek %s tutarında faturanız var:\n", money.FormatTL(r.UpcomingSum))
		writeItems(&b, r.Upcoming)
	}
	b.WriteString("\nÖdemeyi yaptıysanız bu mesajı dikkate almayınız. İyi çalışmalar dileriz.")
	return b.String()
}

func writeItems(b *strings.Builder, items []OpenItem) {
	for _, it := range items {
		if it.IsOpening {
			fmt.Fprintf(b, "- Devreden bakiye: %s\n", money.FormatTL(it.Remaining))
			continue
		}
		fmt.Fprintf(b, "- %s: %s (vade %s)\n", it.Number, money.FormatTL(it.Remaining), it.Due.Format("02.01.2006"))
	}
}
