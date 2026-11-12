// Package invoice holds the invoice domain. It never talks to Trendyol directly:
// it uses an EInvoiceProvider, implemented by internal/trendyol.
package invoice

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// TaxpayerAlias is one e-Fatura mailbox label (posta kutusu etiketi) of a taxpayer.
type TaxpayerAlias struct {
	TaxID string
	Alias string
	Title string
}

// Kind is how an invoice is issued: e-Arşiv for everybody, e-Fatura for registered taxpayers.
type Kind string

const (
	KindEArchive Kind = "EARCHIVE"
	KindEInvoice Kind = "EINVOICE"
)

func (k Kind) Label() string {
	switch k {
	case KindEArchive:
		return "e-Arşiv"
	case KindEInvoice:
		return "e-Fatura"
	}
	return ""
}

// Recipient is the customer data frozen on the invoice at the moment it is issued.
type Recipient struct {
	Person     bool // şahıs (TCKN)
	TaxID      string
	Name       string
	TaxOffice  string
	Address    string
	District   string
	City       string
	PostalCode string
	Email      string
	Phone      string
}

// Taxpayer is what we know about the customer's e-Fatura registration.
type Taxpayer struct {
	IsEInvoiceUser bool
	Alias          string // e-Fatura kullanıcısı için seçilmiş etiket
}

// IssueRequest is everything the provider needs to create the invoice.
// ETTN was generated and stored BEFORE this request (ALTIN KURAL 5).
type IssueRequest struct {
	ETTN        uuid.UUID
	LocalRef    string // bizim fatura kimliğimiz
	Kind        Kind
	Scenario    string // e-Fatura: TEMELFATURA | TICARIFATURA
	Prefix      string // portalda tanımlı 3 karakterli fatura serisi; boşsa sağlayıcının varsayılan serisi
	TargetAlias string
	IssueDate   time.Time
	Notes       string
	Recipient   Recipient
	Lines       []Line
	Totals      Totals
}

// IssueResult is the provider's answer to a successful creation.
type IssueResult struct {
	Number string // fatura numarası; boş olabilir
	Status ProviderStatus
}

// RejectedError means the provider refused the invoice and did NOT create it (validation, HTTP 400/422).
// Retrying the same request is pointless; the user must fix the invoice.
type RejectedError struct{ Message string }

func (e *RejectedError) Error() string { return e.Message }

// ErrNotEArchive is returned when cancelling something that is not an e-Arşiv invoice.
var ErrNotEArchive = errors.New("yalnızca e-Arşiv faturalar iptal edilebilir; e-Fatura için iade faturası gerekir")

// StatusInfo is the provider's view of an invoice. Number is the invoice number (e.g. ABC2026000000003) and may be empty.
type StatusInfo struct {
	Status ProviderStatus
	Number string
}

// EInvoiceProvider is the port to the e-invoice service provider.
type EInvoiceProvider interface {
	// LookupTaxpayer returns the taxpayer's e-Fatura aliases for a VKN (10 digits) or TCKN (11 digits).
	// An empty result (and nil error) means the taxpayer is NOT an e-Fatura user: issue an e-Arşiv invoice.
	LookupTaxpayer(ctx context.Context, taxID string) ([]TaxpayerAlias, error)

	// Issue creates the invoice. A *RejectedError means it was definitely not created.
	// Any other error (timeout, 5xx, connection) leaves the outcome UNKNOWN.
	Issue(ctx context.Context, req IssueRequest) (IssueResult, error)

	// Status asks the provider about an invoice by ETTN. found=false (nil error) means the provider has no
	// such invoice: sending again is safe (ALTIN KURAL 5).
	Status(ctx context.Context, kind Kind, ettn uuid.UUID) (info StatusInfo, found bool, err error)

	// Cancel cancels an e-Arşiv invoice.
	Cancel(ctx context.Context, ettn uuid.UUID) error

	// DocumentURL returns a link to the PDF. permanent=true gives a shareable link that does not expire.
	DocumentURL(ctx context.Context, kind Kind, ettn uuid.UUID, permanent bool) (string, error)
}
