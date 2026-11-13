package db

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"hesapkitap/internal/platform/db/dbgen"
	"hesapkitap/internal/supplier"
)

// SupplierStore implements supplier.Store. It needs the pool because endorsing a cheque is a transaction.
type SupplierStore struct {
	pool *pgxpool.Pool
	q    *dbgen.Queries
}

func NewSupplierStore(pool *pgxpool.Pool) *SupplierStore {
	return &SupplierStore{pool: pool, q: dbgen.New(pool)}
}

var _ supplier.Store = (*SupplierStore)(nil)

func nullUUID(id *uuid.UUID) uuid.NullUUID {
	if id == nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *id, Valid: true}
}

func (s *SupplierStore) CreateSupplier(ctx context.Context, sp supplier.Supplier) (uuid.UUID, error) {
	id, err := s.q.CreateSupplier(ctx, dbgen.CreateSupplierParams{Name: sp.Name, TaxID: sp.TaxID, Phone: sp.Phone, Note: sp.Note, OpeningBalance: sp.Opening})
	if isUniqueViolation(err) {
		return uuid.Nil, supplier.ErrDuplicateName
	}
	return id, err
}

func (s *SupplierStore) UpdateSupplier(ctx context.Context, sp supplier.Supplier) error {
	n, err := s.q.UpdateSupplier(ctx, dbgen.UpdateSupplierParams{ID: sp.ID, Name: sp.Name, TaxID: sp.TaxID, Phone: sp.Phone, Note: sp.Note,
		OpeningBalance: sp.Opening, Active: sp.Active})
	if isUniqueViolation(err) {
		return supplier.ErrDuplicateName
	}
	if err != nil {
		return err
	}
	if n == 0 {
		return supplier.ErrNotFound
	}
	return nil
}

func (s *SupplierStore) List(ctx context.Context, query string) ([]supplier.Supplier, error) {
	rows, err := s.q.ListSuppliers(ctx, query)
	if err != nil {
		return nil, err
	}
	out := make([]supplier.Supplier, len(rows))
	for i, r := range rows {
		out[i] = supplier.Supplier{ID: r.ID, Name: r.Name, TaxID: r.TaxID, Phone: r.Phone, Note: r.Note, Opening: r.OpeningBalance,
			Active: r.Active, Balance: r.Balance, PendingCheques: r.PendingCheques}
	}
	return out, nil
}

func (s *SupplierStore) Get(ctx context.Context, id uuid.UUID) (supplier.Supplier, error) {
	r, err := s.q.GetSupplier(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return supplier.Supplier{}, supplier.ErrNotFound
	}
	if err != nil {
		return supplier.Supplier{}, err
	}
	return supplier.Supplier{ID: r.ID, Name: r.Name, TaxID: r.TaxID, Phone: r.Phone, Note: r.Note, Opening: r.OpeningBalance,
		Active: r.Active, Balance: r.Balance, PendingCheques: r.PendingCheques}, nil
}

func (s *SupplierStore) CreateEntry(ctx context.Context, e supplier.Entry) (uuid.UUID, error) {
	return s.q.CreateSupplierEntry(ctx, dbgen.CreateSupplierEntryParams{SupplierID: e.SupplierID, EntryDate: toDate(e.Date), Amount: e.Amount,
		DocNo: e.DocNo, Description: e.Description, DueDate: toDatePtr(e.Due)})
}

func (s *SupplierStore) DeleteEntry(ctx context.Context, id uuid.UUID) error {
	n, err := s.q.DeleteSupplierEntry(ctx, id)
	if err != nil {
		return err
	}
	if n == 0 {
		return supplier.ErrNotFound
	}
	return nil
}

func (s *SupplierStore) Entries(ctx context.Context, supplierID uuid.UUID) ([]supplier.Entry, error) {
	rows, err := s.q.ListSupplierEntries(ctx, supplierID)
	if err != nil {
		return nil, err
	}
	out := make([]supplier.Entry, len(rows))
	for i, r := range rows {
		out[i] = supplier.Entry{ID: r.ID, SupplierID: r.SupplierID, Date: fromDate(r.EntryDate), Amount: r.Amount, DocNo: r.DocNo,
			Description: r.Description, Due: fromDatePtr(r.DueDate), Paid: r.Paid}
	}
	return out, nil
}

func (s *SupplierStore) CreatePayment(ctx context.Context, p supplier.Payment) (uuid.UUID, error) {
	return s.q.CreateSupplierPayment(ctx, dbgen.CreateSupplierPaymentParams{SupplierID: p.SupplierID, PaidAt: toDate(p.Date), Amount: p.Amount,
		Method: string(p.Method), ChequeDue: toDatePtr(p.ChequeDue), ChequeNo: p.ChequeNo, Bank: p.Bank, FromChequeID: nullUUID(p.FromChequeID),
		EntryID: nullUUID(p.EntryID), Note: p.Note})
}

func (s *SupplierStore) Endorse(ctx context.Context, supplierID, chequeID uuid.UUID, date time.Time, entryID *uuid.UUID, note string) (uuid.UUID, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	c, err := q.GetCheque(ctx, chequeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, supplier.ErrNotFound
	}
	if err != nil {
		return uuid.Nil, err
	}
	n, err := q.SetChequeStatus(ctx, dbgen.SetChequeStatusParams{ID: chequeID, Status: "ENDORSED", StatusDate: toDate(date), AllowedFrom: []string{"PORTFOLIO"}})
	if err != nil {
		return uuid.Nil, err
	}
	if n == 0 {
		return uuid.Nil, supplier.ErrChequeUnavailable
	}
	id, err := q.CreateSupplierPayment(ctx, dbgen.CreateSupplierPaymentParams{SupplierID: supplierID, PaidAt: toDate(date), Amount: c.Amount,
		Method: string(supplier.MethodEndorsed), ChequeDue: c.DueDate, ChequeNo: c.Number, Bank: c.Bank,
		FromChequeID: uuid.NullUUID{UUID: chequeID, Valid: true}, EntryID: nullUUID(entryID), Note: note})
	if err != nil {
		return uuid.Nil, err
	}
	return id, tx.Commit(ctx)
}

func (s *SupplierStore) DeletePayment(ctx context.Context, id uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	chequeID, err := q.DeleteSupplierPayment(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return supplier.ErrNotFound
	}
	if err != nil {
		return err
	}
	if chequeID.Valid { // ciro geri alındı: çek portföye döner
		if _, err := q.SetChequeStatus(ctx, dbgen.SetChequeStatusParams{ID: chequeID.UUID, Status: "PORTFOLIO", AllowedFrom: []string{"ENDORSED"}}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *SupplierStore) Payments(ctx context.Context, supplierID uuid.UUID) ([]supplier.Payment, error) {
	rows, err := s.q.ListSupplierPayments(ctx, supplierID)
	if err != nil {
		return nil, err
	}
	out := make([]supplier.Payment, len(rows))
	for i, r := range rows {
		out[i] = supplier.Payment{ID: r.ID, SupplierID: r.SupplierID, Date: fromDate(r.PaidAt), Amount: r.Amount, Method: supplier.Method(r.Method),
			ChequeDue: fromDatePtr(r.ChequeDue), ChequeNo: r.ChequeNo, Bank: r.Bank, Cleared: r.Cleared, FromChequeID: optUUID(r.FromChequeID),
			EntryID: optUUID(r.EntryID), Note: r.Note}
	}
	return out, nil
}

func (s *SupplierStore) SetCleared(ctx context.Context, paymentID uuid.UUID, cleared bool) error {
	n, err := s.q.SetSupplierChequeCleared(ctx, dbgen.SetSupplierChequeClearedParams{ID: paymentID, Cleared: cleared})
	if err != nil {
		return err
	}
	if n == 0 {
		return supplier.ErrNotFound
	}
	return nil
}

func (s *SupplierStore) OwnCheques(ctx context.Context) ([]supplier.OwnCheque, error) {
	rows, err := s.q.ListOwnCheques(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]supplier.OwnCheque, len(rows))
	for i, r := range rows {
		out[i] = supplier.OwnCheque{PaymentID: r.ID, SupplierID: r.SupplierID, SupplierName: r.SupplierName, Date: fromDate(r.PaidAt),
			Amount: r.Amount, Due: fromDate(r.ChequeDue), Number: r.ChequeNo, Bank: r.Bank, Cleared: r.Cleared}
	}
	return out, nil
}

func (s *SupplierStore) PendingOwnDues(ctx context.Context) ([]supplier.Due, error) {
	rows, err := s.q.PendingOwnChequeDues(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]supplier.Due, len(rows))
	for i, r := range rows {
		out[i] = supplier.Due{Date: fromDate(r.DueDate), Amount: r.Amount}
	}
	return out, nil
}

func (s *SupplierStore) DebtTotal(ctx context.Context) (int64, error) {
	return s.q.SupplierDebtTotal(ctx)
}
