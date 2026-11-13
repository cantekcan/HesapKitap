package trendyol

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"hesapkitap/internal/invoice"
)

const (
	pathTaxpayers         = "/api/invoice/taxpayers/"
	pathEArchive          = "/api/invoice/documents/earchive"
	pathOutgoingEInvoice  = "/api/invoice/documents/outgoing-einvoice"
	pathSentReply         = "/api/invoice/documents/sent-reply/"
	pathDownload          = "/api/invoice/documents/download"
	pathDownloadPermanent = "/api/invoice/documents/download/permanent-url"
)

// LookupTaxpayer implements invoice.EInvoiceProvider. 404 means "not an e-Fatura user" and yields
// an empty slice. Only live (not deleted) aliases of type INVOICE are returned.
func (c *Client) LookupTaxpayer(ctx context.Context, taxID string) ([]invoice.TaxpayerAlias, error) {
	records, err := c.TaxpayerRecords(ctx, taxID)
	if IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var aliases []invoice.TaxpayerAlias
	for _, r := range records {
		if r.AliasType != AliasTypeInvoice || (r.DeletedAt != nil && *r.DeletedAt != "") {
			continue
		}
		aliases = append(aliases, invoice.TaxpayerAlias{TaxID: r.TaxID, Alias: r.Alias, Title: r.Title})
	}
	return aliases, nil
}

// TaxpayerRecords returns the raw records of GET /api/invoice/taxpayers/{taxId}?showDeleted=false.
// A 404 is returned as an error for which IsNotFound is true.
func (c *Client) TaxpayerRecords(ctx context.Context, taxID string) ([]TaxpayerRecord, error) {
	var out []TaxpayerRecord
	q := url.Values{"showDeleted": {"false"}}
	if err := c.do(ctx, http.MethodGet, pathTaxpayers+url.PathEscape(taxID), q, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// prepare fills companyId, userId and source from the session when the caller left them empty.
func (c *Client) prepare(ctx context.Context, req *InvoiceRequest) error {
	companyID, userID, err := c.ids(ctx)
	if err != nil {
		return err
	}
	if req.CompanyID == 0 {
		req.CompanyID = companyID
	}
	if req.UserID == 0 {
		req.UserID = userID
	}
	if req.Source == "" {
		req.Source = SourceWeb // ⚠️ DOĞRULA (Açık Sorular 4)
	}
	return nil
}

// CreateEArchive issues an e-Arşiv invoice. req.InvoiceInfo.InvoiceUUID (ETTN) must be generated and
// stored by the caller BEFORE this call (ALTIN KURAL 5). The invoice type is forced to EARSIVFATURA.
func (c *Client) CreateEArchive(ctx context.Context, req *InvoiceRequest) (*CreateResponse, error) {
	if err := c.prepare(ctx, req); err != nil {
		return nil, err
	}
	req.InvoiceInfo.InvoiceType = InvoiceTypeEArchive
	var out CreateResponse
	if err := c.do(ctx, http.MethodPost, pathEArchive, nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// EArchiveStatus queries the status of an e-Arşiv invoice by ETTN. A 404 means the invoice does not exist
// at the provider (IsNotFound), which makes re-sending after an uncertain timeout safe.
func (c *Client) EArchiveStatus(ctx context.Context, invoiceUUID string) (*EArchiveStatus, error) {
	var out EArchiveStatus
	if err := c.do(ctx, http.MethodGet, pathEArchive+"/status/"+url.PathEscape(invoiceUUID), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CancelEArchive cancels an e-Arşiv invoice. e-Fatura has no cancel endpoint (use an IADE invoice).
func (c *Client) CancelEArchive(ctx context.Context, invoiceUUID string) error {
	companyID, _, err := c.ids(ctx)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPost, pathEArchive+"/cancel", nil, cancelRequest{InvoiceUUID: invoiceUUID, CompanyID: companyID}, nil)
}

// CreateOutgoingEInvoice issues an e-Fatura. req.InvoiceInfo.InvoiceType must be TEMELFATURA or TICARIFATURA;
// set req.TargetAlias when the recipient has more than one alias.
func (c *Client) CreateOutgoingEInvoice(ctx context.Context, req *InvoiceRequest) (*CreateResponse, error) {
	if req.InvoiceInfo.InvoiceType != InvoiceTypeBasic && req.InvoiceInfo.InvoiceType != InvoiceTypeCommerce {
		return nil, fmt.Errorf("e-Fatura için invoiceType %s veya %s olmalı, %q verildi",
			InvoiceTypeBasic, InvoiceTypeCommerce, req.InvoiceInfo.InvoiceType)
	}
	if err := c.prepare(ctx, req); err != nil {
		return nil, err
	}
	var out CreateResponse
	if err := c.do(ctx, http.MethodPost, pathOutgoingEInvoice, nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// OutgoingEInvoiceStatus queries the status of an outgoing e-Fatura by ETTN.
func (c *Client) OutgoingEInvoiceStatus(ctx context.Context, invoiceUUID string) (*EInvoiceStatus, error) {
	var out EInvoiceStatus
	if err := c.do(ctx, http.MethodGet, pathOutgoingEInvoice+"/status/"+url.PathEscape(invoiceUUID), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ResendOutgoingEInvoice re-sends an e-Fatura that failed at GİB, optionally to a new alias.
func (c *Client) ResendOutgoingEInvoice(ctx context.Context, invoiceUUID, newTargetAlias string) error {
	companyID, _, err := c.ids(ctx)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPost, pathOutgoingEInvoice+"/resend", nil,
		resendRequest{InvoiceUUID: invoiceUUID, NewTargetAlias: newTargetAlias, CompanyID: companyID}, nil)
}

// SentReplyByUUID returns the recipient's answer to a commercial e-Fatura.
func (c *Client) SentReplyByUUID(ctx context.Context, invoiceUUID string) (*SentReply, error) {
	companyID, _, err := c.ids(ctx)
	if err != nil {
		return nil, err
	}
	var out SentReply
	path := fmt.Sprintf("%s%d/%s", pathSentReply, companyID, url.PathEscape(invoiceUUID))
	if err := c.do(ctx, http.MethodGet, path, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DownloadURL returns a temporary link to the document (view in the browser).
func (c *Client) DownloadURL(ctx context.Context, req DownloadRequest) (string, error) {
	companyID, _, err := c.ids(ctx)
	if err != nil {
		return "", err
	}
	req.CompanyID = companyID
	var link string
	if err := c.do(ctx, http.MethodPost, pathDownload, nil, req, &link); err != nil {
		return "", err
	}
	return link, nil
}

// PermanentDownloadURL returns a permanent link, suitable to share with the customer.
func (c *Client) PermanentDownloadURL(ctx context.Context, docType DocumentType, ext FileExtension, documentUUID string) (string, error) {
	companyID, _, err := c.ids(ctx)
	if err != nil {
		return "", err
	}
	var link string
	body := permanentDownloadRequest{DocumentType: docType, FileExtension: ext, DocumentUUID: documentUUID, CompanyID: companyID}
	if err := c.do(ctx, http.MethodPost, pathDownloadPermanent, nil, body, &link); err != nil {
		return "", err
	}
	return link, nil
}
