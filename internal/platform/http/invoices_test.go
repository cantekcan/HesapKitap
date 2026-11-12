package httpserver_test

import (
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
