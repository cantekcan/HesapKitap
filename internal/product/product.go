// Package product is the product/service catalogue domain.
package product

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Unit is a UN/ECE unit code as the e-invoice API expects it (ALTIN KURAL 9: sabit ASCII).
type Unit struct {
	Code  string
	Label string
}

// Units are the supported units; the first one is the default (C62 = adet).
var Units = []Unit{
	{"C62", "Adet"}, {"KGM", "Kg"}, {"LTR", "Litre"}, {"MTR", "Metre"}, {"MTK", "m²"}, {"MTQ", "m³"},
	{"HUR", "Saat"}, {"DAY", "Gün"}, {"MON", "Ay"}, {"PA", "Paket"}, {"BX", "Kutu"}, {"SET", "Set"},
	{"DZN", "Düzine"}, {"TNE", "Ton"},
}

// ValidUnit reports whether code is a supported unit code.
func ValidUnit(code string) bool {
	for _, u := range Units {
		if u.Code == code {
			return true
		}
	}
	return false
}

// UnitLabel returns the Turkish label of a unit code (the code itself if unknown).
func UnitLabel(code string) string {
	for _, u := range Units {
		if u.Code == code {
			return u.Label
		}
	}
	return code
}

// TaxPercents are the KDV rates the UI offers. %0 needs an exemption reason (MVP dışı) so it is not offered.
var TaxPercents = []int{20, 10, 1}

// ValidTaxPercent reports whether p is an offered KDV rate.
func ValidTaxPercent(p int) bool {
	for _, v := range TaxPercents {
		if v == p {
			return true
		}
	}
	return false
}

// Product is a catalogue entry. UnitPrice is in kuruş, KDV hariç.
type Product struct {
	ID         uuid.UUID
	Code       string
	Name       string
	Unit       string
	UnitPrice  int64
	TaxPercent int
	Active     bool
}

// FieldErrors maps a form field name to a Turkish message.
type FieldErrors map[string]string

// Normalize trims input.
func (p *Product) Normalize() {
	p.Code = strings.TrimSpace(p.Code)
	p.Name = strings.TrimSpace(p.Name)
}

// Validate checks the product. Call Normalize first.
func (p Product) Validate() FieldErrors {
	errs := FieldErrors{}
	if n := utf8.RuneCountInString(p.Name); n < 2 || n > 200 {
		errs["name"] = "Ürün adı 2 ile 200 karakter arasında olmalı."
	}
	if utf8.RuneCountInString(p.Code) > 50 {
		errs["code"] = "Kod en fazla 50 karakter olabilir."
	}
	if !ValidUnit(p.Unit) {
		errs["unit"] = "Birim seçilmeli."
	}
	if p.UnitPrice < 0 {
		errs["unit_price"] = "Fiyat eksi olamaz."
	}
	if !ValidTaxPercent(p.TaxPercent) {
		errs["tax_percent"] = "KDV oranı seçilmeli."
	}
	return errs
}

var (
	ErrNotFound      = errors.New("ürün bulunamadı")
	ErrDuplicateCode = errors.New("bu kodla kayıtlı başka bir ürün var")
)

// ListFilter selects a page of products. Status is "", "active" or "passive".
type ListFilter struct {
	Query  string
	Status string
	Limit  int
	Offset int
}

// Store is the persistence port, implemented in internal/platform/db.
type Store interface {
	Get(ctx context.Context, id uuid.UUID) (Product, error)
	List(ctx context.Context, f ListFilter) ([]Product, int, error)
	ListActive(ctx context.Context) ([]Product, error)
	Create(ctx context.Context, p Product) (Product, error) // ErrDuplicateCode
	Update(ctx context.Context, p Product) error            // ErrNotFound, ErrDuplicateCode
}

// Service holds the product use cases.
type Service struct{ Store Store }

func (s *Service) Get(ctx context.Context, id uuid.UUID) (Product, error) {
	return s.Store.Get(ctx, id)
}
func (s *Service) List(ctx context.Context, f ListFilter) ([]Product, int, error) {
	return s.Store.List(ctx, f)
}
func (s *Service) ListActive(ctx context.Context) ([]Product, error) { return s.Store.ListActive(ctx) }

// Create validates and stores a product. Validation problems come back as FieldErrors.
func (s *Service) Create(ctx context.Context, p Product) (Product, FieldErrors, error) {
	p.Normalize()
	if errs := p.Validate(); len(errs) > 0 {
		return Product{}, errs, nil
	}
	created, err := s.Store.Create(ctx, p)
	if errors.Is(err, ErrDuplicateCode) {
		return Product{}, FieldErrors{"code": ErrDuplicateCode.Error() + "."}, nil
	}
	return created, nil, err
}

// Update validates and saves a product.
func (s *Service) Update(ctx context.Context, p Product) (FieldErrors, error) {
	p.Normalize()
	if errs := p.Validate(); len(errs) > 0 {
		return errs, nil
	}
	err := s.Store.Update(ctx, p)
	if errors.Is(err, ErrDuplicateCode) {
		return FieldErrors{"code": ErrDuplicateCode.Error() + "."}, nil
	}
	return nil, err
}
