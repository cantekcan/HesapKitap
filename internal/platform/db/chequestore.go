package db

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hesapkitap/internal/cheque"
	"hesapkitap/internal/payment"
	"hesapkitap/internal/platform/db/dbgen"
)

// ChequeStore implements cheque.Store.
type ChequeStore struct{ q *dbgen.Queries }

func NewChequeStore(q *dbgen.Queries) *ChequeStore { return &ChequeStore{q: q} }

var _ cheque.Store = (*ChequeStore)(nil)

func (s *ChequeStore) List(ctx context.Context, f cheque.ListFilter) ([]cheque.Cheque, int, error) {
	total, err := s.q.CountCheques(ctx, dbgen.CountChequesParams{Status: string(f.Status), Q: f.Query, FromDue: toDate(f.From), ToDue: toDate(f.To)})
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.q.ListCheques(ctx, dbgen.ListChequesParams{Status: string(f.Status), Q: f.Query, FromDue: toDate(f.From), ToDue: toDate(f.To),
		Lim: int32(f.Limit), Off: int32(f.Offset)}) //nolint:gosec // sayfa boyutu sabit ve küçük
	if err != nil {
		return nil, 0, err
	}
	out := make([]cheque.Cheque, len(rows))
	for i, r := range rows {
		out[i] = cheque.Cheque{ID: r.ID, PaymentID: r.PaymentID, CustomerID: r.CustomerID, CustomerName: r.CustomerName,
			Kind: payment.Method(r.Method), Number: r.Number, Bank: r.Bank, Drawer: r.Drawer, ReceivedAt: fromDate(r.PaidAt),
			DueDate: fromDate(r.DueDate), Amount: r.Amount, Status: cheque.Status(r.Status), StatusDate: fromDatePtr(r.StatusDate),
			Note: r.Note, EndorsedTo: r.EndorsedTo}
	}
	return out, int(total), nil
}

func (s *ChequeStore) Get(ctx context.Context, id uuid.UUID) (cheque.Cheque, error) {
	r, err := s.q.GetCheque(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return cheque.Cheque{}, cheque.ErrNotFound
	}
	if err != nil {
		return cheque.Cheque{}, err
	}
	return cheque.Cheque{ID: r.ID, PaymentID: r.PaymentID, CustomerID: r.CustomerID, CustomerName: r.CustomerName,
		Kind: payment.Method(r.Method), Number: r.Number, Bank: r.Bank, Drawer: r.Drawer, ReceivedAt: fromDate(r.PaidAt),
		DueDate: fromDate(r.DueDate), Amount: r.Amount, Status: cheque.Status(r.Status), StatusDate: fromDatePtr(r.StatusDate),
		Note: r.Note, EndorsedTo: r.EndorsedTo}, nil
}

func (s *ChequeStore) SetStatus(ctx context.Context, id uuid.UUID, to cheque.Status, allowedFrom []cheque.Status, date time.Time) (bool, error) {
	from := make([]string, len(allowedFrom))
	for i, a := range allowedFrom {
		from[i] = string(a)
	}
	n, err := s.q.SetChequeStatus(ctx, dbgen.SetChequeStatusParams{ID: id, Status: string(to), StatusDate: toDate(date), AllowedFrom: from})
	return n > 0, err
}

func (s *ChequeStore) Totals(ctx context.Context) (cheque.Totals, error) {
	rows, err := s.q.ChequeStatusTotals(ctx)
	if err != nil {
		return nil, err
	}
	out := cheque.Totals{}
	for _, r := range rows {
		out[cheque.Status(r.Status)] = cheque.Total{Count: int(r.N), Amount: r.Total}
	}
	return out, nil
}

func (s *ChequeStore) DueSummary(ctx context.Context, today, soon time.Time) (cheque.DueSummary, error) {
	r, err := s.q.ChequeDueSummary(ctx, dbgen.ChequeDueSummaryParams{Today: toDate(today), Soon: toDate(soon)})
	if err != nil {
		return cheque.DueSummary{}, err
	}
	return cheque.DueSummary{
		Overdue: cheque.Total{Count: int(r.OverdueN), Amount: r.OverdueTotal},
		Soon:    cheque.Total{Count: int(r.SoonN), Amount: r.SoonTotal},
		All:     cheque.Total{Count: int(r.AllN), Amount: r.AllTotal},
	}, nil
}

func (s *ChequeStore) PortfolioDues(ctx context.Context) ([]cheque.Due, error) {
	rows, err := s.q.PortfolioChequeDues(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]cheque.Due, len(rows))
	for i, r := range rows {
		out[i] = cheque.Due{Date: fromDate(r.DueDate), Amount: r.Amount}
	}
	return out, nil
}

func (s *ChequeStore) Habits(ctx context.Context, since time.Time) ([]cheque.Habit, error) {
	rows, err := s.q.CustomerPaymentHabits(ctx, toDate(since))
	if err != nil {
		return nil, err
	}
	out := make([]cheque.Habit, len(rows))
	for i, r := range rows {
		out[i] = cheque.Habit{CustomerID: r.CustomerID, CustomerName: r.CustomerName, Payments: int(r.PaymentN), Paid: r.PaidTotal,
			ByCheque: r.ChequeTotal, AvgTermDays: r.AvgTermDays, Bounced: int(r.BouncedN)}
	}
	return out, nil
}
