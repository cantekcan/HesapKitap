package db

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hesapkitap/internal/customer"
	"hesapkitap/internal/ledger"
	"hesapkitap/internal/payment"
	"hesapkitap/internal/platform/db/dbgen"
)

func optUUID(u uuid.NullUUID) *uuid.UUID {
	if !u.Valid {
		return nil
	}
	id := u.UUID
	return &id
}

// PaymentStore implements payment.Store.
type PaymentStore struct{ q *dbgen.Queries }

func NewPaymentStore(q *dbgen.Queries) *PaymentStore { return &PaymentStore{q: q} }

var _ payment.Store = (*PaymentStore)(nil)

func (s *PaymentStore) Create(ctx context.Context, p payment.Payment) (uuid.UUID, error) {
	var inv uuid.NullUUID
	if p.InvoiceID != nil {
		inv = uuid.NullUUID{UUID: *p.InvoiceID, Valid: true}
	}
	return s.q.CreatePayment(ctx, dbgen.CreatePaymentParams{
		CustomerID: p.CustomerID, PaidAt: toDate(p.Date), Amount: p.Amount, Method: string(p.Method), Note: p.Note, InvoiceID: inv,
	})
}

func (s *PaymentStore) Delete(ctx context.Context, id uuid.UUID) error {
	n, err := s.q.DeletePayment(ctx, id)
	if err != nil {
		return err
	}
	if n == 0 {
		return payment.ErrNotFound
	}
	return nil
}

func (s *PaymentStore) Get(ctx context.Context, id uuid.UUID) (payment.Payment, error) {
	r, err := s.q.GetPayment(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return payment.Payment{}, payment.ErrNotFound
	}
	if err != nil {
		return payment.Payment{}, err
	}
	return payment.Payment{ID: r.ID, CustomerID: r.CustomerID, CustomerName: r.CustomerName, Date: fromDate(r.PaidAt),
		Amount: r.Amount, Method: payment.Method(r.Method), Note: r.Note, InvoiceID: optUUID(r.InvoiceID)}, nil
}

func (s *PaymentStore) List(ctx context.Context, f payment.ListFilter) ([]payment.Payment, int, error) {
	total, err := s.q.CountPayments(ctx, dbgen.CountPaymentsParams{Q: f.Query, FromDate: toDate(f.From), ToDate: toDate(f.To)})
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.q.ListPayments(ctx, dbgen.ListPaymentsParams{Q: f.Query, FromDate: toDate(f.From), ToDate: toDate(f.To),
		Lim: int32(f.Limit), Off: int32(f.Offset)}) //nolint:gosec // sayfa boyutu sabit ve küçük
	if err != nil {
		return nil, 0, err
	}
	out := make([]payment.Payment, len(rows))
	for i, r := range rows {
		out[i] = payment.Payment{ID: r.ID, CustomerID: r.CustomerID, CustomerName: r.CustomerName, Date: fromDate(r.PaidAt),
			Amount: r.Amount, Method: payment.Method(r.Method), Note: r.Note, InvoiceID: optUUID(r.InvoiceID)}
	}
	return out, int(total), nil
}

func (s *PaymentStore) Total(ctx context.Context, from, to time.Time) (int64, error) {
	return s.q.SumPayments(ctx, dbgen.SumPaymentsParams{FromDate: toDate(from), ToDate: toDate(to)})
}

// LedgerStore implements ledger.Source.
type LedgerStore struct{ q *dbgen.Queries }

func NewLedgerStore(q *dbgen.Queries) *LedgerStore { return &LedgerStore{q: q} }

var _ ledger.Source = (*LedgerStore)(nil)

func (s *LedgerStore) Customer(ctx context.Context, id uuid.UUID) (ledger.Data, error) {
	c, err := s.q.GetCustomer(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ledger.Data{}, customer.ErrNotFound
	}
	if err != nil {
		return ledger.Data{}, err
	}
	invs, err := s.q.LedgerInvoicesByCustomer(ctx, id)
	if err != nil {
		return ledger.Data{}, err
	}
	pays, err := s.q.LedgerPaymentsByCustomer(ctx, id)
	if err != nil {
		return ledger.Data{}, err
	}
	d := ledger.Data{CustomerID: c.ID, Name: c.Name, Active: c.Active, Opening: c.OpeningBalance}
	for _, r := range invs {
		d.Invoices = append(d.Invoices, ledger.Invoice{ID: r.ID, Number: r.InvoiceNumber, IssueDate: fromDate(r.IssueDate), DueDate: fromDatePtr(r.DueDate), Amount: r.Payable})
	}
	for _, r := range pays {
		d.Payments = append(d.Payments, payment.Payment{ID: r.ID, CustomerID: r.CustomerID, Date: fromDate(r.PaidAt), Amount: r.Amount,
			Method: payment.Method(r.Method), Note: r.Note, InvoiceID: optUUID(r.InvoiceID)})
	}
	return d, nil
}

func (s *LedgerStore) All(ctx context.Context) ([]ledger.Data, error) {
	cs, err := s.q.LedgerCustomersAll(ctx)
	if err != nil {
		return nil, err
	}
	invs, err := s.q.LedgerInvoicesAll(ctx)
	if err != nil {
		return nil, err
	}
	pays, err := s.q.LedgerPaymentsAll(ctx)
	if err != nil {
		return nil, err
	}
	idx := make(map[uuid.UUID]int, len(cs))
	out := make([]ledger.Data, len(cs))
	for i, c := range cs {
		idx[c.ID] = i
		out[i] = ledger.Data{CustomerID: c.ID, Name: c.Name, Active: c.Active, Opening: c.OpeningBalance}
	}
	for _, r := range invs {
		if i, ok := idx[r.CustomerID]; ok {
			out[i].Invoices = append(out[i].Invoices, ledger.Invoice{ID: r.ID, Number: r.InvoiceNumber, IssueDate: fromDate(r.IssueDate), DueDate: fromDatePtr(r.DueDate), Amount: r.Payable})
		}
	}
	for _, r := range pays {
		if i, ok := idx[r.CustomerID]; ok {
			out[i].Payments = append(out[i].Payments, payment.Payment{ID: r.ID, CustomerID: r.CustomerID, Date: fromDate(r.PaidAt), Amount: r.Amount,
				Method: payment.Method(r.Method), Note: r.Note, InvoiceID: optUUID(r.InvoiceID)})
		}
	}
	return out, nil
}
