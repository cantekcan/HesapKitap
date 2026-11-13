package httpserver

import (
	"net/http"
	"sort"
	"strings"

	"github.com/google/uuid"

	"hesapkitap/internal/customer"
	"hesapkitap/internal/ledger"
	"hesapkitap/internal/platform/http/views"
)

// whatsappNumber turns a Turkish phone number into the international digits wa.me expects ("" if unusable).
func whatsappNumber(phone string) string {
	var b strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	d := b.String()
	switch {
	case strings.HasPrefix(d, "90") && len(d) == 12:
		return d
	case strings.HasPrefix(d, "0") && len(d) == 11:
		return "90" + d[1:]
	case len(d) == 10:
		return "90" + d
	}
	return ""
}

func (s *server) reminders(w http.ResponseWriter, r *http.Request) {
	all, err := s.Ledger.All(r.Context())
	if err != nil {
		s.serverError(w, r, "hatırlatma verisi okunamadı", err)
		return
	}
	cs, _, err := s.Customers.List(r.Context(), customer.ListFilter{Limit: 100000})
	if err != nil {
		s.serverError(w, r, "müşteriler okunamadı", err)
		return
	}
	byID := make(map[uuid.UUID]customer.Customer, len(cs))
	for _, c := range cs {
		byID[c.ID] = c
	}
	now := today()
	var rows []views.ReminderRow
	for _, d := range all {
		rem, ok := ledger.BuildReminder(d.Name, d.Position(now), now)
		if !ok {
			continue
		}
		c := byID[d.CustomerID]
		rows = append(rows, views.ReminderRow{
			CustomerID: d.CustomerID.String(), Reminder: rem, Text: rem.Text(), Email: c.Email, WhatsApp: whatsappNumber(c.Phone), Phone: c.Phone,
		})
	}
	sort.SliceStable(rows, func(a, b int) bool {
		if rows[a].Reminder.OverdueTotal != rows[b].Reminder.OverdueTotal {
			return rows[a].Reminder.OverdueTotal > rows[b].Reminder.OverdueTotal
		}
		return rows[a].Reminder.UpcomingSum > rows[b].Reminder.UpcomingSum
	})
	render(w, r, http.StatusOK, views.RemindersPage(s.pageData(r, "reminders", "Hatırlatmalar"), rows))
}
