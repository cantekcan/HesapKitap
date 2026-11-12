package trendyol

import (
	"encoding/json"
	"time"

	"github.com/shopspring/decimal"

	"hesapkitap/internal/invoice"
)

// Bu dosyadaki alan adları docs/trendyol-efaturam-api.md ile birebirdir. Dokümanda olmayan alan eklenmez.
// Dokümanda tipi belirtilmeyen cevap alanları (id, year, rank, gibStatusCode, tarihler) için
// json.Number / json.RawMessage / string kullanılır; stage'de tipler netleşince daraltılır (Açık Sorular 8).

// Enum values. Plain ASCII constants, never derived with a locale-aware case conversion (ALTIN KURAL 9).
type (
	Source          string
	InvoiceType     string
	InvoiceTypeCode string
	UnitCode        string
	AliasType       string
	PaymentMeans    string
	ResponseType    string
	DocumentType    string
	FileExtension   string
	GIBStatus       string
)

const (
	SourcePortal  Source = "PORTAL"
	SourceWeb     Source = "WEB"
	SourceMobile  Source = "MOBILE"
	SourcePartner Source = "PARTNER"
)

const (
	InvoiceTypeEArchive InvoiceType = "EARSIVFATURA"
	InvoiceTypeBasic    InvoiceType = "TEMELFATURA"
	InvoiceTypeCommerce InvoiceType = "TICARIFATURA"
)

const (
	InvoiceTypeCodeSale   InvoiceTypeCode = "SATIS"
	InvoiceTypeCodeReturn InvoiceTypeCode = "IADE"
)

const (
	UnitPiece    UnitCode = "C62"
	UnitKilogram UnitCode = "KGM"
	UnitLiter    UnitCode = "LTR"
	UnitMeter    UnitCode = "MTR"
	UnitSquareM  UnitCode = "MTK"
	UnitCubicM   UnitCode = "MTQ"
	UnitHour     UnitCode = "HUR"
	UnitDay      UnitCode = "DAY"
	UnitMonth    UnitCode = "MON"
	UnitPack     UnitCode = "PA"
	UnitBox      UnitCode = "BX"
	UnitSet      UnitCode = "SET"
	UnitDozen    UnitCode = "DZN"
	UnitTon      UnitCode = "TNE"
)

const (
	AliasTypeInvoice        AliasType = "INVOICE"
	AliasTypeDespatchAdvice AliasType = "DESPATCH_ADVICE"
)

const (
	PaymentCreditCard PaymentMeans = "CREDIT_CARD"
	PaymentEFT        PaymentMeans = "EFT"
	PaymentOnDelivery PaymentMeans = "ON_DELIVERY"
	PaymentMediator   PaymentMeans = "MEDIATOR"
	PaymentOther      PaymentMeans = "OTHER"
)

const (
	ResponseApproved     ResponseType = "APPROVED"
	ResponseAutoApproved ResponseType = "AUTO_APPROVED"
	ResponseDenied       ResponseType = "DENIED"
	ResponseReturned     ResponseType = "RETURNED"
)

const (
	DocumentEArchive DocumentType = "EARCHIVE"
	DocumentEInvoice DocumentType = "EINVOICE"
)

const (
	FilePDF  FileExtension = "pdf"
	FileXML  FileExtension = "xml"
	FileHTML FileExtension = "html"
)

const (
	GIBReported          GIBStatus = "REPORTED"
	GIBReadyToBeReported GIBStatus = "READY_TO_BE_REPORTED"
)

// KDVTaxType is the only tax type used (MVP).
const KDVTaxType = "KDV"

// Num builds a JSON number literal from a decimal without going through float64
// (quantity and unitPriceAmount are "double" in the API; money must never touch float).
func Num(d decimal.Decimal) json.Number { return json.Number(d.String()) }

// ---------------------------------------------------------------------------------------------
// Auth / mükellef

// TaxpayerRecord is one element of GET /api/invoice/taxpayers/{taxId}.
type TaxpayerRecord struct {
	TaxID             string    `json:"taxId"`
	Alias             string    `json:"alias"`
	Title             string    `json:"title"`
	GIBUserType       string    `json:"gibUserType"`
	AliasType         AliasType `json:"aliasType"`
	PostBoxType       string    `json:"postBoxType"`
	AliasCreationTime string    `json:"aliasCreationTime"`
	FirstCreationTime string    `json:"firstCreationTime"`
	DeletedAt         *string   `json:"deletedAt"`
}

// ---------------------------------------------------------------------------------------------
// Fatura gövdesi (e-Arşiv ve e-Fatura aynı yapıyı kullanır). Tüm tutarlar KURUŞ cinsinden tamsayıdır.

// InvoiceRequest is the body of POST .../earchive and POST .../outgoing-einvoice.
type InvoiceRequest struct {
	AutoInvoiceID    *bool         `json:"autoInvoiceId,omitempty"`
	XSLTCode         string        `json:"xsltCode,omitempty"`
	CompanyID        int64         `json:"companyId"`
	UserID           int64         `json:"userId"`
	Source           Source        `json:"source"`
	LocalReferenceID string        `json:"localReferenceId,omitempty"`
	Prefix           string        `json:"prefix,omitempty"`
	Notes            []string      `json:"notes,omitempty"`
	IssuedAt         *time.Time    `json:"issuedAt,omitempty"`
	RecipientInfo    RecipientInfo `json:"recipientInfo"`
	InvoiceInfo      InvoiceInfo   `json:"invoiceInfo"`
	InvoiceLines     []InvoiceLine `json:"invoiceLines"`
	TotalTax         TotalTax      `json:"totalTax"`
	InvoiceTotal     InvoiceTotal  `json:"invoiceTotal"`
	PaymentInfo      *PaymentInfo  `json:"paymentInfo,omitempty"`
	BillingReference *BillingRef   `json:"billingReference,omitempty"`
	OrderInfo        *OrderInfo    `json:"orderInfo,omitempty"`
	TargetAlias      string        `json:"targetAlias,omitempty"` // sadece e-Fatura
}

// RecipientInfo is the invoice recipient. Validation rules: docs/trendyol-efaturam-api.md bölüm 5.
type RecipientInfo struct {
	TaxID       string `json:"taxId"`
	Name        string `json:"name"`
	Surname     string `json:"surname,omitempty"`
	City        string `json:"city"`
	District    string `json:"district,omitempty"`
	Address     string `json:"address,omitempty"`
	PostalCode  string `json:"postalCode,omitempty"`
	CountryCode string `json:"countryCode,omitempty"`
	TaxOffice   string `json:"taxOffice,omitempty"`
	Phone       string `json:"phone,omitempty"`
	Email       string `json:"email,omitempty"`
}

// InvoiceInfo carries scenario, type, number and ETTN. We always send our own InvoiceUUID (ALTIN KURAL 5).
type InvoiceInfo struct {
	InvoiceType     InvoiceType     `json:"invoiceType"`
	InvoiceTypeCode InvoiceTypeCode `json:"invoiceTypeCode"`
	InvoiceID       string          `json:"invoiceId,omitempty"`
	InvoiceUUID     string          `json:"invoiceUuid,omitempty"`
}

// InvoiceLine is one invoice line. unitPriceAmount and quantity are "double" in the API: use Num().
type InvoiceLine struct {
	ItemName            string      `json:"itemName"`
	Quantity            json.Number `json:"quantity"`
	UnitCode            UnitCode    `json:"unitCode,omitempty"`
	UnitPriceAmount     json.Number `json:"unitPriceAmount"`
	TotalDiscountAmount int64       `json:"totalDiscountAmount"`
	TaxableAmount       int64       `json:"taxableAmount"`
	TaxPercent          int32       `json:"taxPercent"`
	TaxAmount           int64       `json:"taxAmount"`
	TotalAmount         int64       `json:"totalAmount"` // ⚠️ KDV dahil mi hariç mi: stage'de doğrulanacak (Açık Sorular 3)
	TaxName             string      `json:"taxName,omitempty"`
	TaxCode             string      `json:"taxCode,omitempty"`
	TotalTax            *TotalTax   `json:"totalTax,omitempty"`
}

// TotalTax is the tax summary; one SubTotalTax per KDV rate.
type TotalTax struct {
	TotalTaxAmount int64         `json:"totalTaxAmount"`
	SubTotalTaxes  []SubTotalTax `json:"subTotalTaxes"`
}

// SubTotalTax is the tax total for one rate.
type SubTotalTax struct {
	TaxableAmount          int64   `json:"taxableAmount"`
	TaxAmount              int64   `json:"taxAmount"`
	TaxType                string  `json:"taxType"`
	Percent                int32   `json:"percent"`
	Name                   string  `json:"name"`
	TaxExemptionReason     *string `json:"taxExemptionReason"`
	TaxExemptionReasonCode *string `json:"taxExemptionReasonCode"`
}

// InvoiceTotal are the invoice totals, all in kuruş.
type InvoiceTotal struct {
	LineExtensionAmount  int64 `json:"lineExtensionAmount"`
	AllowanceTotalAmount int64 `json:"allowanceTotalAmount"`
	TaxExclusiveAmount   int64 `json:"taxExclusiveAmount"`
	TaxInclusiveAmount   int64 `json:"taxInclusiveAmount"`
	PayableAmount        int64 `json:"payableAmount"`
}

// PaymentInfo is mandatory for internet-sale e-Arşiv invoices. Date formats are not specified in the
// reference, so they are plain strings (Açık Sorular 8).
type PaymentInfo struct {
	PurchaseURL      string       `json:"purchaseUrl,omitempty"`
	PaymentAgentName string       `json:"paymentAgentName,omitempty"`
	PaymentType      string       `json:"paymentType,omitempty"`
	PaymentDate      string       `json:"paymentDate,omitempty"`
	PaymentDueDate   string       `json:"paymentDueDate,omitempty"`
	PaymentMeans     PaymentMeans `json:"paymentMeans,omitempty"`
	InstructionNote  string       `json:"instructionNote,omitempty"`
}

// BillingRef references the returned invoice on an IADE invoice.
type BillingRef struct {
	InvoiceID string `json:"invoiceId"`
	IssueDate string `json:"issueDate"`
	TypeCode  string `json:"typeCode"`
}

// OrderInfo links the invoice to an order.
type OrderInfo struct {
	OrderID   string `json:"orderId"`
	OrderDate string `json:"orderDate"`
}

// ---------------------------------------------------------------------------------------------
// Cevaplar

// CreateResponse is the answer to creating an e-Arşiv or e-Fatura (only documented fields).
type CreateResponse struct {
	ID                 json.Number            `json:"id"`
	InvoiceUUID        string                 `json:"invoiceUuid"`
	InvoiceID          string                 `json:"invoiceId"`
	Prefix             string                 `json:"prefix"`
	Year               json.Number            `json:"year"`
	Rank               json.Number            `json:"rank"`
	Status             invoice.ProviderStatus `json:"status"`
	GIBStatus          GIBStatus              `json:"gibStatus"`
	TaxExcludedPrice   json.Number            `json:"taxExcludedPrice"`
	TaxAmount          json.Number            `json:"taxAmount"`
	DiscountAmount     json.Number            `json:"discountAmount"`
	PayableAmount      json.Number            `json:"payableAmount"`
	TaxInclusiveAmount json.Number            `json:"taxInclusiveAmount"`
	ReceiverName       string                 `json:"receiverName"`
	ReceiverTaxID      string                 `json:"receiverTaxId"`
	Scenario           string                 `json:"scenario"`
	InvoiceTypeCode    InvoiceTypeCode        `json:"invoiceTypeCode"`
	IssuedAt           string                 `json:"issuedAt"`
	CreatedAt          string                 `json:"createdAt"`
	LocalReferenceID   string                 `json:"localReferenceId"`
}

// EArchiveStatus is the answer of GET .../earchive/status/{invoiceUuid}.
type EArchiveStatus struct {
	Status      invoice.ProviderStatus `json:"status"`
	GIBStatus   GIBStatus              `json:"gibStatus"`
	InvoiceUUID string                 `json:"invoiceUuid"`
}

// EInvoiceStatus is the answer of GET .../outgoing-einvoice/status/{invoiceUuid}.
type EInvoiceStatus struct {
	Status        invoice.ProviderStatus `json:"status"`
	GIBStatusCode json.RawMessage        `json:"gibStatusCode"`
	InvoiceUUID   string                 `json:"invoiceUuid"`
}

// SentReply is the recipient's answer to a commercial e-Fatura.
type SentReply struct {
	ResponseType ResponseType `json:"responseType"`
	Note         string       `json:"note"`
	IssuedAt     string       `json:"issuedAt"`
}

// ---------------------------------------------------------------------------------------------
// Diğer istekler

type cancelRequest struct {
	InvoiceUUID string `json:"invoiceUuid"`
	CompanyID   int64  `json:"companyId"`
}

type resendRequest struct {
	InvoiceUUID    string `json:"invoiceUuid"`
	NewTargetAlias string `json:"newTargetAlias,omitempty"`
	CompanyID      int64  `json:"companyId"`
}

// DownloadRequest asks for a temporary link to a document (PDF, XML or HTML).
// DocumentUUID is the ETTN. CompanyID is filled from the session.
type DownloadRequest struct {
	DocumentType  DocumentType  `json:"documentType"`
	FileExtension FileExtension `json:"fileExtension"`
	DocumentUUID  string        `json:"documentUuid"`
	CompanyID     int64         `json:"companyId"`
	Inline        bool          `json:"inline"`
	Envelope      bool          `json:"envelope"`
}

type permanentDownloadRequest struct {
	DocumentType  DocumentType  `json:"documentType"`
	FileExtension FileExtension `json:"fileExtension"`
	DocumentUUID  string        `json:"documentUuid"`
	CompanyID     int64         `json:"companyId"`
}
