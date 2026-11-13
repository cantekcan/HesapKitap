// Package supplier tracks what we owe to our suppliers (toptancılar): purchases entered by hand, payments we made
// (cash, transfer, our own cheques, or customer cheques endorsed to them) and the resulting balance.
//
// Kural: bakiye = açılış + Σ alımlar − Σ ödemeler. Pozitif bakiye "tedarikçiye borcumuz var" demektir.
// Verdiğimiz çek, verildiği gün borcu düşer; vadesi gelene kadar "ödenecek çek" olarak ayrıca görünür.
package supplier

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Supplier is a supplier card. Balance and PendingCheques are filled by the store when reading.
type Supplier struct {
	ID             uuid.UUID
	Name           string
	TaxID          string
	Phone          string
	Note           string
	Opening        int64 // kuruş; + borcumuz var
	Active         bool
	Balance        int64 // güncel borcumuz
	PendingCheques int64 // henüz hesabımızdan çıkmamış kendi çeklerimiz
}

// Entry is a purchase / debt record (mal alımı).
type Entry struct {
	ID          uuid.UUID
	SupplierID  uuid.UUID
	Date        time.Time
	Amount      int64 // kuruş, pozitif
	DocNo       string
	Description string
	Due         *time.Time
	Paid        int64 // bu alıma bağlanan ödemeler (yalnızca okurken dolar)
}

// Method is how we paid the supplier.
type Method string

const (
	MethodCash     Method = "CASH"
	MethodTransfer Method = "TRANSFER"
	MethodCard     Method = "CARD"
	MethodCheque   Method = "CHEQUE"   // kendi çekimiz
	MethodEndorsed Method = "ENDORSED" // müşteriden aldığımız çeki ciro ettik
	MethodNote     Method = "NOTE"
	MethodOther    Method = "OTHER"
)

// Methods lists the methods the payment form offers (ENDORSED has its own form).
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
		return "Kendi çekimiz"
	case MethodEndorsed:
		return "Ciro edilen çek"
	case MethodNote:
		return "Senet"
	case MethodOther:
		return "Diğer"
	}
	return string(m)
}

// Valid reports whether m can be chosen on the payment form.
func (m Method) Valid() bool {
	for _, v := range Methods {
		if v == m {
			return true
		}
	}
	return false
}

// Payment is money (or a cheque) we gave to a supplier.
type Payment struct {
	ID           uuid.UUID
	SupplierID   uuid.UUID
	Date         time.Time
	Amount       int64
	Method       Method
	ChequeDue    *time.Time // kendi çekimizin vadesi
	ChequeNo     string
	Bank         string
	Cleared      bool       // kendi çekimiz hesabımızdan çıktı
	FromChequeID *uuid.UUID // ciro edilen müşteri çeki
	EntryID      *uuid.UUID // hangi alıma karşılık
	Note         string
}

// OwnCheque is one of our own cheques given to a supplier.
type OwnCheque struct {
	PaymentID    uuid.UUID
	SupplierID   uuid.UUID
	SupplierName string
	Date         time.Time
	Amount       int64
	Due          time.Time
	Number       string
	Bank         string
	Cleared      bool
}

// Due is a future cash movement.
type Due struct {
	Date   time.Time
	Amount int64
}

// FieldErrors maps a form field name to a Turkish message.
type FieldErrors map[string]string

// Errors.
var (
	ErrNotFound          = errors.New("kayıt bulunamadı")
	ErrChequeUnavailable = errors.New("bu çek artık portföyde değil; ciro edilemez")
	ErrDuplicateName     = errors.New("bu adla bir tedarikçi zaten var")
)

// Validate checks the supplier card.
func (s Supplier) Validate() FieldErrors {
	errs := FieldErrors{}
	if n := utf8.RuneCountInString(strings.TrimSpace(s.Name)); n == 0 {
		errs["name"] = "Tedarikçi adı girilmeli."
	} else if n > 120 {
		errs["name"] = "Ad en fazla 120 karakter olabilir."
	}
	if utf8.RuneCountInString(s.Note) > 500 {
		errs["note"] = "Not en fazla 500 karakter olabilir."
	}
	if t := strings.TrimSpace(s.TaxID); t != "" && (len(t) != 10 && len(t) != 11 || strings.Trim(t, "0123456789") != "") {
		errs["tax_id"] = "VKN 10, TCKN 11 haneli olmalı (boş bırakılabilir)."
	}
	return errs
}

// Validate checks a purchase entry.
func (e Entry) Validate(today time.Time) FieldErrors {
	errs := FieldErrors{}
	if e.Date.IsZero() {
		errs["entry_date"] = "Tarih girilmeli."
	} else if e.Date.After(today.AddDate(0, 0, 1)) {
		errs["entry_date"] = "Tarih gelecekte olamaz."
	}
	if e.Amount <= 0 {
		errs["amount"] = "Tutar sıfırdan büyük olmalı."
	}
	if e.Due != nil && !e.Date.IsZero() && e.Due.Before(e.Date) {
		errs["due_date"] = "Vade, alım tarihinden önce olamaz."
	}
	if utf8.RuneCountInString(e.Description) > 500 || utf8.RuneCountInString(e.DocNo) > 60 {
		errs["description"] = "Açıklama en fazla 500, belge no en fazla 60 karakter olabilir."
	}
	return errs
}

// Validate checks a payment (not an endorsement; see Service.Endorse).
func (p Payment) Validate(today time.Time) FieldErrors {
	errs := FieldErrors{}
	if p.Date.IsZero() {
		errs["paid_at"] = "Ödeme tarihi girilmeli."
	} else if p.Date.After(today.AddDate(0, 0, 1)) {
		errs["paid_at"] = "Ödeme tarihi gelecekte olamaz."
	}
	if p.Amount <= 0 {
		errs["amount"] = "Tutar sıfırdan büyük olmalı."
	}
	if !p.Method.Valid() {
		errs["method"] = "Ödeme yöntemi seçilmeli."
	}
	if p.Method == MethodCheque || p.Method == MethodNote {
		switch {
		case p.ChequeDue == nil:
			errs["cheque_due"] = "Vade tarihi girilmeli."
		case !p.Date.IsZero() && p.ChequeDue.Before(p.Date):
			errs["cheque_due"] = "Vade, ödeme tarihinden önce olamaz."
		case p.ChequeDue.After(p.Date.AddDate(5, 0, 0)):
			errs["cheque_due"] = "Vade en fazla 5 yıl sonra olabilir."
		}
	}
	if utf8.RuneCountInString(p.Note) > 500 {
		errs["note"] = "Açıklama en fazla 500 karakter olabilir."
	}
	return errs
}

// Store is the persistence port, implemented in internal/platform/db.
type Store interface {
	CreateSupplier(ctx context.Context, s Supplier) (uuid.UUID, error)
	UpdateSupplier(ctx context.Context, s Supplier) error
	List(ctx context.Context, query string) ([]Supplier, error)
	Get(ctx context.Context, id uuid.UUID) (Supplier, error)

	CreateEntry(ctx context.Context, e Entry) (uuid.UUID, error)
	DeleteEntry(ctx context.Context, id uuid.UUID) error
	Entries(ctx context.Context, supplierID uuid.UUID) ([]Entry, error)

	CreatePayment(ctx context.Context, p Payment) (uuid.UUID, error)
	// Endorse gives a portfolio cheque to the supplier: the cheque becomes ENDORSED and a payment of its amount is
	// recorded, atomically. ErrChequeUnavailable when the cheque is not in the portfolio.
	Endorse(ctx context.Context, supplierID, chequeID uuid.UUID, date time.Time, entryID *uuid.UUID, note string) (uuid.UUID, error)
	// DeletePayment removes the payment; an endorsed cheque goes back to the portfolio.
	DeletePayment(ctx context.Context, id uuid.UUID) error
	Payments(ctx context.Context, supplierID uuid.UUID) ([]Payment, error)

	SetCleared(ctx context.Context, paymentID uuid.UUID, cleared bool) error
	OwnCheques(ctx context.Context) ([]OwnCheque, error)
	PendingOwnDues(ctx context.Context) ([]Due, error)
	DebtTotal(ctx context.Context) (int64, error)
}

// Service holds the supplier use cases.
type Service struct {
	Store Store
	Now   func() time.Time
}

func (s *Service) today() time.Time {
	n := time.Now()
	if s.Now != nil {
		n = s.Now()
	}
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

// CreateSupplier validates and stores a supplier.
func (s *Service) CreateSupplier(ctx context.Context, sp Supplier) (uuid.UUID, FieldErrors, error) {
	sp.Name, sp.TaxID, sp.Phone, sp.Note = strings.TrimSpace(sp.Name), strings.TrimSpace(sp.TaxID), strings.TrimSpace(sp.Phone), strings.TrimSpace(sp.Note)
	if errs := sp.Validate(); len(errs) > 0 {
		return uuid.Nil, errs, nil
	}
	id, err := s.Store.CreateSupplier(ctx, sp)
	if errors.Is(err, ErrDuplicateName) {
		return uuid.Nil, FieldErrors{"name": err.Error() + "."}, nil
	}
	return id, nil, err
}

// UpdateSupplier validates and updates a supplier.
func (s *Service) UpdateSupplier(ctx context.Context, sp Supplier) (FieldErrors, error) {
	sp.Name, sp.TaxID, sp.Phone, sp.Note = strings.TrimSpace(sp.Name), strings.TrimSpace(sp.TaxID), strings.TrimSpace(sp.Phone), strings.TrimSpace(sp.Note)
	if errs := sp.Validate(); len(errs) > 0 {
		return errs, nil
	}
	err := s.Store.UpdateSupplier(ctx, sp)
	if errors.Is(err, ErrDuplicateName) {
		return FieldErrors{"name": err.Error() + "."}, nil
	}
	return nil, err
}

func (s *Service) List(ctx context.Context, q string) ([]Supplier, error) {
	return s.Store.List(ctx, strings.TrimSpace(q))
}
func (s *Service) Get(ctx context.Context, id uuid.UUID) (Supplier, error) {
	return s.Store.Get(ctx, id)
}
func (s *Service) Entries(ctx context.Context, id uuid.UUID) ([]Entry, error) {
	return s.Store.Entries(ctx, id)
}
func (s *Service) Payments(ctx context.Context, id uuid.UUID) ([]Payment, error) {
	return s.Store.Payments(ctx, id)
}
func (s *Service) OwnCheques(ctx context.Context) ([]OwnCheque, error) {
	return s.Store.OwnCheques(ctx)
}
func (s *Service) PendingOwnDues(ctx context.Context) ([]Due, error) {
	return s.Store.PendingOwnDues(ctx)
}
func (s *Service) DebtTotal(ctx context.Context) (int64, error) { return s.Store.DebtTotal(ctx) }
func (s *Service) DeleteEntry(ctx context.Context, id uuid.UUID) error {
	return s.Store.DeleteEntry(ctx, id)
}
func (s *Service) DeletePayment(ctx context.Context, id uuid.UUID) error {
	return s.Store.DeletePayment(ctx, id)
}
func (s *Service) SetCleared(ctx context.Context, id uuid.UUID, cleared bool) error {
	return s.Store.SetCleared(ctx, id, cleared)
}

// AddEntry validates and stores a purchase.
func (s *Service) AddEntry(ctx context.Context, e Entry) (uuid.UUID, FieldErrors, error) {
	e.DocNo, e.Description = strings.TrimSpace(e.DocNo), strings.TrimSpace(e.Description)
	if errs := e.Validate(s.today()); len(errs) > 0 {
		return uuid.Nil, errs, nil
	}
	id, err := s.Store.CreateEntry(ctx, e)
	return id, nil, err
}

// AddPayment validates and stores a payment (not an endorsement).
func (s *Service) AddPayment(ctx context.Context, p Payment) (uuid.UUID, FieldErrors, error) {
	p.Note, p.ChequeNo, p.Bank = strings.TrimSpace(p.Note), strings.TrimSpace(p.ChequeNo), strings.TrimSpace(p.Bank)
	if errs := p.Validate(s.today()); len(errs) > 0 {
		return uuid.Nil, errs, nil
	}
	if p.Method != MethodCheque && p.Method != MethodNote {
		p.ChequeDue, p.ChequeNo, p.Bank = nil, "", ""
	}
	id, err := s.Store.CreatePayment(ctx, p)
	return id, nil, err
}

// Endorse gives a customer's cheque to the supplier as a payment.
func (s *Service) Endorse(ctx context.Context, supplierID, chequeID uuid.UUID, date time.Time, entryID *uuid.UUID, note string) (uuid.UUID, error) {
	if date.IsZero() || date.After(s.today().AddDate(0, 0, 1)) {
		date = s.today()
	}
	return s.Store.Endorse(ctx, supplierID, chequeID, date, entryID, strings.TrimSpace(note))
}

// Row is one line of the supplier statement.
type Row struct {
	Date    time.Time
	Payment bool // false: alım
	ID      uuid.UUID
	Ref     string
	Note    string
	Debit   int64 // borcumuzu artıran (alım)
	Credit  int64 // borcumuzu azaltan (ödeme)
	Balance int64

	Method    Method     // yalnızca ödeme satırında
	ChequeDue *time.Time // kendi çekimizin vadesi
	Cleared   bool       // kendi çekimiz hesabımızdan çıktı
}

// Statement lists the movements by date with a running balance (opening first when non-zero). Same day: purchase before payment.
func Statement(opening int64, entries []Entry, payments []Payment) []Row {
	type mv struct {
		row   Row
		order int
	}
	var ms []mv
	for _, e := range entries {
		ref := e.DocNo
		if ref == "" {
			ref = "Alım"
		}
		ms = append(ms, mv{Row{Date: e.Date, ID: e.ID, Ref: ref, Note: e.Description, Debit: e.Amount}, 1})
	}
	for _, p := range payments {
		note := p.Note
		if p.ChequeDue != nil {
			if note != "" {
				note += " · "
			}
			note += "vade " + p.ChequeDue.Format("02.01.2006")
		}
		ms = append(ms, mv{Row{Date: p.Date, Payment: true, ID: p.ID, Ref: p.Method.Label(), Note: note, Credit: p.Amount, Method: p.Method, ChequeDue: p.ChequeDue, Cleared: p.Cleared}, 2})
	}
	sort.SliceStable(ms, func(a, b int) bool {
		if !ms[a].row.Date.Equal(ms[b].row.Date) {
			return ms[a].row.Date.Before(ms[b].row.Date)
		}
		return ms[a].order < ms[b].order
	})
	bal := opening
	var rows []Row
	if opening != 0 {
		rows = append(rows, Row{Ref: "Açılış bakiyesi", Debit: max(opening, 0), Credit: max(-opening, 0), Balance: bal})
	}
	for _, m := range ms {
		bal += m.row.Debit - m.row.Credit
		m.row.Balance = bal
		rows = append(rows, m.row)
	}
	return rows
}
