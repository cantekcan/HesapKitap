package db

import (
	"context"
	"encoding/json"
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
type InvoiceStore struct {
	pool     *pgxpool.Pool
	enqueuer SendEnqueuer
}

// NewInvoiceStore creates the store. enqueuer may be nil when issuing is not used (tests, tools).
func NewInvoiceStore(pool *pgxpool.Pool, enqueuer SendEnqueuer) *InvoiceStore {
	return &InvoiceStore{pool: pool, enqueuer: enqueuer}
}

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
		Kind: invoice.Kind(h.Kind.String), Scenario: h.Scenario, ETTN: h.Ettn.UUID, Number: h.InvoiceNumber,
		TargetAlias: h.TargetAlias, LastError: h.LastError,
	}
	if h.ProviderStatus.Valid {
		ps := invoice.ProviderStatus(h.ProviderStatus.Int32)
		d.ProviderStatus = &ps
	}
	if len(h.Recipient) > 0 {
		var rj recipientJSON
		if err := json.Unmarshal(h.Recipient, &rj); err != nil {
			return invoice.Draft{}, fmt.Errorf("alıcı bilgisi okunamadı: %w", err)
		}
		rec := invoice.Recipient(rj)
		d.Recipient = &rec
	}
	for _, r := range rows {
		l := invoice.Line{
			ItemName: r.ItemName, Unit: r.Unit, Quantity: decimal.New(r.QuantityMilli, -3),
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
	total, err := q.CountInvoices(ctx, dbgen.CountInvoicesParams{Q: f.Query, Status: f.Status, Kind: f.Kind})
	if err != nil {
		return nil, 0, err
	}
	rows, err := q.ListInvoices(ctx, dbgen.ListInvoicesParams{Q: f.Query, Status: f.Status, Kind: f.Kind, Lim: int32(f.Limit), Off: int32(f.Offset)}) //nolint:gosec // sayfa boyutu sabit ve küçük
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

// SendEnqueuer puts the send job into the queue inside the caller's transaction (transactional outbox).
type SendEnqueuer interface {
	EnqueueSend(ctx context.Context, tx pgx.Tx, invoiceID uuid.UUID) error
}

var _ invoice.IssueStore = (*InvoiceStore)(nil)

// recipientJSON is the stored form of invoice.Recipient (snapshot taken when the invoice is issued).
type recipientJSON struct {
	Person     bool   `json:"person"`
	TaxID      string `json:"taxId"`
	Name       string `json:"name"`
	TaxOffice  string `json:"taxOffice"`
	Address    string `json:"address"`
	District   string `json:"district"`
	City       string `json:"city"`
	PostalCode string `json:"postalCode"`
	Email      string `json:"email"`
	Phone      string `json:"phone"`
}

// BeginSend writes SENDING + ETTN + recipient snapshot and the River job in ONE transaction:
// either both exist or neither does.
func (s *InvoiceStore) BeginSend(ctx context.Context, p invoice.SendPlan) error {
	if s.enqueuer == nil {
		return errors.New("gönderim kuyruğu ayarlı değil")
	}
	rec, err := json.Marshal(recipientJSON(p.Recipient))
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	n, err := dbgen.New(tx).BeginInvoiceSend(ctx, dbgen.BeginInvoiceSendParams{
		ID: p.ID, Kind: pgtype.Text{String: string(p.Kind), Valid: true}, Scenario: p.Scenario,
		Ettn: uuid.NullUUID{UUID: p.ETTN, Valid: true}, TargetAlias: p.TargetAlias, Recipient: rec,
	})
	if err != nil {
		return fmt.Errorf("fatura gönderime alınamadı: %w", err)
	}
	if n == 0 {
		return invoice.ErrNotEditable
	}
	if err := s.enqueuer.EnqueueSend(ctx, tx, p.ID); err != nil {
		return fmt.Errorf("gönderim işi kuyruğa yazılamadı: %w", err)
	}
	return tx.Commit(ctx)
}

func (s *InvoiceStore) RecordResult(ctx context.Context, id uuid.UUID, u invoice.ResultUpdate) error {
	ps := pgtype.Int4{}
	if u.ProviderStatus != nil {
		ps = pgtype.Int4{Int32: int32(*u.ProviderStatus), Valid: true} //nolint:gosec // statü kodları küçük
	}
	return dbgen.New(s.pool).RecordInvoiceResult(ctx, dbgen.RecordInvoiceResultParams{
		ID: id, Status: string(u.Status), ProviderStatus: ps, InvoiceNumber: u.Number, LastError: u.LastError,
	})
}

func (s *InvoiceStore) RevertToDraft(ctx context.Context, id uuid.UUID, reason string) error {
	return dbgen.New(s.pool).RevertInvoiceToDraft(ctx, dbgen.RevertInvoiceToDraftParams{ID: id, LastError: reason})
}

func (s *InvoiceStore) ListPending(ctx context.Context, limit int) ([]uuid.UUID, error) {
	return dbgen.New(s.pool).ListPendingInvoices(ctx, int32(limit)) //nolint:gosec // küçük sabit
}
