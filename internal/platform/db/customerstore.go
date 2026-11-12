package db

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"hesapkitap/internal/customer"
	"hesapkitap/internal/platform/db/dbgen"
)

// CustomerStore implements customer.Store with sqlc queries.
type CustomerStore struct{ q *dbgen.Queries }

func NewCustomerStore(q *dbgen.Queries) *CustomerStore { return &CustomerStore{q: q} }

var _ customer.Store = (*CustomerStore)(nil)

func isUniqueViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

func toCustomer(r dbgen.Customer) customer.Customer {
	c := customer.Customer{
		ID: r.ID, Kind: customer.Kind(r.Kind), TaxID: r.TaxID, Name: r.Name, TaxOffice: r.TaxOffice,
		Address: r.Address, District: r.District, City: r.City, PostalCode: r.PostalCode, Email: r.Email,
		Phone: r.Phone, Note: r.Note, Active: r.Active, IsEInvoiceUser: r.IsEinvoiceUser,
		EInvoiceAlias: r.EinvoiceAlias, OpeningBalance: r.OpeningBalance,
	}
	if r.TaxpayerCheckedAt.Valid {
		t := r.TaxpayerCheckedAt.Time
		c.TaxpayerCheckedAt = &t
	}
	return c
}

func (s *CustomerStore) Get(ctx context.Context, id uuid.UUID) (customer.Customer, error) {
	r, err := s.q.GetCustomer(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return customer.Customer{}, customer.ErrNotFound
	}
	if err != nil {
		return customer.Customer{}, err
	}
	return toCustomer(r), nil
}

func (s *CustomerStore) List(ctx context.Context, f customer.ListFilter) ([]customer.Customer, int, error) {
	total, err := s.q.CountCustomers(ctx, dbgen.CountCustomersParams{Q: f.Query, Status: f.Status})
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.q.ListCustomers(ctx, dbgen.ListCustomersParams{Q: f.Query, Status: f.Status, Lim: int32(f.Limit), Off: int32(f.Offset)}) //nolint:gosec // sayfa boyutu sabit ve küçük
	if err != nil {
		return nil, 0, err
	}
	out := make([]customer.Customer, len(rows))
	for i, r := range rows {
		out[i] = toCustomer(r)
	}
	return out, int(total), nil
}

func (s *CustomerStore) Create(ctx context.Context, c customer.Customer) (customer.Customer, error) {
	r, err := s.q.CreateCustomer(ctx, dbgen.CreateCustomerParams{
		Kind: string(c.Kind), TaxID: c.TaxID, Name: c.Name, TaxOffice: c.TaxOffice, Address: c.Address,
		District: c.District, City: c.City, PostalCode: c.PostalCode, Email: c.Email, Phone: c.Phone,
		Note: c.Note, Active: c.Active, OpeningBalance: c.OpeningBalance,
	})
	if isUniqueViolation(err) {
		return customer.Customer{}, customer.ErrDuplicateTaxID
	}
	if err != nil {
		return customer.Customer{}, err
	}
	return toCustomer(r), nil
}

func (s *CustomerStore) Update(ctx context.Context, c customer.Customer) error {
	n, err := s.q.UpdateCustomer(ctx, dbgen.UpdateCustomerParams{
		ID: c.ID, Kind: string(c.Kind), TaxID: c.TaxID, Name: c.Name, TaxOffice: c.TaxOffice, Address: c.Address,
		District: c.District, City: c.City, PostalCode: c.PostalCode, Email: c.Email, Phone: c.Phone,
		Note: c.Note, Active: c.Active, OpeningBalance: c.OpeningBalance,
	})
	if isUniqueViolation(err) {
		return customer.ErrDuplicateTaxID
	}
	if err != nil {
		return err
	}
	if n == 0 {
		return customer.ErrNotFound
	}
	return nil
}

// SaveTaxpayer stores the lookup result. A zero checkedAt clears the check time.
func (s *CustomerStore) SaveTaxpayer(ctx context.Context, id uuid.UUID, isEInvoiceUser bool, alias string, checkedAt time.Time) error {
	n, err := s.q.SaveCustomerTaxpayer(ctx, dbgen.SaveCustomerTaxpayerParams{
		ID: id, IsEinvoiceUser: isEInvoiceUser, EinvoiceAlias: alias,
		TaxpayerCheckedAt: pgtype.Timestamptz{Time: checkedAt, Valid: !checkedAt.IsZero()},
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return customer.ErrNotFound
	}
	return nil
}
