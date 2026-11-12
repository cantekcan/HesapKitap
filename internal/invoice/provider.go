// Package invoice holds the invoice domain. It never talks to Trendyol directly:
// it uses an EInvoiceProvider, implemented by internal/trendyol.
package invoice

import "context"

// TaxpayerAlias is one e-Fatura mailbox label (posta kutusu etiketi) of a taxpayer.
type TaxpayerAlias struct {
	TaxID string
	Alias string
	Title string
}

// EInvoiceProvider is the port to the e-invoice service provider.
//
// Faz 2'de yalnızca mükellef sorgusu vardır. Fatura oluşturma, durum, iptal ve doküman
// metotları Faz 5'te, alan modeli (Invoice) oluştuktan sonra buraya eklenir.
type EInvoiceProvider interface {
	// LookupTaxpayer returns the taxpayer's e-Fatura aliases for a VKN (10 digits) or TCKN (11 digits).
	// An empty result (and nil error) means the taxpayer is NOT an e-Fatura user: issue an e-Arşiv invoice.
	LookupTaxpayer(ctx context.Context, taxID string) ([]TaxpayerAlias, error)
}
