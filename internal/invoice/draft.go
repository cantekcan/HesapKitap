package invoice

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"hesapkitap/internal/product"
)

// Status is the local invoice state. Provider statuses (ProviderStatus) are tracked separately (Faz 5).
type Status string

const (
	StatusDraft    Status = "DRAFT"
	StatusSending  Status = "SENDING"   // gönderim kuyrukta veya sonucu belirsiz
	StatusIssued   Status = "ISSUED"    // sağlayıcı faturayı aldı (ayrıntı: ProviderStatus)
	StatusErrored  Status = "FAILED"    // sağlayıcıda hatalı/reddedildi
	StatusAnnulled Status = "CANCELLED" // iptal edildi
)

func (s Status) Label() string {
	switch s {
	case StatusDraft:
		return "Taslak"
	case StatusSending:
		return "Gönderiliyor"
	case StatusIssued:
		return "Kesildi"
	case StatusErrored:
		return "Hatalı"
	case StatusAnnulled:
		return "İptal"
	}
	return string(s)
}

// Line is an invoice line. ItemName, Unit, UnitPrice and TaxPercent are COPIED from the product when it is chosen,
// so later product edits never change an invoice (docs/ROADMAP.md Faz 4).
type Line struct {
	ProductID  *uuid.UUID
	ItemName   string
	Unit       string
	Quantity   decimal.Decimal
	UnitPrice  int64 // kuruş, KDV hariç
	Discount   int64 // kuruş
	TaxPercent int
}

// Input converts the line to calculator input.
func (l Line) Input() LineInput {
	return LineInput{Quantity: l.Quantity, UnitPrice: l.UnitPrice, Discount: l.Discount, TaxPercent: l.TaxPercent}
}

// Draft is an editable invoice. Only drafts can be edited or deleted (ALTIN KURAL 6).
type Draft struct {
	ID           uuid.UUID
	Status       Status
	CustomerID   uuid.UUID
	CustomerName string // yalnızca okurken dolar
	IssueDate    time.Time
	DueDate      *time.Time
	Notes        string
	Lines        []Line
	Payable      int64 // kuruş; kayıtta yeniden hesaplanır

	// Gönderim bilgileri (taslakta boş). ETTN gönderimden önce üretilip yazılır.
	Kind           Kind
	Scenario       string
	ETTN           uuid.UUID
	ProviderStatus *ProviderStatus
	Number         string
	TargetAlias    string
	Recipient      *Recipient
	LastError      string
}

// Summary is a row of the invoice list.
type Summary struct {
	ID           uuid.UUID
	Status       Status
	CustomerID   uuid.UUID
	CustomerName string
	IssueDate    time.Time
	DueDate      *time.Time
	Payable      int64
}

// FieldErrors maps a form field (or "lines", "line_N") to a Turkish message.
type FieldErrors map[string]string

// Validate checks the draft. Amount rules (discount ≤ gross, …) come from the calculator.
func (d Draft) Validate() FieldErrors {
	errs := FieldErrors{}
	if d.CustomerID == uuid.Nil {
		errs["customer_id"] = "Müşteri seçilmeli."
	}
	if d.IssueDate.IsZero() {
		errs["issue_date"] = "Fatura tarihi girilmeli."
	}
	if d.DueDate != nil && !d.IssueDate.IsZero() && d.DueDate.Before(d.IssueDate) {
		errs["due_date"] = "Vade tarihi fatura tarihinden önce olamaz."
	}
	if utf8.RuneCountInString(d.Notes) > 1000 {
		errs["notes"] = "Not en fazla 1000 karakter olabilir."
	}
	if len(d.Lines) == 0 {
		errs["lines"] = "En az bir kalem ekleyin."
		return errs
	}
	for i, l := range d.Lines {
		key := fmt.Sprintf("line_%d", i)
		switch {
		case utf8.RuneCountInString(l.ItemName) < 2 || utf8.RuneCountInString(l.ItemName) > 200:
			errs[key] = fmt.Sprintf("%d. kalem: ürün/hizmet adı 2–200 karakter olmalı.", i+1)
		case !product.ValidUnit(l.Unit):
			errs[key] = fmt.Sprintf("%d. kalem: birim geçersiz.", i+1)
		case !product.ValidTaxPercent(l.TaxPercent):
			errs[key] = fmt.Sprintf("%d. kalem: KDV oranı geçersiz.", i+1)
		case !l.Quantity.Mul(decimal.NewFromInt(1000)).IsInteger():
			errs[key] = fmt.Sprintf("%d. kalem: miktar en fazla 3 ondalık içerebilir.", i+1)
		default:
			if _, err := (InvoiceCalculator{}).Line(l.Input()); err != nil {
				errs[key] = fmt.Sprintf("%d. kalem: %s.", i+1, err.Error())
			}
		}
	}
	return errs
}

// Totals computes the invoice amounts with the shared calculator.
func (d Draft) Totals() (Totals, error) {
	in := make([]LineInput, len(d.Lines))
	for i, l := range d.Lines {
		in[i] = l.Input()
	}
	return InvoiceCalculator{}.Calculate(in)
}

// ErrNotEditable is returned when the invoice does not exist or is no longer a draft.
var ErrNotEditable = errors.New("fatura bulunamadı veya artık değiştirilemez")

// ErrNotFound is returned by Get.
var ErrNotFound = errors.New("fatura bulunamadı")

// ListFilter selects a page of invoices. Status is "" or a Status value.
type ListFilter struct {
	Query  string // müşteri adı
	Status string
	Limit  int
	Offset int
}

// Store is the persistence port, implemented in internal/platform/db.
type Store interface {
	Get(ctx context.Context, id uuid.UUID) (Draft, error)
	List(ctx context.Context, f ListFilter) ([]Summary, int, error)
	Create(ctx context.Context, d Draft) (uuid.UUID, error)
	Update(ctx context.Context, d Draft) error      // ErrNotEditable
	Delete(ctx context.Context, id uuid.UUID) error // ErrNotEditable
}

// Service holds the invoice use cases. Provider, Recipients and Issues are only needed for issuing (Faz 5).
type Service struct {
	Store      Store
	Provider   EInvoiceProvider
	Recipients RecipientSource
	Issues     IssueStore
	Settings   Settings
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (Draft, error) { return s.Store.Get(ctx, id) }
func (s *Service) List(ctx context.Context, f ListFilter) ([]Summary, int, error) {
	return s.Store.List(ctx, f)
}
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error { return s.Store.Delete(ctx, id) }

// Save validates the draft and creates it (zero ID) or updates it. Validation problems come back as FieldErrors.
func (s *Service) Save(ctx context.Context, d Draft) (uuid.UUID, FieldErrors, error) {
	if errs := d.Validate(); len(errs) > 0 {
		return uuid.Nil, errs, nil
	}
	t, err := d.Totals()
	if err != nil {
		return uuid.Nil, FieldErrors{"lines": err.Error() + "."}, nil //nolint:nilerr // doğrulama hatası kullanıcıya alan hatası olarak döner
	}
	d.Payable = t.Payable
	d.Status = StatusDraft
	if d.ID == uuid.Nil {
		id, err := s.Store.Create(ctx, d)
		return id, nil, err
	}
	return d.ID, nil, s.Store.Update(ctx, d)
}
