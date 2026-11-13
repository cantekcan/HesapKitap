package trendyol

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"hesapkitap/internal/incoming"
	"hesapkitap/internal/invoice"
)

// Gelen e-Fatura uçları: https://developers.trendyolefaturam.com/OpenApi/Gelen%20eFatura/ (docs/trendyol-efaturam-api.md §7b).
const (
	pathIncomingSearch = "/api/invoice/documents/incoming-einvoice/search"
	pathIncomingReply  = "/api/invoice/documents/sent-reply"
)

// DocumentIncomingEInvoice is the documentType of an incoming e-Fatura in the download endpoints.
const DocumentIncomingEInvoice DocumentType = "INCOMING_EINVOICE"

// pagination is the paging object of the search request.
type pagination struct {
	Page int `json:"page"`
	Size int `json:"size"`
}

type incomingSearchRequest struct {
	CompanyID   int64       `json:"companyId"`
	Scenario    InvoiceType `json:"scenario,omitempty"`
	IsRead      *bool       `json:"isRead,omitempty"`
	SenderTitle string      `json:"senderTitle,omitempty"`
	Pagination  pagination  `json:"pagination"`
}

// IncomingInvoice is one element of the search answer; field names are those of the documentation.
type IncomingInvoice struct {
	ID              json.Number            `json:"id"`
	Currency        string                 `json:"currency"`
	InvoiceUUID     string                 `json:"invoiceUuid"`
	InvoiceID       string                 `json:"invoiceId"`
	Status          invoice.ProviderStatus `json:"status"`
	PayableAmount   json.Number            `json:"payableAmount"`
	Scenario        string                 `json:"scenario"`
	InvoiceTypeCode string                 `json:"invoiceTypeCode"`
	IssuedAt        string                 `json:"issuedAt"`
	SenderName      string                 `json:"senderName"`
	SenderSurname   string                 `json:"senderSurname"`
	SenderTitle     string                 `json:"senderTitle"`
	SenderTaxID     string                 `json:"senderTaxId"`
	ReplyNote       string                 `json:"replyNote"`
	ReplyType       string                 `json:"replyType"`
}

// IncomingSearchResponse is the answer of POST .../incoming-einvoice/search.
type IncomingSearchResponse struct {
	Content        []IncomingInvoice `json:"content"`
	PageSize       int               `json:"pageSize"`
	CurrentPage    int               `json:"currentPage"`
	TotalPageCount int               `json:"totalPageCount"`
	TotalCount     int64             `json:"totalCount"`
}

type incomingReplyRequest struct {
	InvoiceUUID  string       `json:"invoiceUuid"`
	ResponseType ResponseType `json:"responseType"`
	Note         string       `json:"note"`
	CompanyID    int64        `json:"companyId"`
}

// SearchIncomingEInvoice lists the incoming invoices.
func (c *Client) SearchIncomingEInvoice(ctx context.Context, f incoming.Filter) (*IncomingSearchResponse, error) {
	companyID, _, err := c.ids(ctx)
	if err != nil {
		return nil, err
	}
	req := incomingSearchRequest{
		CompanyID: companyID, Scenario: InvoiceType(f.Scenario), SenderTitle: strings.TrimSpace(f.Sender),
		Pagination: pagination{Page: f.Page, Size: f.Size},
	}
	if f.Unread {
		unread := false
		req.IsRead = &unread
	}
	var out IncomingSearchResponse
	if err := c.do(ctx, http.MethodPost, pathIncomingSearch, nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReplyIncomingEInvoice answers an incoming commercial e-Fatura.
func (c *Client) ReplyIncomingEInvoice(ctx context.Context, invoiceUUID string, answer ResponseType, note string) error {
	companyID, _, err := c.ids(ctx)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPost, pathIncomingReply, nil,
		incomingReplyRequest{InvoiceUUID: invoiceUUID, ResponseType: answer, Note: note, CompanyID: companyID}, nil)
}

// Incoming adapts the client to incoming.Provider.
type Incoming struct{ C *Client }

var _ incoming.Provider = Incoming{}

// Search implements incoming.Provider.
func (p Incoming) Search(ctx context.Context, f incoming.Filter) (incoming.Page, error) {
	r, err := p.C.SearchIncomingEInvoice(ctx, f)
	if err != nil {
		return incoming.Page{}, err
	}
	out := incoming.Page{Total: int(r.TotalCount), Pages: r.TotalPageCount, Page: r.CurrentPage}
	for _, it := range r.Content {
		out.Items = append(out.Items, toIncoming(it))
	}
	return out, nil
}

// Reply implements incoming.Provider.
func (p Incoming) Reply(ctx context.Context, id uuid.UUID, answer invoice.ReplyType, note string) error {
	return p.C.ReplyIncomingEInvoice(ctx, id.String(), ResponseType(answer), note)
}

// DocumentURL implements incoming.Provider.
func (p Incoming) DocumentURL(ctx context.Context, id uuid.UUID) (string, error) {
	return p.C.DownloadURL(ctx, DownloadRequest{DocumentType: DocumentIncomingEInvoice, FileExtension: FilePDF, DocumentUUID: id.String(), Inline: true})
}

func toIncoming(it IncomingInvoice) incoming.Invoice {
	id, _ := uuid.Parse(it.InvoiceUUID)
	sender := strings.TrimSpace(it.SenderTitle)
	if sender == "" {
		sender = strings.TrimSpace(it.SenderName + " " + it.SenderSurname)
	}
	issued, _ := time.Parse(time.RFC3339, it.IssuedAt)
	return incoming.Invoice{
		UUID: id, Number: it.InvoiceID, Sender: sender, SenderTaxID: it.SenderTaxID, Scenario: it.Scenario,
		TypeCode: it.InvoiceTypeCode, Status: it.Status, Payable: kurus(it.PayableAmount), Currency: it.Currency,
		IssuedAt: issued, ReplyType: invoice.ReplyType(it.ReplyType), ReplyNote: it.ReplyNote,
	}
}

// kurus reads an amount. The documentation does not say whether answers carry kuruş or TL (Açık Sorular 14):
// whole numbers are taken as kuruş like the request totals, decimals as TL.
func kurus(n json.Number) int64 {
	d, err := decimal.NewFromString(n.String())
	if err != nil {
		return 0
	}
	if d.IsInteger() {
		return d.IntPart()
	}
	return d.Mul(decimal.NewFromInt(100)).Round(0).IntPart()
}
