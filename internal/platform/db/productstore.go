package db

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hesapkitap/internal/platform/db/dbgen"
	"hesapkitap/internal/product"
)

// ProductStore implements product.Store with sqlc queries.
type ProductStore struct{ q *dbgen.Queries }

func NewProductStore(q *dbgen.Queries) *ProductStore { return &ProductStore{q: q} }

var _ product.Store = (*ProductStore)(nil)

func toProduct(r dbgen.Product) product.Product {
	return product.Product{
		ID: r.ID, Code: r.Code, Name: r.Name, Unit: r.Unit, UnitPrice: r.UnitPrice,
		TaxPercent: int(r.TaxPercent), Active: r.Active,
	}
}

func (s *ProductStore) Get(ctx context.Context, id uuid.UUID) (product.Product, error) {
	r, err := s.q.GetProduct(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return product.Product{}, product.ErrNotFound
	}
	if err != nil {
		return product.Product{}, err
	}
	return toProduct(r), nil
}

func (s *ProductStore) List(ctx context.Context, f product.ListFilter) ([]product.Product, int, error) {
	total, err := s.q.CountProducts(ctx, dbgen.CountProductsParams{Q: f.Query, Status: f.Status})
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.q.ListProducts(ctx, dbgen.ListProductsParams{Q: f.Query, Status: f.Status, Lim: int32(f.Limit), Off: int32(f.Offset)}) //nolint:gosec // sayfa boyutu sabit ve küçük
	if err != nil {
		return nil, 0, err
	}
	out := make([]product.Product, len(rows))
	for i, r := range rows {
		out[i] = toProduct(r)
	}
	return out, int(total), nil
}

func (s *ProductStore) ListActive(ctx context.Context) ([]product.Product, error) {
	rows, err := s.q.ListActiveProducts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]product.Product, len(rows))
	for i, r := range rows {
		out[i] = toProduct(r)
	}
	return out, nil
}

func (s *ProductStore) Create(ctx context.Context, p product.Product) (product.Product, error) {
	r, err := s.q.CreateProduct(ctx, dbgen.CreateProductParams{
		Code: p.Code, Name: p.Name, Unit: p.Unit, UnitPrice: p.UnitPrice,
		TaxPercent: int32(p.TaxPercent), Active: p.Active, //nolint:gosec // 0-100 doğrulandı
	})
	if isUniqueViolation(err) {
		return product.Product{}, product.ErrDuplicateCode
	}
	if err != nil {
		return product.Product{}, err
	}
	return toProduct(r), nil
}

func (s *ProductStore) Update(ctx context.Context, p product.Product) error {
	n, err := s.q.UpdateProduct(ctx, dbgen.UpdateProductParams{
		ID: p.ID, Code: p.Code, Name: p.Name, Unit: p.Unit, UnitPrice: p.UnitPrice,
		TaxPercent: int32(p.TaxPercent), Active: p.Active, //nolint:gosec // 0-100 doğrulandı
	})
	if isUniqueViolation(err) {
		return product.ErrDuplicateCode
	}
	if err != nil {
		return err
	}
	if n == 0 {
		return product.ErrNotFound
	}
	return nil
}
