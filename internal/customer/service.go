package customer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"hesapkitap/internal/invoice"
)

var (
	ErrNotFound       = errors.New("müşteri bulunamadı")
	ErrDuplicateTaxID = errors.New("bu VKN/TCKN ile kayıtlı başka bir müşteri var")
	ErrNoProvider     = errors.New("e-Fatura sağlayıcısı ayarlı değil")
	ErrInvalidTaxID   = errors.New("geçerli bir VKN (10 hane) veya TCKN (11 hane) girin")
)

// ListFilter selects a page of customers. Status is "", "active" or "passive".
type ListFilter struct {
	Query  string
	Status string
	Limit  int
	Offset int
}

// Store is the persistence port, implemented in internal/platform/db.
type Store interface {
	Get(ctx context.Context, id uuid.UUID) (Customer, error)
	List(ctx context.Context, f ListFilter) (items []Customer, total int, err error)
	Create(ctx context.Context, c Customer) (Customer, error)
	Update(ctx context.Context, c Customer) error // ErrNotFound, ErrDuplicateTaxID
	SaveTaxpayer(ctx context.Context, id uuid.UUID, isEInvoiceUser bool, alias string, checkedAt time.Time) error
}

// Service holds the customer use cases. Provider may be nil (taxpayer lookup disabled).
type Service struct {
	Store    Store
	Provider invoice.EInvoiceProvider
	Now      func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (Customer, error) {
	return s.Store.Get(ctx, id)
}

func (s *Service) List(ctx context.Context, f ListFilter) ([]Customer, int, error) {
	return s.Store.List(ctx, f)
}

// Create validates and stores a new customer. Validation problems are returned as FieldErrors, not error.
func (s *Service) Create(ctx context.Context, c Customer) (Customer, FieldErrors, error) {
	c.Normalize()
	if errs := c.Validate(); len(errs) > 0 {
		return Customer{}, errs, nil
	}
	created, err := s.Store.Create(ctx, c)
	if errors.Is(err, ErrDuplicateTaxID) {
		return Customer{}, FieldErrors{"tax_id": ErrDuplicateTaxID.Error() + "."}, nil
	}
	return created, nil, err
}

// Update validates and saves the card. If the tax id changed, the stored e-Fatura lookup is stale and is cleared.
func (s *Service) Update(ctx context.Context, c Customer) (FieldErrors, error) {
	c.Normalize()
	if errs := c.Validate(); len(errs) > 0 {
		return errs, nil
	}
	old, err := s.Store.Get(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	err = s.Store.Update(ctx, c)
	if errors.Is(err, ErrDuplicateTaxID) {
		return FieldErrors{"tax_id": ErrDuplicateTaxID.Error() + "."}, nil
	}
	if err != nil {
		return nil, err
	}
	if old.TaxID != c.TaxID && old.TaxpayerCheckedAt != nil {
		return nil, s.Store.SaveTaxpayer(ctx, c.ID, false, "", time.Time{})
	}
	return nil, nil
}

// TaxpayerResult is the outcome of an e-Fatura mükellef lookup.
type TaxpayerResult struct {
	IsEInvoiceUser bool
	Aliases        []invoice.TaxpayerAlias
	// NeedsChoice is true when several aliases exist and none has been saved yet.
	NeedsChoice bool
}

// LookupTaxpayer queries the provider. With zero aliases the customer is saved as e-Arşiv; with one it is saved
// directly; with several the caller must pick one via SelectAlias.
func (s *Service) LookupTaxpayer(ctx context.Context, id uuid.UUID) (TaxpayerResult, error) {
	if s.Provider == nil {
		return TaxpayerResult{}, ErrNoProvider
	}
	c, err := s.Store.Get(ctx, id)
	if err != nil {
		return TaxpayerResult{}, err
	}
	aliases, err := s.Provider.LookupTaxpayer(ctx, c.TaxID)
	if err != nil {
		return TaxpayerResult{}, fmt.Errorf("mükellef sorgusu: %w", err)
	}
	res := TaxpayerResult{IsEInvoiceUser: len(aliases) > 0, Aliases: aliases}
	switch len(aliases) {
	case 0:
		err = s.Store.SaveTaxpayer(ctx, id, false, "", s.now())
	case 1:
		err = s.Store.SaveTaxpayer(ctx, id, true, aliases[0].Alias, s.now())
	default:
		res.NeedsChoice = true
	}
	return res, err
}

// SelectAlias saves the alias the user picked among several.
func (s *Service) SelectAlias(ctx context.Context, id uuid.UUID, alias string) error {
	if alias == "" {
		return errors.New("etiket seçilmedi")
	}
	return s.Store.SaveTaxpayer(ctx, id, true, alias, s.now())
}

// Preview looks up a VKN/TCKN at the provider without a saved customer (new-customer form).
// Only e-Fatura taxpayers are known to the provider; for others the result is empty.
func (s *Service) Preview(ctx context.Context, taxID string) ([]invoice.TaxpayerAlias, error) {
	if s.Provider == nil {
		return nil, ErrNoProvider
	}
	if !ValidTaxID(taxID) {
		return nil, ErrInvalidTaxID
	}
	aliases, err := s.Provider.LookupTaxpayer(ctx, taxID)
	if err != nil {
		return nil, fmt.Errorf("mükellef sorgusu: %w", err)
	}
	return aliases, nil
}

// taxpayerMaxAge is how old a stored e-Fatura lookup may be before it is refreshed at issue time.
const taxpayerMaxAge = 7 * 24 * time.Hour

// ResolveRecipient implements invoice.RecipientSource: the customer data to freeze on the invoice and the
// customer's e-Fatura registration, refreshed when the stored lookup is missing or older than 7 days.
func (s *Service) ResolveRecipient(ctx context.Context, id uuid.UUID) (invoice.Recipient, invoice.Taxpayer, error) {
	c, err := s.Store.Get(ctx, id)
	if err != nil {
		return invoice.Recipient{}, invoice.Taxpayer{}, err
	}
	tp := invoice.Taxpayer{IsEInvoiceUser: c.IsEInvoiceUser, Alias: c.EInvoiceAlias}
	if s.Provider != nil && (c.TaxpayerCheckedAt == nil || s.now().Sub(*c.TaxpayerCheckedAt) > taxpayerMaxAge) {
		res, err := s.LookupTaxpayer(ctx, id)
		if err != nil {
			return invoice.Recipient{}, invoice.Taxpayer{}, err
		}
		tp = invoice.Taxpayer{IsEInvoiceUser: res.IsEInvoiceUser}
		if !res.NeedsChoice && len(res.Aliases) == 1 {
			tp.Alias = res.Aliases[0].Alias
		}
	}
	return invoice.Recipient{
		Person: c.Kind == KindPerson, TaxID: c.TaxID, Name: c.Name, TaxOffice: c.TaxOffice, Address: c.Address,
		District: c.District, City: c.City, PostalCode: c.PostalCode, Email: c.Email, Phone: c.Phone,
	}, tp, nil
}
