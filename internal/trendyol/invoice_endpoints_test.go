package trendyol_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hesapkitap/internal/invoice"
	"hesapkitap/internal/trendyol"
)

const ettn = "5d2a8a3c-6f0e-4b1f-9a52-0c1d2e3f4a5b"

func sampleRequest() *trendyol.InvoiceRequest {
	issued := time.Date(2026, 11, 12, 10, 30, 0, 0, time.UTC)
	return &trendyol.InvoiceRequest{
		LocalReferenceID: "inv-1001",
		Prefix:           "ABC",
		IssuedAt:         &issued,
		RecipientInfo: trendyol.RecipientInfo{
			TaxID: "11111111111", Name: "Ayşe", Surname: "Yılmaz", City: "İstanbul", Email: "ayse@ornek.com",
		},
		InvoiceInfo: trendyol.InvoiceInfo{InvoiceTypeCode: trendyol.InvoiceTypeCodeSale, InvoiceUUID: ettn},
		InvoiceLines: []trendyol.InvoiceLine{{
			ItemName:        "Elma",
			Quantity:        trendyol.Num(decimal.RequireFromString("1.5")),
			UnitCode:        trendyol.UnitKilogram,
			UnitPriceAmount: trendyol.Num(decimal.RequireFromString("8367")),
			TaxableAmount:   12550, TaxPercent: 1, TaxAmount: 126, TotalAmount: 12676,
		}},
		TotalTax: trendyol.TotalTax{TotalTaxAmount: 126, SubTotalTaxes: []trendyol.SubTotalTax{
			{TaxableAmount: 12550, TaxAmount: 126, TaxType: trendyol.KDVTaxType, Percent: 1, Name: "KDV"},
		}},
		InvoiceTotal: trendyol.InvoiceTotal{
			LineExtensionAmount: 12550, TaxExclusiveAmount: 12550, TaxInclusiveAmount: 12676, PayableAmount: 12676,
		},
	}
}

func TestCreateEArchiveBodyAndResponse(t *testing.T) {
	f := newFakeAPI(t)
	f.signin(func(int32) string { return defaultToken(t) })
	var raw []byte
	f.handle("POST /api/invoice/documents/earchive", func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		writeJSON(w, `{"id":123,"invoiceUuid":"`+ettn+`","invoiceId":"ABC2026000000001","prefix":"ABC","year":2026,"rank":1,
			"status":205,"gibStatus":"REPORTED","taxExcludedPrice":12550,"taxAmount":126,"payableAmount":12676,
			"receiverName":"Ayşe Yılmaz","scenario":"EARSIVFATURA","invoiceTypeCode":"SATIS","localReferenceId":"inv-1001"}`)
	})

	// InvoiceType bilerek verilmedi: CreateEArchive EARSIVFATURA'ya zorlar.
	resp, err := f.client().CreateEArchive(t.Context(), sampleRequest())
	require.NoError(t, err)

	assert.Equal(t, invoice.StatusApproved, resp.Status)
	assert.Equal(t, "ABC2026000000001", resp.InvoiceID)
	assert.Equal(t, ettn, resp.InvoiceUUID)
	assert.Equal(t, trendyol.GIBReported, resp.GIBStatus)

	var body map[string]any
	require.NoError(t, json.Unmarshal(raw, &body))
	assert.EqualValues(t, 42, body["companyId"], "companyId JWT'den doldurulur")
	assert.EqualValues(t, 7, body["userId"], "userId JWT'den doldurulur")
	assert.Equal(t, "WEB", body["source"])
	assert.Equal(t, "inv-1001", body["localReferenceId"])
	info := body["invoiceInfo"].(map[string]any)
	assert.Equal(t, "EARSIVFATURA", info["invoiceType"])
	assert.Equal(t, "SATIS", info["invoiceTypeCode"])
	assert.Equal(t, ettn, info["invoiceUuid"], "ETTN bizden gider (ALTIN KURAL 5)")
	assert.NotContains(t, body, "targetAlias")

	// Para alanları kuruş tamsayı; miktar float'a uğramadan ondalık sayı olarak gider.
	rawStr := string(raw)
	assert.Contains(t, rawStr, `"quantity":1.5`)
	assert.Contains(t, rawStr, `"unitPriceAmount":8367`)
	assert.Contains(t, rawStr, `"payableAmount":12676`)
	assert.Contains(t, rawStr, `"taxExemptionReason":null`)
	assert.Contains(t, rawStr, `"issuedAt":"2026-11-12T10:30:00Z"`)
}

func TestCreateOutgoingEInvoiceChecksTypeBeforeAnyRequest(t *testing.T) {
	f := newFakeAPI(t)
	f.signin(func(int32) string { return defaultToken(t) })

	req := sampleRequest()
	req.InvoiceInfo.InvoiceType = trendyol.InvoiceTypeEArchive
	_, err := f.client().CreateOutgoingEInvoice(t.Context(), req)
	require.Error(t, err)
	assert.Zero(t, f.logins.Load(), "yanlış tipte ağa hiç çıkılmaz")
}

func TestCreateOutgoingEInvoiceSendsTargetAlias(t *testing.T) {
	f := newFakeAPI(t)
	f.signin(func(int32) string { return defaultToken(t) })
	var body map[string]any
	f.handle("POST /api/invoice/documents/outgoing-einvoice", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, `{"invoiceUuid":"`+ettn+`","status":10}`)
	})

	req := sampleRequest()
	req.InvoiceInfo.InvoiceType = trendyol.InvoiceTypeCommerce
	req.TargetAlias = "urn:mail:pk@x.com"
	resp, err := f.client().CreateOutgoingEInvoice(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, invoice.StatusProcessing, resp.Status)
	assert.Equal(t, "urn:mail:pk@x.com", body["targetAlias"])
	assert.Equal(t, "TICARIFATURA", body["invoiceInfo"].(map[string]any)["invoiceType"])
}

func TestStatusEndpoints(t *testing.T) {
	f := newFakeAPI(t)
	f.signin(func(int32) string { return defaultToken(t) })
	f.handle("GET /api/invoice/documents/earchive/status/{uuid}", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, ettn, r.PathValue("uuid"))
		writeJSON(w, `{"status":205,"gibStatus":"READY_TO_BE_REPORTED","invoiceUuid":"`+ettn+`"}`)
	})
	f.handle("GET /api/invoice/documents/outgoing-einvoice/status/{uuid}", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, `{"status":40,"gibStatusCode":1300,"invoiceUuid":"`+ettn+`"}`)
	})
	f.handle("GET /api/invoice/documents/earchive/status/yok", func(w http.ResponseWriter, _ *http.Request) {
		problem(w, http.StatusNotFound, "Not Found", "invoice not found")
	})
	c := f.client()

	ea, err := c.EArchiveStatus(t.Context(), ettn)
	require.NoError(t, err)
	assert.Equal(t, invoice.StatusApproved, ea.Status)
	assert.Equal(t, trendyol.GIBReadyToBeReported, ea.GIBStatus)

	ei, err := c.OutgoingEInvoiceStatus(t.Context(), ettn)
	require.NoError(t, err)
	assert.Equal(t, invoice.StatusSentToGIB, ei.Status)
	assert.JSONEq(t, `1300`, string(ei.GIBStatusCode))

	_, err = c.EArchiveStatus(t.Context(), "yok")
	assert.True(t, trendyol.IsNotFound(err), "404 → fatura sağlayıcıda yok → tekrar göndermek güvenli")
}

func TestCancelAndResendBodies(t *testing.T) {
	f := newFakeAPI(t)
	f.signin(func(int32) string { return defaultToken(t) })
	bodies := map[string]string{}
	for _, p := range []string{"earchive/cancel", "outgoing-einvoice/resend"} {
		f.handle("POST /api/invoice/documents/"+p, func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			bodies[p] = string(b)
			w.WriteHeader(http.StatusOK)
		})
	}
	c := f.client()

	require.NoError(t, c.CancelEArchive(t.Context(), ettn))
	assert.JSONEq(t, `{"invoiceUuid":"`+ettn+`","companyId":42}`, bodies["earchive/cancel"])

	require.NoError(t, c.ResendOutgoingEInvoice(t.Context(), ettn, "urn:mail:yeni@x.com"))
	assert.JSONEq(t, `{"invoiceUuid":"`+ettn+`","newTargetAlias":"urn:mail:yeni@x.com","companyId":42}`, bodies["outgoing-einvoice/resend"])
}

func TestSentReplyUsesCompanyIDFromToken(t *testing.T) {
	f := newFakeAPI(t)
	f.signin(func(int32) string { return defaultToken(t) })
	f.handle("GET /api/invoice/documents/sent-reply/{companyId}/{uuid}", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "42", r.PathValue("companyId"))
		assert.Equal(t, ettn, r.PathValue("uuid"))
		writeJSON(w, `{"responseType":"DENIED","note":"yanlış tutar","issuedAt":"2026-11-12T10:00:00Z"}`)
	})

	reply, err := f.client().SentReply(t.Context(), ettn)
	require.NoError(t, err)
	assert.Equal(t, trendyol.ResponseDenied, reply.ResponseType)
	assert.Equal(t, "yanlış tutar", reply.Note)
}

func TestDownloadLinks(t *testing.T) {
	f := newFakeAPI(t)
	f.signin(func(int32) string { return defaultToken(t) })
	bodies := map[string]string{}
	f.handle("POST /api/invoice/documents/download", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies["temp"] = string(b)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "https://files.example/temp.pdf\n")
	})
	f.handle("POST /api/invoice/documents/download/permanent-url", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies["perm"] = string(b)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "https://files.example/perm.pdf")
	})
	c := f.client()

	link, err := c.DownloadURL(t.Context(), trendyol.DownloadRequest{
		DocumentType: trendyol.DocumentEArchive, FileExtension: trendyol.FilePDF, DocumentUUID: ettn, Inline: true,
	})
	require.NoError(t, err)
	assert.Equal(t, "https://files.example/temp.pdf", link, "text/plain cevap kırpılır")
	assert.JSONEq(t, `{"documentType":"EARCHIVE","fileExtension":"pdf","documentUuid":"`+ettn+`","companyId":42,"inline":true,"envelope":false}`, bodies["temp"])

	link, err = c.PermanentDownloadURL(t.Context(), trendyol.DocumentEInvoice, trendyol.FilePDF, ettn)
	require.NoError(t, err)
	assert.Equal(t, "https://files.example/perm.pdf", link)
	assert.JSONEq(t, `{"documentType":"EINVOICE","fileExtension":"pdf","documentUuid":"`+ettn+`","companyId":42}`, bodies["perm"],
		"kalıcı linkte inline/envelope yok")
}
