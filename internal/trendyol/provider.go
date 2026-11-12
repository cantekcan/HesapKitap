package trendyol

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"hesapkitap/internal/invoice"
)

// Client implements invoice.EInvoiceProvider.
var _ invoice.EInvoiceProvider = (*Client)(nil)

var istanbul = mustLocation("Europe/Istanbul")

func mustLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.FixedZone("TRT", 3*60*60) // Türkiye yıl boyu UTC+3
	}
	return loc
}

// Issue creates an e-Arşiv or e-Fatura. HTTP 400/422 answers are definite refusals (*invoice.RejectedError);
// 404 on create means a missing portal setting (e.g. default series), not an existing invoice; every other error leaves the outcome unknown and the caller must check the status before retrying.
func (c *Client) Issue(ctx context.Context, in invoice.IssueRequest) (invoice.IssueResult, error) {
	req := buildRequest(in)
	var (
		resp *CreateResponse
		err  error
	)
	switch in.Kind {
	case invoice.KindEArchive:
		resp, err = c.CreateEArchive(ctx, &req)
	case invoice.KindEInvoice:
		resp, err = c.CreateOutgoingEInvoice(ctx, &req)
	default:
		return invoice.IssueResult{}, errors.New("bilinmeyen fatura türü")
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) && (apiErr.Status == 400 || apiErr.Status == 404 || apiErr.Status == 422) {
		return invoice.IssueResult{}, &invoice.RejectedError{Message: UserMessage(err)}
	}
	if err != nil {
		return invoice.IssueResult{}, err
	}
	return invoice.IssueResult{Number: resp.InvoiceID, Status: resp.Status}, nil
}

// buildRequest maps the domain invoice to the API body. Amounts are kuruş integers taken from the calculator.
func buildRequest(in invoice.IssueRequest) InvoiceRequest {
	t := in.Totals
	req := InvoiceRequest{
		LocalReferenceID: in.LocalRef,
		Prefix:           in.Prefix,
		RecipientInfo:    recipientInfo(in.Recipient),
		InvoiceInfo: InvoiceInfo{
			InvoiceType:     InvoiceType(in.Scenario),
			InvoiceTypeCode: InvoiceTypeCodeSale,
			InvoiceUUID:     in.ETTN.String(),
		},
		TotalTax: TotalTax{TotalTaxAmount: t.TotalTax},
		InvoiceTotal: InvoiceTotal{
			LineExtensionAmount:  t.LineExtension,
			AllowanceTotalAmount: t.Allowance,
			TaxExclusiveAmount:   t.TaxExclusive,
			TaxInclusiveAmount:   t.TaxInclusive,
			PayableAmount:        t.Payable,
		},
		IssuedAt:    issuedAt(in.IssueDate),
		TargetAlias: in.TargetAlias,
	}
	if n := strings.TrimSpace(in.Notes); n != "" {
		req.Notes = []string{n}
	}
	for i, l := range in.Lines {
		r := t.Lines[i]
		req.InvoiceLines = append(req.InvoiceLines, InvoiceLine{
			ItemName:            l.ItemName,
			Quantity:            Num(l.Quantity),
			UnitCode:            UnitCode(l.Unit),
			UnitPriceAmount:     Num(decimal.NewFromInt(l.UnitPrice)),
			TotalDiscountAmount: r.Discount,
			TaxableAmount:       r.Taxable,
			TaxPercent:          int32(l.TaxPercent), //nolint:gosec // 0-100 doğrulandı
			TaxAmount:           r.Tax,
			// ⚠️ totalAmount KDV dahil mi hariç mi: stage'de doğrulanacak (docs Açık Sorular 3). Şimdilik KDV dahil.
			TotalAmount: r.Total,
			// Stage her kalemde totalTax ister (422: "Total tax (totalTax) must be provided for each invoice line").
			TotalTax: &TotalTax{TotalTaxAmount: r.Tax, SubTotalTaxes: []SubTotalTax{{
				TaxableAmount: r.Taxable, TaxAmount: r.Tax, TaxType: KDVTaxType, Percent: int32(l.TaxPercent), Name: "KDV", //nolint:gosec // 0-100 doğrulandı
			}}},
		})
	}
	for _, g := range t.TaxGroups {
		req.TotalTax.SubTotalTaxes = append(req.TotalTax.SubTotalTaxes, SubTotalTax{
			TaxableAmount: g.Taxable, TaxAmount: g.Tax, TaxType: KDVTaxType,
			Percent: int32(g.Percent), Name: "KDV", //nolint:gosec // 0-100 doğrulandı
		})
	}
	return req
}

func recipientInfo(r invoice.Recipient) RecipientInfo {
	info := RecipientInfo{
		TaxID: r.TaxID, Name: r.Name, City: r.City, District: r.District, Address: r.Address,
		PostalCode: r.PostalCode, TaxOffice: r.TaxOffice, Phone: r.Phone,
		Email: strings.ToLower(r.Email), // ALTIN KURAL 9: Türkçe locale yok
	}
	if r.Person {
		// "Ad Soyad" → name + surname (soyad: son sözcük). API soyad için en az 2 karakter ister.
		if i := strings.LastIndex(r.Name, " "); i > 0 && utf8.RuneCountInString(r.Name[i+1:]) >= 2 {
			info.Name, info.Surname = strings.TrimSpace(r.Name[:i]), r.Name[i+1:]
		}
	}
	return info
}

// issuedAt returns nil (= "now" at the provider) for today's date and noon Istanbul time for other dates.
func issuedAt(date time.Time) *time.Time {
	now := time.Now().In(istanbul)
	if date.IsZero() || (date.Year() == now.Year() && date.YearDay() == now.YearDay()) {
		return nil
	}
	t := time.Date(date.Year(), date.Month(), date.Day(), 12, 0, 0, 0, istanbul)
	return &t
}

// Status implements invoice.EInvoiceProvider. A 404 yields found=false with a nil error.
func (c *Client) Status(ctx context.Context, kind invoice.Kind, ettn uuid.UUID) (invoice.StatusInfo, bool, error) {
	var (
		info invoice.StatusInfo
		err  error
	)
	switch kind {
	case invoice.KindEArchive:
		var s *EArchiveStatus
		if s, err = c.EArchiveStatus(ctx, ettn.String()); err == nil {
			info = invoice.StatusInfo{Status: s.Status, Number: s.InvoiceID}
		}
	case invoice.KindEInvoice:
		var s *EInvoiceStatus
		if s, err = c.OutgoingEInvoiceStatus(ctx, ettn.String()); err == nil {
			info = invoice.StatusInfo{Status: s.Status, Number: s.InvoiceID}
		}
	default:
		return invoice.StatusInfo{}, false, errors.New("bilinmeyen fatura türü")
	}
	if IsNotFound(err) {
		return invoice.StatusInfo{}, false, nil
	}
	if err != nil {
		return invoice.StatusInfo{}, false, err
	}
	return info, true, nil
}

// Cancel implements invoice.EInvoiceProvider (e-Arşiv only).
func (c *Client) Cancel(ctx context.Context, ettn uuid.UUID) error {
	return c.CancelEArchive(ctx, ettn.String())
}

// DocumentURL implements invoice.EInvoiceProvider: the PDF link.
func (c *Client) DocumentURL(ctx context.Context, kind invoice.Kind, ettn uuid.UUID, permanent bool) (string, error) {
	docType := DocumentEArchive
	if kind == invoice.KindEInvoice {
		docType = DocumentEInvoice
	}
	if permanent {
		return c.PermanentDownloadURL(ctx, docType, FilePDF, ettn.String())
	}
	return c.DownloadURL(ctx, DownloadRequest{DocumentType: docType, FileExtension: FilePDF, DocumentUUID: ettn.String(), Inline: true})
}
