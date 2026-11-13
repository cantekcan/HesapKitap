// Package payment is the payment (tahsilat) domain.
package payment

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Method is how the customer paid.
type Method string

const (
	MethodCash     Method = "CASH"
	MethodTransfer Method = "TRANSFER"
	MethodCard     Method = "CARD"
	MethodCheque   Method = "CHEQUE"
	MethodNote     Method = "NOTE"
	MethodOther    Method = "OTHER"
)

// Methods lists the payment methods in display order.
var Methods = []Method{MethodCash, MethodTransfer, MethodCard, MethodCheque, MethodNote, MethodOther}

// Label is the Turkish display text.
func (m Method) Label() string {
	switch m {
	case MethodCash:
		return "Nakit"
	case MethodTransfer:
		return "Havale/EFT"
	case MethodCard:
		return "Kredi kartı"
	case MethodCheque:
		return "Çek"
	case MethodNote:
		return "Senet"
	case MethodOther:
		return "Diğer"
	}
	return string(m)
}

// Valid reports whether m is a known method.
func (m Method) Valid() bool {
	for _, v := range Methods {
		if v == m {
			return true
		}
	}
	return false
}

// HasTerm reports whether the method is a cheque or note, which carries a due date (vade).
func (m Method) HasTerm() bool { return m == MethodCheque || m == MethodNote }

// ChequeInfo describes the cheque/note received. Due is the date it can be cashed.
type ChequeInfo struct {
	Number string
	Bank   string
	Drawer string // keşideci
	Due    time.Time
}

// Payment is money received from a customer. Amount is kuruş and always positive.
type Payment struct {
	ID           uuid.UUID
	CustomerID   uuid.UUID
	CustomerName string // yalnızca okurken dolar
	Date         time.Time
	Amount       int64
	Method       Method
	Note         string
	InvoiceID    *uuid.UUID  // isteğe bağlı: bu faturaya mahsup
	Cheque       *ChequeInfo // yalnızca çek/senet tahsilatında
}

// FieldErrors maps a form field name to a Turkish message.
type FieldErrors map[string]string

// Validate checks the payment. today is used to refuse dates far in the future.
func (p Payment) Validate(today time.Time) FieldErrors {
	errs := FieldErrors{}
	if p.CustomerID == uuid.Nil {
		errs["customer_id"] = "Müşteri seçilmeli."
	}
	if p.Date.IsZero() {
		errs["paid_at"] = "Tahsilat tarihi girilmeli."
	} else if p.Date.After(today.AddDate(0, 0, 1)) {
		errs["paid_at"] = "Tahsilat tarihi gelecekte olamaz."
	}
	if p.Amount <= 0 {
		errs["amount"] = "Tutar sıfırdan büyük olmalı."
	}
	if !p.Method.Valid() {
		errs["method"] = "Ödeme yöntemi seçilmeli."
	}
	if p.Method.HasTerm() {
		switch {
		case p.Cheque == nil || p.Cheque.Due.IsZero():
			errs["cheque_due"] = "Çek/senet için vade tarihi girilmeli."
		case !p.Date.IsZero() && p.Cheque.Due.Before(p.Date):
			errs["cheque_due"] = "Vade, tahsilat tarihinden önce olamaz."
		case p.Cheque.Due.After(p.Date.AddDate(5, 0, 0)):
			errs["cheque_due"] = "Vade en fazla 5 yıl sonra olabilir."
		}
		if p.Cheque != nil && (utf8.RuneCountInString(p.Cheque.Number) > 40 || utf8.RuneCountInString(p.Cheque.Bank) > 80 || utf8.RuneCountInString(p.Cheque.Drawer) > 120) {
			errs["cheque_no"] = "Çek no, banka veya keşideci çok uzun."
		}
	}
	if utf8.RuneCountInString(strings.TrimSpace(p.Note)) > 500 {
		errs["note"] = "Açıklama en fazla 500 karakter olabilir."
	}
	return errs
}

// Errors.
var (
	ErrNotFound = errors.New("tahsilat bulunamadı")
	// ErrInUse: tahsilattaki çek ciro edildiği için silinemez; önce tedarikçi ödemesi silinmeli.
	ErrInUse = errors.New("bu tahsilatın çeki bir tedarikçiye ciro edilmiş; önce tedarikçideki ödemeyi silin")
)

// ListFilter selects a page of payments. From/To are inclusive dates (zero = open).
type ListFilter struct {
	Query  string
	From   time.Time
	To     time.Time
	Limit  int
	Offset int
}

// Store is the persistence port, implemented in internal/platform/db.
type Store interface {
	Create(ctx context.Context, p Payment) (uuid.UUID, error)
	Delete(ctx context.Context, id uuid.UUID) error
	Get(ctx context.Context, id uuid.UUID) (Payment, error)
	List(ctx context.Context, f ListFilter) ([]Payment, int, error)
	Total(ctx context.Context, from, to time.Time) (int64, error)
}

// Service holds the payment use cases.
type Service struct {
	Store Store
	Now   func() time.Time
}

func (s *Service) today() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Create validates and stores a payment. Validation problems come back as FieldErrors.
func (s *Service) Create(ctx context.Context, p Payment) (uuid.UUID, FieldErrors, error) {
	p.Note = strings.TrimSpace(p.Note)
	if p.Cheque != nil {
		p.Cheque.Number, p.Cheque.Bank, p.Cheque.Drawer = strings.TrimSpace(p.Cheque.Number), strings.TrimSpace(p.Cheque.Bank), strings.TrimSpace(p.Cheque.Drawer)
	}
	if errs := p.Validate(s.today()); len(errs) > 0 {
		return uuid.Nil, errs, nil
	}
	if !p.Method.HasTerm() {
		p.Cheque = nil
	}
	id, err := s.Store.Create(ctx, p)
	return id, nil, err
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error { return s.Store.Delete(ctx, id) }
func (s *Service) Get(ctx context.Context, id uuid.UUID) (Payment, error) {
	return s.Store.Get(ctx, id)
}
func (s *Service) List(ctx context.Context, f ListFilter) ([]Payment, int, error) {
	return s.Store.List(ctx, f)
}
func (s *Service) Total(ctx context.Context, from, to time.Time) (int64, error) {
	return s.Store.Total(ctx, from, to)
}
