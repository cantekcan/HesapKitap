// Package ledger computes customer balances, statements and overdue amounts.
//
// Kural (docs/ROADMAP.md Faz 6): borç = cariye işlenen faturalar + açılış bakiyesi; alacak = tahsilatlar.
// Bakiye = borç − alacak. Pozitif bakiye "müşteri bize borçlu" demektir. Tüm tutarlar int64 kuruştur.
// Taslak, hatalı/reddedilen ve iptal edilen faturalar veriye hiç girmez (sorgu katmanı eler).
package ledger

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"

	"hesapkitap/internal/payment"
)

// Invoice is an invoice that counts towards the customer's account.
type Invoice struct {
	ID        uuid.UUID
	Number    string
	IssueDate time.Time
	DueDate   *time.Time
	Amount    int64 // kuruş, KDV dahil ödenecek tutar
}

// EffectiveDue is the due date; an invoice without a due date is due on its issue date.
func (i Invoice) EffectiveDue() time.Time {
	if i.DueDate != nil {
		return *i.DueDate
	}
	return i.IssueDate
}

// Data is everything the ledger needs for one customer.
type Data struct {
	CustomerID uuid.UUID
	Name       string
	Active     bool
	Opening    int64 // açılış bakiyesi; + müşteri bize borçlu
	Invoices   []Invoice
	Payments   []payment.Payment
}

// RowKind tells what a statement row is.
type RowKind string

const (
	RowCarried RowKind = "CARRIED" // devir (seçilen aralıktan önceki bakiye)
	RowOpening RowKind = "OPENING" // açılış bakiyesi
	RowInvoice RowKind = "INVOICE"
	RowPayment RowKind = "PAYMENT"
)

// Row is one line of the statement.
type Row struct {
	Date    time.Time
	Kind    RowKind
	Ref     string // fatura no veya ödeme yöntemi
	Note    string
	ID      uuid.UUID
	Debit   int64 // borç
	Credit  int64 // alacak
	Balance int64 // satır sonrası yürüyen bakiye
}

// Balance is opening + Σ invoices − Σ payments.
func (d Data) Balance() int64 {
	b := d.Opening
	for _, i := range d.Invoices {
		b += i.Amount
	}
	for _, p := range d.Payments {
		b -= p.Amount
	}
	return b
}

func day(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC) }

type movement struct {
	date   time.Time
	order  int // aynı gün: önce fatura, sonra tahsilat
	kind   RowKind
	ref    string
	note   string
	id     uuid.UUID
	debit  int64
	credit int64
}

// Statement lists the movements in date order with a running balance. from/to are inclusive; zero means open.
// If from is set, one RowCarried row shows the balance before it.
func Statement(d Data, from, to time.Time) []Row {
	var ms []movement
	for _, i := range d.Invoices {
		ms = append(ms, movement{date: day(i.IssueDate), order: 1, kind: RowInvoice, ref: i.Number, id: i.ID, debit: i.Amount})
	}
	for _, p := range d.Payments {
		ms = append(ms, movement{date: day(p.Date), order: 2, kind: RowPayment, ref: p.Method.Label(), note: p.Note, id: p.ID, credit: p.Amount})
	}
	sort.SliceStable(ms, func(a, b int) bool {
		if !ms[a].date.Equal(ms[b].date) {
			return ms[a].date.Before(ms[b].date)
		}
		return ms[a].order < ms[b].order
	})

	bal := d.Opening
	var rows []Row
	if from.IsZero() {
		rows = append(rows, Row{Kind: RowOpening, Debit: max(d.Opening, 0), Credit: max(-d.Opening, 0), Balance: bal})
	}
	from, to = day(from), day(to)
	carried := false
	for _, m := range ms {
		inRange := (from.IsZero() || !m.date.Before(from)) && (to.IsZero() || !m.date.After(to))
		if !from.IsZero() && m.date.Before(from) {
			bal += m.debit - m.credit
			continue
		}
		if !from.IsZero() && !carried {
			carried = true
			rows = append(rows, Row{Date: from, Kind: RowCarried, Balance: bal})
		}
		if !inRange {
			break
		}
		bal += m.debit - m.credit
		rows = append(rows, Row{Date: m.date, Kind: m.kind, Ref: m.ref, Note: m.note, ID: m.id, Debit: m.debit, Credit: m.credit, Balance: bal})
	}
	if !from.IsZero() && !carried {
		rows = append(rows, Row{Date: from, Kind: RowCarried, Balance: bal})
	}
	return rows
}

// OpenItem is an invoice (or the opening balance) with what is still unpaid after the FIFO allocation.
type OpenItem struct {
	ID          uuid.UUID
	Number      string
	IssueDate   time.Time
	Due         time.Time
	Amount      int64
	Remaining   int64
	DaysOverdue int // vadesi geçmişse gecikme günü, değilse 0
	IsOpening   bool
}

// Position is the customer's standing on a given day.
type Position struct {
	Balance           int64      // borç − alacak
	Overdue           int64      // vadesi geçmiş ödenmemiş tutar
	Credit            int64      // fazla ödeme (hiçbir borca mahsup edilemeyen tutar)
	LastPayment       *time.Time // son tahsilat tarihi
	LastPaymentAmount int64
	OpenItems         []OpenItem // yalnızca ödenmemiş kalanı olanlar, en eskiden başlayarak
}

// Position allocates payments to debts and reports what is open and overdue as of today.
//
// FIFO: bir faturaya bağlı tahsilat önce o faturaya mahsup edilir; kalan tutar ve bağlantısız tahsilatlar
// en eski borçtan başlayarak dağıtılır. Açılış bakiyesi en eski borçtur ve vadesi geçmiş sayılır.
// Eksi açılış bakiyesi (müşteri alacaklı) bir tahsilat gibi davranır.
func (d Data) Position(today time.Time) Position {
	today = day(today)
	remaining := make(map[uuid.UUID]int64, len(d.Invoices))
	for _, i := range d.Invoices {
		remaining[i.ID] = i.Amount
	}

	var pool int64
	if d.Opening < 0 {
		pool = -d.Opening
	}
	var last *payment.Payment
	for idx := range d.Payments {
		p := d.Payments[idx]
		if last == nil || !day(p.Date).Before(day(last.Date)) {
			last = &d.Payments[idx]
		}
		left := p.Amount
		if p.InvoiceID != nil {
			if rem, ok := remaining[*p.InvoiceID]; ok {
				use := min(rem, left)
				remaining[*p.InvoiceID] = rem - use
				left -= use
			}
		}
		pool += left
	}

	var items []OpenItem
	if d.Opening > 0 {
		use := min(d.Opening, pool)
		pool -= use
		if rem := d.Opening - use; rem > 0 {
			items = append(items, OpenItem{Number: "Açılış bakiyesi", Amount: d.Opening, Remaining: rem, IsOpening: true,
				Due: time.Time{}, DaysOverdue: 0})
		}
	}

	sorted := append([]Invoice(nil), d.Invoices...)
	sort.SliceStable(sorted, func(a, b int) bool {
		if !day(sorted[a].IssueDate).Equal(day(sorted[b].IssueDate)) {
			return day(sorted[a].IssueDate).Before(day(sorted[b].IssueDate))
		}
		return sorted[a].EffectiveDue().Before(sorted[b].EffectiveDue())
	})
	for _, i := range sorted {
		rem := remaining[i.ID]
		use := min(rem, pool)
		pool -= use
		rem -= use
		if rem <= 0 {
			continue
		}
		due := day(i.EffectiveDue())
		over := 0
		if due.Before(today) {
			over = int(today.Sub(due).Hours() / 24)
		}
		items = append(items, OpenItem{ID: i.ID, Number: i.Number, IssueDate: day(i.IssueDate), Due: due, Amount: i.Amount, Remaining: rem, DaysOverdue: over})
	}

	pos := Position{Balance: d.Balance(), Credit: pool, OpenItems: items}
	for _, it := range items {
		if it.IsOpening || it.DaysOverdue > 0 {
			pos.Overdue += it.Remaining
		}
	}
	if last != nil {
		t := day(last.Date)
		pos.LastPayment, pos.LastPaymentAmount = &t, last.Amount
	}
	return pos
}

// Source loads ledger data; implemented in internal/platform/db.
type Source interface {
	Customer(ctx context.Context, id uuid.UUID) (Data, error)
	All(ctx context.Context) ([]Data, error)
}
