package httpserver_test

import (
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hesapkitap/internal/invoice"
)

var (
	idRe        = regexp.MustCompile(`^/(?:products|invoices|customers)/([0-9a-f-]{36})(?:\?saved=1)?$`)
	hxPreview   = map[string]string{"HX-Request": "true"}
	productForm = func(name, price, tax string) url.Values {
		return url.Values{"name": {name}, "unit": {"C62"}, "unit_price": {price}, "tax_percent": {tax}, "active": {"1"}}
	}
)

func idFrom(t *testing.T, r result) string {
	t.Helper()
	require.Equal(t, http.StatusSeeOther, r.Status, r.Body)
	m := idRe.FindStringSubmatch(r.Header.Get("Location"))
	require.NotNil(t, m, r.Header.Get("Location"))
	return m[1]
}

func TestProductFlow(t *testing.T) {
	a := newTestApp(t)
	require.Equal(t, http.StatusSeeOther, a.login(testPassword).Status)

	assert.Contains(t, a.get("/products").Body, "Henüz ürün yok")

	// Geçersiz fiyat ve ad
	r := a.do(http.MethodPost, "/products", productForm("A", "abc", "20"), nil)
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)
	assert.Contains(t, r.Body, "Fiyatı 1.234,56 biçiminde girin")
	assert.Contains(t, r.Body, "Ürün adı 2 ile 200")

	// %0 KDV sunulmaz
	r = a.do(http.MethodPost, "/products", productForm("Hizmet", "10", "0"), nil)
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)

	f := productForm("Danışmanlık", "1.250,00", "20")
	f.Set("code", "DNS-1")
	id := idFrom(t, a.do(http.MethodPost, "/products", f, nil))

	// Aynı kod
	f2 := productForm("Başka", "5", "10")
	f2.Set("code", "DNS-1")
	r = a.do(http.MethodPost, "/products", f2, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)
	assert.Contains(t, r.Body, "kayıtlı başka bir ürün var")

	page := a.get("/products/" + id + "?saved=1")
	assert.Contains(t, page.Body, "1.250,00")
	assert.Contains(t, a.get("/products?q=danış").Body, "Danışmanlık")

	// Güncelle → pasif
	f = productForm("Danışmanlık Saati", "1.500,00", "20")
	f.Del("active")
	idFrom(t, a.do(http.MethodPost, "/products/"+id, f, nil))
	assert.Contains(t, a.get("/products?status=passive").Body, "Danışmanlık Saati")
	assert.NotContains(t, a.get("/products?status=active").Body, "Danışmanlık Saati")
}

func TestInvoiceDraftFlow(t *testing.T) {
	a := newTestApp(t)
	require.Equal(t, http.StatusSeeOther, a.login(testPassword).Status)

	custID := idFrom(t, a.do(http.MethodPost, "/customers", newCustomer("10000000146", "Ali Veli"), nil))
	prodID := idFrom(t, a.do(http.MethodPost, "/products", productForm("Kalem", "100,00", "20"), nil))

	assert.Contains(t, a.get("/invoices").Body, "Henüz fatura yok")
	newPage := a.get("/invoices/new")
	assert.Equal(t, http.StatusOK, newPage.Status)
	assert.Contains(t, newPage.Body, "Ali Veli")

	draft := func(lines ...[]string) url.Values {
		v := url.Values{"customer_id": {custID}, "issue_date": {"2026-11-13"}, "due_date": {"2026-12-13"}, "notes": {"deneme"}}
		for _, l := range lines { // ürün, ad, birim, miktar, fiyat, indirim, kdv
			v.Add("line_product", l[0])
			v.Add("line_name", l[1])
			v.Add("line_unit", l[2])
			v.Add("line_qty", l[3])
			v.Add("line_price", l[4])
			v.Add("line_discount", l[5])
			v.Add("line_tax", l[6])
		}
		return v
	}

	// Canlı toplam: sunucudaki InvoiceCalculator'dan. 1,5 × 100 = 150 + %20 KDV 30 = 180; 2 × 50 %10 → 100 + 10.
	prev := a.do(http.MethodPost, "/invoices/preview", draft(
		[]string{prodID, "Kalem", "C62", "1,5", "100,00", "", "20"},
		[]string{"", "Hizmet", "HUR", "2", "50,00", "", "10"},
	), hxPreview)
	assert.Equal(t, http.StatusOK, prev.Status)
	assert.Contains(t, prev.Body, "250,00 ₺") // KDV hariç
	assert.Contains(t, prev.Body, "40,00 ₺")  // toplam KDV (30 + 10)
	assert.Contains(t, prev.Body, "290,00 ₺") // ödenecek
	assert.NotContains(t, prev.Body, "<html")

	// Kalem yokken kayıt reddedilir
	r := a.do(http.MethodPost, "/invoices", draft(), nil)
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)
	assert.Contains(t, r.Body, "En az bir kalem")

	// İndirim brüt tutardan büyük
	r = a.do(http.MethodPost, "/invoices", draft([]string{"", "Kalem", "C62", "1", "10,00", "20,00", "20"}), nil)
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)
	assert.Contains(t, r.Body, "indirim satır tutarından büyük")

	// Kaydet
	id := idFrom(t, a.do(http.MethodPost, "/invoices", draft(
		[]string{prodID, "Kalem", "C62", "1,5", "100,00", "", "20"},
		[]string{"", "Hizmet", "HUR", "2", "50,00", "", "10"},
	), nil))
	page := a.get("/invoices/" + id + "?saved=1")
	assert.Equal(t, http.StatusOK, page.Status)
	assert.Contains(t, page.Body, "290,00 ₺")
	assert.Contains(t, page.Body, "Hizmet")
	list := a.get("/invoices")
	assert.Contains(t, list.Body, "Ali Veli")
	assert.Contains(t, list.Body, "290,00 ₺")
	assert.Contains(t, list.Body, "Taslak")

	// Ürün sonradan değişse de fatura kalemi değişmez (kopya)
	f := productForm("Kalem", "999,00", "20")
	idFrom(t, a.do(http.MethodPost, "/products/"+prodID, f, nil))
	assert.Contains(t, a.get("/invoices/"+id).Body, "290,00 ₺")

	// Güncelle: tek kalem
	idFrom(t, a.do(http.MethodPost, "/invoices/"+id, draft([]string{"", "Tek kalem", "C62", "1", "10,00", "", "20"}), nil))
	page = a.get("/invoices/" + id)
	assert.Contains(t, page.Body, "12,00 ₺")
	assert.NotContains(t, page.Body, "Hizmet")

	// Sil
	r = a.do(http.MethodPost, "/invoices/"+id+"/delete", url.Values{}, nil)
	assert.Equal(t, http.StatusSeeOther, r.Status)
	assert.Equal(t, http.StatusNotFound, a.get("/invoices/"+id).Status)
}

// readyDraft creates a customer and a one-line draft; it returns the invoice id.
func readyDraft(t *testing.T, a *testApp, taxID string) string {
	t.Helper()
	cust := newCustomer(taxID, "Ali Veli")
	if len(taxID) == 10 {
		cust.Set("kind", "COMPANY")
	}
	custID := idFrom(t, a.do(http.MethodPost, "/customers", cust, nil))
	return idFrom(t, a.do(http.MethodPost, "/invoices", url.Values{
		"customer_id": {custID}, "issue_date": {"2026-11-13"}, "notes": {"deneme"},
		"line_product": {""}, "line_name": {"Danışmanlık"}, "line_unit": {"HUR"}, "line_qty": {"2"},
		"line_price": {"500,00"}, "line_discount": {""}, "line_tax": {"20"},
	}, nil))
}

func TestIssueFlowNeverCreatesADoubleInvoice(t *testing.T) {
	a := newTestApp(t)
	require.Equal(t, http.StatusSeeOther, a.login(testPassword).Status)
	id := readyDraft(t, a, "10000000146")
	uid := uuid.MustParse(id)

	// "Faturayı Kes": taslak SENDING olur ve iş outbox'a yazılır. Çift tıklama ikinci iş yazmaz.
	assert.Equal(t, http.StatusSeeOther, a.do(http.MethodPost, "/invoices/"+id+"/issue", url.Values{}, nil).Status)
	assert.Equal(t, http.StatusSeeOther, a.do(http.MethodPost, "/invoices/"+id+"/issue", url.Values{}, nil).Status)
	assert.Equal(t, 1, a.jobs.count(), "çift tıklama ikinci gönderim işi yazmamalı")

	page := a.get("/invoices/" + id)
	assert.Contains(t, page.Body, "Gönderiliyor")
	assert.Contains(t, page.Body, "e-Arşiv", "firma e-Fatura mükellefi değil: e-Arşiv kesilir")
	assert.Contains(t, page.Body, `hx-trigger="every 4s"`)

	// Gönderilen fatura düzenlenemez ve silinemez (ALTIN KURAL 6).
	upd := a.do(http.MethodPost, "/invoices/"+id, url.Values{"customer_id": {uuid.NewString()}}, nil)
	assert.NotEqual(t, http.StatusSeeOther, upd.Status)
	assert.Equal(t, http.StatusNotFound, a.do(http.MethodPost, "/invoices/"+id+"/delete", url.Values{}, nil).Status)

	// Worker: sağlayıcı faturayı OLUŞTURUR ama cevap zaman aşımına uğrar → sonuç belirsiz, iş hata döner.
	a.provider.failAfterCreate = true
	require.Error(t, a.invoices.Send(t.Context(), uid))
	assert.Contains(t, a.get("/invoices/"+id).Body, "Gönderiliyor")
	assert.Equal(t, 1, a.provider.issues)

	// River işi tekrar çalıştırır: önce durum sorgulanır, sağlayıcıda VAR → yeniden gönderilmez.
	require.NoError(t, a.invoices.Send(t.Context(), uid))
	assert.Equal(t, 1, a.provider.issues, "belirsiz zaman aşımından sonra ikinci Issue çağrısı yapılmamalı")
	assert.Len(t, a.provider.created, 1)
	page = a.get("/invoices/" + id)
	assert.Contains(t, page.Body, "Kesildi")
	assert.NotContains(t, page.Body, `hx-trigger="every 4s"`)

	// İş üçüncü kez çalışsa da (en az bir kez) hiçbir şey olmaz.
	require.NoError(t, a.invoices.Send(t.Context(), uid))
	assert.Equal(t, 1, a.provider.issues)

	// Gönderilen istekte alıcı, kalem ve toplamlar doğru; ETTN gönderimden önce yazılmıştı.
	var req invoice.IssueRequest
	for _, r := range a.provider.created {
		req = r
	}
	assert.Equal(t, invoice.KindEArchive, req.Kind)
	assert.Equal(t, "EARSIVFATURA", req.Scenario)
	assert.Equal(t, "10000000146", req.Recipient.TaxID)
	assert.EqualValues(t, 120000, req.Totals.Payable)
	assert.Equal(t, id, req.LocalRef)

	// PDF ve paylaşım bağlantısı
	pdf := a.get("/invoices/" + id + "/pdf")
	assert.Equal(t, http.StatusSeeOther, pdf.Status)
	assert.Contains(t, pdf.Header.Get("Location"), "gecici")
	share := a.do(http.MethodPost, "/invoices/"+id+"/share", url.Values{}, hxPreview)
	assert.Contains(t, share.Body, "kalici")

	// e-Arşiv iptal
	assert.Equal(t, http.StatusSeeOther, a.do(http.MethodPost, "/invoices/"+id+"/cancel", url.Values{}, nil).Status)
	assert.Len(t, a.provider.cancelled, 1)
	assert.Contains(t, a.get("/invoices/"+id).Body, "İptal")
}

func TestIssueSuccessShowsInvoiceNumber(t *testing.T) {
	a := newTestApp(t)
	require.Equal(t, http.StatusSeeOther, a.login(testPassword).Status)
	id := readyDraft(t, a, "10000000146")
	a.do(http.MethodPost, "/invoices/"+id+"/issue", url.Values{}, nil)

	require.NoError(t, a.invoices.Send(t.Context(), uuid.MustParse(id)))
	page := a.get("/invoices/" + id)
	assert.Contains(t, page.Body, "ABC2026000000001")
	assert.Contains(t, a.get("/invoices").Body, "Kesildi")
}

func TestIssueRejectedByProviderReturnsToDraft(t *testing.T) {
	a := newTestApp(t)
	require.Equal(t, http.StatusSeeOther, a.login(testPassword).Status)
	id := readyDraft(t, a, "10000000146")
	a.do(http.MethodPost, "/invoices/"+id+"/issue", url.Values{}, nil)

	a.provider.reject = "Alıcı adresi geçersiz"
	require.NoError(t, a.invoices.Send(t.Context(), uuid.MustParse(id)))

	page := a.get("/invoices/" + id)
	assert.Contains(t, page.Body, "Alıcı adresi geçersiz")
	assert.Contains(t, page.Body, "Faturayı Kes", "kesin red: taslağa dönülür, düzeltilip yeniden denenebilir")
	assert.Contains(t, page.Body, "Taslağı kaydet")
}

func TestIssueWithoutProviderIsRefused(t *testing.T) {
	a := newTestApp(t)
	require.Equal(t, http.StatusSeeOther, a.login(testPassword).Status)
	id := readyDraft(t, a, "10000000146")
	a.invoices.Provider = nil

	r := a.do(http.MethodPost, "/invoices/"+id+"/issue", url.Values{}, nil)
	assert.Equal(t, http.StatusOK, r.Status)
	assert.Contains(t, r.Body, "ayarlı değil")
	assert.Equal(t, 0, a.jobs.count())
}
