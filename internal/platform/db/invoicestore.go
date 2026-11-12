package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"hesapkitap/internal/invoice"
	"hesapkitap/internal/platform/db/dbgen"
)

// InvoiceStore implements invoice.Store. Header and lines are always written in one transaction.
type InvoiceStore struct{ pool *pgxpool.Pool }

func NewInvoiceStore(pool *pgxpool.Pool) *InvoiceStore { return &InvoiceStore{pool: pool} }

var _ invoice.Store = (*InvoiceStore)(nil)

var milli = decimal.NewFromInt(1000)

func toDate(t time.Time) pgtype.Date { return pgtype.Date{Time: t, Valid: !t.IsZero()} }

func toDatePtr(t *time.Time) pgtype.Date {
	if t == nil {
		return pgtype.Date{}
	}
	return toDate(*t)
}

func fromDate(d pgtype.Date) time.Time {
	if !d.Valid {
		return time.Time{}
	}
	return time.Date(d.Time.Year(), d.Time.Month(), d.Time.Day(), 0, 0, 0, 0, time.UTC)
}

func fromDatePtr(d pgtype.Date) *time.Time {
	if !d.Valid {
		return nil
	}
	t := fromDate(d)
	return &t
}

func (s *InvoiceStore) Get(ctx context.Context, id uuid.UUID) (invoice.Draft, error) {
	q := dbgen.New(s.pool)
	h, err := q.GetInvoice(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return invoice.Draft{}, invoice.ErrNotFound
	}
	if err != nil {
		return invoice.Draft{}, err
	}
	rows, err := q.ListInvoiceLines(ctx, id)
	if err != nil {
		return invoice.Draft{}, err
	}
	d := invoice.Draft{
		ID: h.ID, Status: invoice.Status(h.Status), CustomerID: h.CustomerID, CustomerName: h.CustomerName,
		IssueDate: fromDate(h.IssueDate), DueDate: fromDatePtr(h.DueDate), Notes: h.Notes, Payable: h.Payable,
	}
	for _, r := range rows {
		l := invoice.Line{
			ItemName: r.ItemName, Unit: r.Unit, Quantity: decimal.NewFromInt(r.QuantityMilli).Div(milli),
			UnitPrice: r.UnitPrice, Discount: r.Discount, TaxPercent: int(r.TaxPercent),
		}
		if r.ProductID.Valid {
			pid := r.ProductID.UUID
			l.ProductID = &pid
		}
		d.Lines = append(d.Lines, l)
	}
	return d, nil
}

func (s *InvoiceStore) List(ctx context.Context, f invoice.ListFilter) ([]invoice.Summary, int, error) {
	q := dbgen.New(s.pool)
	total, err := q.CountInvoices(ctx, dbgen.CountInvoicesParams{Q: f.Query, Status: f.Status})
	if err != nil {
		return nil, 0, err
	}
	rows, err := q.ListInvoices(ctx, dbgen.ListInvoicesParams{Q: f.Query, Status: f.Status, Lim: int32(f.Limit), Off: int32(f.Offset)}) //nolint:gosec // sayfa boyutu sabit ve küçük
	if err != nil {
		return nil, 0, err
	}
	out := make([]invoice.Summary, len(rows))
	for i, r := range rows {
		out[i] = invoice.Summary{
			ID: r.ID, Status: invoice.Status(r.Status), CustomerID: r.CustomerID, CustomerName: r.CustomerName,
			IssueDate: fromDate(r.IssueDate), DueDate: fromDatePtr(r.DueDate), Payable: r.Payable,
		}
	}
	return out, int(total), nil
}

func (s *InvoiceStore) Create(ctx context.Context, d invoice.Draft) (uuid.UUID, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := dbgen.New(tx)

	id, err := q.CreateInvoice(ctx, dbgen.CreateInvoiceParams{
		CustomerID: d.CustomerID, IssueDate: toDate(d.IssueDate), DueDate: toDatePtr(d.DueDate), Notes: d.Notes, Payable: d.Payable,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("fatura yazılamadı: %w", err)
	}
	if err := insertLines(ctx, q, id, d.Lines); err != nil {
		return uuid.Nil, err
	}
	return id, tx.Commit(ctx)
}

func (s *InvoiceStore) Update(ctx context.Context, d invoice.Draft) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := dbgen.New(tx)

	n, err := q.UpdateDraftInvoice(ctx, dbgen.UpdateDraftInvoiceParams{
		ID: d.ID, CustomerID: d.CustomerID, IssueDate: toDate(d.IssueDate), DueDate: toDatePtr(d.DueDate), Notes: d.Notes, Payable: d.Payable,
	})
	if err != nil {
		return fmt.Errorf("fatura güncellenemedi: %w", err)
	}
	if n == 0 {
		return invoice.ErrNotEditable
	}
	if err := q.DeleteInvoiceLines(ctx, d.ID); err != nil {
		return err
	}
	if err := insertLines(ctx, q, d.ID, d.Lines); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *InvoiceStore) Delete(ctx context.Context, id uuid.UUID) error {
	n, err := dbgen.New(s.pool).DeleteDraftInvoice(ctx, id)
	if err != nil {
		return err
	}
	if n == 0 {
		return invoice.ErrNotEditable
	}
	return nil
}

func insertLines(ctx context.Context, q *dbgen.Queries, invoiceID uuid.UUID, lines []invoice.Line) error {
	for i, l := range lines {
		pid := uuid.NullUUID{}
		if l.ProductID != nil {
			pid = uuid.NullUUID{UUID: *l.ProductID, Valid: true}
		}
		err := q.InsertInvoiceLine(ctx, dbgen.InsertInvoiceLineParams{
			InvoiceID: invoiceID, Position: int32(i), ProductID: pid, ItemName: l.ItemName, Unit: l.Unit, //nolint:gosec // kalem sayısı küçük
			QuantityMilli: l.Quantity.Mul(milli).IntPart(), UnitPrice: l.UnitPrice, Discount: l.Discount,
			TaxPercent: int32(l.TaxPercent), //nolint:gosec // 0-100 doğrulandı
		})
		if err != nil {
			return fmt.Errorf("%d. kalem yazılamadı: %w", i+1, err)
		}
	}
	return nil
}
