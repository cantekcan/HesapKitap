package trendyol_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hesapkitap/internal/incoming"
	"hesapkitap/internal/invoice"
	"hesapkitap/internal/trendyol"
)

func TestIncomingSearchRequestAndMapping(t *testing.T) {
	f := newFakeAPI(t)
	f.signin(func(int32) string { return defaultToken(t) })
	var raw []byte
	f.handle("POST /api/invoice/documents/incoming-einvoice/search", func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		writeJSON(w, `{"content":[
			{"id":1,"currency":"TRY","invoiceUuid":"`+ettn+`","invoiceId":"ABC2026000000001","status":205,"payableAmount":123456,
			 "scenario":"TICARIFATURA","invoiceTypeCode":"SATIS","issuedAt":"2026-11-02T10:00:00Z","senderTitle":"Tedarik Ltd.","senderTaxId":"1234567890"},
			{"id":2,"currency":"TRY","invoiceUuid":"`+uuid.NewString()+`","invoiceId":"B2","payableAmount":12.5,"scenario":"TEMELFATURA",
			 "senderName":"Ali","senderSurname":"Veli","replyType":"APPROVED","replyNote":"tamam"}
		],"pageSize":20,"currentPage":0,"totalPageCount":3,"totalCount":41}`)
	})

	p := trendyol.Incoming{C: f.client()}
	pg, err := p.Search(t.Context(), incoming.Filter{Unread: true, Sender: " Tedarik ", Scenario: incoming.ScenarioCommercial, Page: 2, Size: 20})
	require.NoError(t, err)

	var body map[string]any
	require.NoError(t, json.Unmarshal(raw, &body))
	assert.EqualValues(t, 42, body["companyId"])
	assert.Equal(t, "TICARIFATURA", body["scenario"])
	assert.Equal(t, false, body["isRead"], "okunmamış: isRead=false")
	assert.Equal(t, "Tedarik", body["senderTitle"])
	assert.Equal(t, map[string]any{"page": float64(2), "size": float64(20)}, body["pagination"])

	assert.Equal(t, 41, pg.Total)
	assert.Equal(t, 3, pg.Pages)
	require.Len(t, pg.Items, 2)
	first := pg.Items[0]
	assert.Equal(t, "Tedarik Ltd.", first.Sender)
	assert.Equal(t, int64(123456), first.Payable, "tam sayı kuruş")
	assert.Equal(t, invoice.StatusApproved, first.Status)
	assert.True(t, first.AwaitingReply())
	second := pg.Items[1]
	assert.Equal(t, "Ali Veli", second.Sender, "ünvan yoksa ad soyad")
	assert.Equal(t, int64(1250), second.Payable, "ondalıklı tutar TL sayılır")
	assert.False(t, second.AwaitingReply())
	assert.Equal(t, invoice.ReplyApproved, second.ReplyType)

	// Süzgeç verilmezse isRead hiç gönderilmez
	_, err = p.Search(t.Context(), incoming.Filter{Size: 1})
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "isRead")
}

func TestIncomingReplyAndDocumentURL(t *testing.T) {
	f := newFakeAPI(t)
	f.signin(func(int32) string { return defaultToken(t) })
	bodies := map[string]string{}
	f.handle("POST /api/invoice/documents/sent-reply", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies["reply"] = string(b)
		w.WriteHeader(http.StatusOK)
	})
	f.handle("POST /api/invoice/documents/download", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies["download"] = string(b)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("https://dosya.example.test/x.pdf"))
	})

	p := trendyol.Incoming{C: f.client()}
	id := uuid.MustParse(ettn)
	require.NoError(t, p.Reply(t.Context(), id, invoice.ReplyDenied, "Sipariş yok"))
	assert.JSONEq(t, `{"invoiceUuid":"`+ettn+`","responseType":"DENIED","note":"Sipariş yok","companyId":42}`, bodies["reply"])

	link, err := p.DocumentURL(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, "https://dosya.example.test/x.pdf", link)
	assert.JSONEq(t, `{"documentType":"INCOMING_EINVOICE","fileExtension":"pdf","documentUuid":"`+ettn+`","companyId":42,"inline":true,"envelope":false}`, bodies["download"])
}
