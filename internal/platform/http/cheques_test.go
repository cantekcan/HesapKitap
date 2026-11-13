package httpserver_test

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	chequeLinkRe     = regexp.MustCompile(`/cheques/([0-9a-f-]{36})"`)
	supplierIDRe     = regexp.MustCompile(`^/suppliers/([0-9a-f-]{36})\?flash=created$`)
	supplierPayDelRe = regexp.MustCompile(`/supplier-payments/([0-9a-f-]{36})/delete`)
	attachmentLinkRe = regexp.MustCompile(`/attachments/([0-9a-f-]{36})`)
)

// tinyPNG is a valid 1x1 PNG.
var tinyPNG = func() []byte {
	b, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGNgYGD4DwABBAEAwS2OUAAAAABJRU5ErkJggg==")
	return b
}()

// chequePayment records a cheque collection and returns the cheque's id.
func chequePayment(t *testing.T, a *testApp, custID, amount string) string {
	t.Helper()
	r := a.do(http.MethodPost, "/payments", url.Values{"customer_id": {custID}, "paid_at": {"2026-01-05"}, "amount": {amount}, "method": {"CHEQUE"},
		"cheque_due": {"2026-12-31"}, "cheque_no": {"123456"}, "bank": {"Ziraat"}, "drawer": {"Ali Veli"}}, nil)
	require.Equal(t, http.StatusSeeOther, r.Status, r.Body)
	m := chequeLinkRe.FindStringSubmatch(a.get("/cheques").Body)
	require.NotNil(t, m)
	return m[1]
}

func TestChequeNeedsDueDateAndBounceReopensDebt(t *testing.T) {
	a := newTestApp(t)
	require.Equal(t, http.StatusSeeOther, a.login(testPassword).Status)
	custID := idFrom(t, a.do(http.MethodPost, "/customers", newCustomer("10000000146", "Ali Veli"), nil))

	// Vade yoksa çek kaydedilmez
	r := a.do(http.MethodPost, "/payments", url.Values{"customer_id": {custID}, "paid_at": {"2026-01-05"}, "amount": {"250,00"}, "method": {"CHEQUE"}}, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)
	assert.Contains(t, r.Body, "vade tarihi girilmeli")

	id := chequePayment(t, a, custID, "250,00")
	assert.Contains(t, a.get("/customers/"+custID).Body, "1.000,50") // 1.250,50 − 250,00

	page := a.get("/cheques/" + id)
	assert.Equal(t, http.StatusOK, page.Status)
	assert.Contains(t, page.Body, "Ali Veli")
	assert.Contains(t, page.Body, "123456")

	// Karşılıksız → borç yeniden açılır
	assert.Equal(t, http.StatusBadRequest, a.do(http.MethodPost, "/cheques/"+id+"/status", url.Values{"status": {"???"}}, nil).Status)
	require.Equal(t, http.StatusSeeOther, a.do(http.MethodPost, "/cheques/"+id+"/status", url.Values{"status": {"BOUNCED"}}, nil).Status)
	assert.NotContains(t, a.get("/customers/"+custID).Body, "1.000,50")
	assert.Contains(t, a.get("/cheques?tab=bounced").Body, "Ali Veli")
	assert.NotContains(t, a.get("/cheques").Body, "/cheques/"+id)

	// Portföye geri al → borç yeniden düşer; sonra tahsil edildi
	require.Equal(t, http.StatusSeeOther, a.do(http.MethodPost, "/cheques/"+id+"/status", url.Values{"status": {"PORTFOLIO"}}, nil).Status)
	assert.Contains(t, a.get("/customers/"+custID).Body, "1.000,50")
	require.Equal(t, http.StatusSeeOther, a.do(http.MethodPost, "/cheques/"+id+"/status", url.Values{"status": {"COLLECTED"}}, nil).Status)
	assert.Contains(t, a.get("/cheques?tab=collected").Body, "Ali Veli")
	// Tahsil edilmiş çek doğrudan karşılıksız olamaz
	assert.Equal(t, http.StatusConflict, a.do(http.MethodPost, "/cheques/"+id+"/status", url.Values{"status": {"ENDORSED"}}, nil).Status)
}

func TestSupplierDebtAndEndorsement(t *testing.T) {
	a := newTestApp(t)
	require.Equal(t, http.StatusSeeOther, a.login(testPassword).Status)
	custID := idFrom(t, a.do(http.MethodPost, "/customers", newCustomer("10000000146", "Ali Veli"), nil))

	r := a.do(http.MethodPost, "/suppliers", url.Values{"name": {"Toptan A.Ş."}, "opening_balance": {"0"}}, nil)
	require.Equal(t, http.StatusSeeOther, r.Status, r.Body)
	m := supplierIDRe.FindStringSubmatch(r.Header.Get("Location"))
	require.NotNil(t, m, r.Header.Get("Location"))
	sid := m[1]

	// Alım: borcumuz artar
	r = a.do(http.MethodPost, "/suppliers/"+sid+"/entries", url.Values{"entry_date": {"2026-01-05"}, "amount": {"5.000,00"}, "doc_no": {"F-77"}}, nil)
	require.Equal(t, http.StatusSeeOther, r.Status, r.Body)
	page := a.get("/suppliers/" + sid)
	assert.Contains(t, page.Body, "F-77")
	assert.Contains(t, page.Body, "5.000,00")
	assert.Contains(t, a.get("/suppliers").Body, "Toptan A.Ş.")

	// Kendi çekimiz vade ister
	r = a.do(http.MethodPost, "/suppliers/"+sid+"/payments", url.Values{"paid_at": {"2026-01-06"}, "amount": {"1.000,00"}, "method": {"CHEQUE"}}, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)
	r = a.do(http.MethodPost, "/suppliers/"+sid+"/payments", url.Values{"paid_at": {"2026-01-06"}, "amount": {"1.000,00"}, "method": {"CHEQUE"}, "cheque_due": {"2026-06-01"}, "cheque_no": {"B-1"}}, nil)
	require.Equal(t, http.StatusSeeOther, r.Status, r.Body)
	assert.Contains(t, a.get("/cheques/own").Body, "B-1")

	// Müşteri çekini ciro et: borç 4.000 → 3.750
	cid := chequePayment(t, a, custID, "250,00")
	r = a.do(http.MethodPost, "/cheques/"+cid+"/endorse", url.Values{"supplier_id": {sid}}, nil)
	require.Equal(t, http.StatusSeeOther, r.Status, r.Body)
	page = a.get("/suppliers/" + sid)
	assert.Contains(t, page.Body, "3.750,00")
	assert.Contains(t, a.get("/cheques?tab=endorsed").Body, "Ali Veli")
	// Ciro edilmiş çek ikinci kez ciro edilemez
	assert.Equal(t, http.StatusUnprocessableEntity, a.do(http.MethodPost, "/cheques/"+cid+"/endorse", url.Values{"supplier_id": {sid}}, nil).Status)

	// Ciro ödemesini sil → çek portföye döner
	var endorsed string
	for _, mm := range supplierPayDelRe.FindAllStringSubmatch(page.Body, -1) {
		endorsed = mm[1]
	}
	require.NotEmpty(t, endorsed)
	r = a.do(http.MethodPost, "/supplier-payments/"+endorsed+"/delete", url.Values{"supplier_id": {sid}}, nil)
	require.Equal(t, http.StatusSeeOther, r.Status)
	assert.Contains(t, a.get("/cheques").Body, "/cheques/"+cid)
}

func TestChequeAttachmentsAndFinanceReports(t *testing.T) {
	a := newTestApp(t)
	require.Equal(t, http.StatusSeeOther, a.login(testPassword).Status)
	custID := idFrom(t, a.do(http.MethodPost, "/customers", newCustomer("10000000146", "Ali Veli"), nil))
	cid := chequePayment(t, a, custID, "250,00")

	upload := func(name string, data []byte) string {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		require.NoError(t, mw.WriteField("type", "cheque"))
		require.NoError(t, mw.WriteField("id", cid))
		fw, err := mw.CreateFormFile("file", name)
		require.NoError(t, err)
		_, err = fw.Write(data)
		require.NoError(t, err)
		require.NoError(t, mw.Close())
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, a.srv.URL+"/attachments", &body)
		require.NoError(t, err)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		resp, err := a.client.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		out, _ := io.ReadAll(resp.Body)
		require.Equal(t, http.StatusOK, resp.StatusCode, string(out))
		return string(out)
	}

	// Metin dosyası reddedilir (uzantıya değil içeriğe bakılır)
	assert.Contains(t, upload("cek.png", []byte("bu bir resim degil")), "yalnızca fotoğraf")
	// Gerçek görsel eklenir, görüntülenir, silinir
	assert.Contains(t, upload("../cek foto.png", tinyPNG), "cek foto.png")
	m := attachmentLinkRe.FindStringSubmatch(a.get("/attachments/panel?type=cheque&id=" + cid).Body)
	require.NotNil(t, m)
	img := a.get("/attachments/" + m[1])
	assert.Equal(t, http.StatusOK, img.Status)
	assert.Equal(t, "image/png", img.Header.Get("Content-Type"))
	assert.Equal(t, http.StatusBadRequest, a.get("/attachments/panel?type=nope&id="+cid).Status)
	r := a.do(http.MethodPost, "/attachments/"+m[1]+"/delete", url.Values{}, nil)
	assert.Equal(t, http.StatusOK, r.Status)
	assert.Equal(t, http.StatusNotFound, a.get("/attachments/"+m[1]).Status)

	// Raporlar ve pano kutucukları
	home := a.get("/")
	assert.Contains(t, home.Body, "ÇEKLER VE BORÇLARIMIZ")
	cf := a.get("/reports/cashflow")
	assert.Equal(t, http.StatusOK, cf.Status)
	assert.Contains(t, cf.Body, "250,00")
	hb := a.get("/reports/habits")
	assert.Equal(t, http.StatusOK, hb.Status)
	assert.Contains(t, hb.Body, "Ali Veli")
	assert.Equal(t, http.StatusOK, a.get("/reports").Status)
}
