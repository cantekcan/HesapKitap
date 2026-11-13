// Package cheque tracks cheques and promissory notes (çek/senet) received from customers: their due dates,
// whether they were collected, bounced or endorsed (ciro) to a supplier, and the cash-flow they promise.
package cheque

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"hesapkitap/internal/payment"
)

// Status is where a cheque is in its life.
type Status string

const (
	StatusPortfolio Status = "PORTFOLIO" // elimizde, vadesi bekleniyor
	StatusCollected Status = "COLLECTED" // tahsil edildi
	StatusBounced   Status = "BOUNCED"   // karşılıksız
	StatusEndorsed  Status = "ENDORSED"  // tedarikçiye ciro edildi
)

// Statuses lists the statuses in display order.
var Statuses = []Status{StatusPortfolio, StatusCollected, StatusBounced, StatusEndorsed}

// Label is the Turkish display text.
func (s Status) Label() string {
	switch s {
	case StatusPortfolio:
		return "Portföyde"
	case StatusCollected:
		return "Tahsil edildi"
	case StatusBounced:
		return "Karşılıksız"
	case StatusEndorsed:
		return "Ciro edildi"
	}
	return string(s)
}

// Valid reports whether s is a known status.
func (s Status) Valid() bool {
	for _, v := range Statuses {
		if v == s {
			return true
		}
	}
	return false
}

// Cheque is a cheque or note received as a payment. Amount is kuruş.
type Cheque struct {
	ID           uuid.UUID
	PaymentID    uuid.UUID
	CustomerID   uuid.UUID
	CustomerName string
	Kind         payment.Method // CHEQUE | NOTE
	Number       string
	Bank         string
	Drawer       string // keşideci
	ReceivedAt   time.Time
	DueDate      time.Time
	Amount       int64
	Status       Status
	StatusDate   *time.Time
	Note         string
	EndorsedTo   string // ciro edildiği tedarikçi
}

// Overdue reports whether the cheque is still in the portfolio after its due date.
func (c Cheque) Overdue(today time.Time) bool {
	return c.Status == StatusPortfolio && c.DueDate.Before(today)
}

// DaysToDue is the number of days until the due date (negative when past).
func (c Cheque) DaysToDue(today time.Time) int {
	return int(c.DueDate.Sub(today).Hours() / 24)
}

// ListFilter selects a page of cheques. Status "" = all. From/To filter the due date.
type ListFilter struct {
	Status Status
	Query  string
	From   time.Time
	To     time.Time
	Limit  int
	Offset int
}

// Total is a count and an amount.
type Total struct {
	Count  int
	Amount int64
}

// Totals are the per-status counts and amounts.
type Totals map[Status]Total

// DueSummary is the dashboard summary of the cheques in the portfolio.
type DueSummary struct {
	Overdue Total // vadesi geçmiş ama hâlâ portföyde
	Soon    Total // önümüzdeki 7 gün
	All     Total // portföydeki tümü
}

// Due is a future cash movement: a date and an amount.
type Due struct {
	Date   time.Time
	Amount int64
}

// Habit is how one customer has paid since a date: how much, how much of it by cheque, and for how long.
type Habit struct {
	CustomerID   uuid.UUID
	CustomerName string
	Payments     int
	Paid         int64
	ByCheque     int64 // çek/senet olarak alınan
	AvgTermDays  float64
	Bounced      int
}

// ChequeShare is the share of cheque/note payments in the total, in percent.
func (h Habit) ChequeShare() int {
	if h.Paid == 0 {
		return 0
	}
	return int(h.ByCheque * 100 / h.Paid)
}

// Errors.
var (
	ErrNotFound      = errors.New("çek bulunamadı")
	ErrBadTransition = errors.New("çekin durumu bu şekilde değiştirilemez")
)

// Store is the persistence port, implemented in internal/platform/db.
type Store interface {
	List(ctx context.Context, f ListFilter) ([]Cheque, int, error)
	Get(ctx context.Context, id uuid.UUID) (Cheque, error)
	// SetStatus changes the status when the current one is in allowedFrom; false means it did not match.
	SetStatus(ctx context.Context, id uuid.UUID, to Status, allowedFrom []Status, date time.Time) (bool, error)
	Totals(ctx context.Context) (Totals, error)
	DueSummary(ctx context.Context, today, soon time.Time) (DueSummary, error)
	PortfolioDues(ctx context.Context) ([]Due, error)
	Habits(ctx context.Context, since time.Time) ([]Habit, error)
}

// Service holds the cheque use cases.
type Service struct {
	Store Store
	Now   func() time.Time
}

func (s *Service) today() time.Time {
	n := time.Now()
	if s.Now != nil {
		n = s.Now()
	}
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

func (s *Service) List(ctx context.Context, f ListFilter) ([]Cheque, int, error) {
	return s.Store.List(ctx, f)
}
func (s *Service) Get(ctx context.Context, id uuid.UUID) (Cheque, error) { return s.Store.Get(ctx, id) }
func (s *Service) Totals(ctx context.Context) (Totals, error)            { return s.Store.Totals(ctx) }
func (s *Service) PortfolioDues(ctx context.Context) ([]Due, error) {
	return s.Store.PortfolioDues(ctx)
}

// Habits returns how each customer has paid in the last 12 months.
func (s *Service) Habits(ctx context.Context) ([]Habit, error) {
	return s.Store.Habits(ctx, s.today().AddDate(-1, 0, 0))
}

// DueSummary returns the overdue / next-7-days / all numbers of the portfolio.
func (s *Service) DueSummary(ctx context.Context) (DueSummary, error) {
	t := s.today()
	return s.Store.DueSummary(ctx, t, t.AddDate(0, 0, 7))
}

// SetStatus collects, bounces or restores a cheque to the portfolio. Endorsement is not done here: it happens
// when the cheque is given to a supplier as a payment (internal/supplier), and is undone by deleting that payment.
func (s *Service) SetStatus(ctx context.Context, id uuid.UUID, to Status) error {
	var from []Status
	switch to {
	case StatusCollected, StatusBounced:
		from = []Status{StatusPortfolio}
	case StatusPortfolio:
		from = []Status{StatusCollected, StatusBounced}
	default:
		return ErrBadTransition
	}
	ok, err := s.Store.SetStatus(ctx, id, to, from, s.today())
	if err != nil {
		return err
	}
	if !ok {
		if _, gerr := s.Store.Get(ctx, id); errors.Is(gerr, ErrNotFound) {
			return ErrNotFound
		}
		return ErrBadTransition
	}
	return nil
}

// FlowMonth is the expected cash movement of one month. Overdue is the bucket of dues before today.
type FlowMonth struct {
	Month   time.Time // ayın ilk günü; Overdue için sıfır
	Overdue bool
	In      int64 // portföydeki müşteri çekleri
	Out     int64 // vadesi gelecek kendi çeklerimiz
}

// Net is In − Out.
func (m FlowMonth) Net() int64 { return m.In - m.Out }

// BuildFlow buckets the incoming (customer cheques) and outgoing (our own cheques) dues: first everything that is
// already past due, then `months` calendar months starting with the current one. Later dues are summed into the last month.
func BuildFlow(today time.Time, in, out []Due, months int) []FlowMonth {
	if months < 1 {
		months = 1
	}
	start := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	res := make([]FlowMonth, months+1)
	res[0].Overdue = true
	for i := 0; i < months; i++ {
		res[i+1].Month = start.AddDate(0, i, 0)
	}
	idx := func(d time.Time) int {
		if d.Before(today) {
			return 0
		}
		n := (d.Year()-start.Year())*12 + int(d.Month()-start.Month())
		return min(max(n, 0), months-1) + 1
	}
	for _, d := range in {
		res[idx(d.Date)].In += d.Amount
	}
	for _, d := range out {
		res[idx(d.Date)].Out += d.Amount
	}
	return res
}
