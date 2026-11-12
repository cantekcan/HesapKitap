package httpserver_test

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hesapkitap/internal/invoice"
)

// stubProvider answers taxpayer lookups without any network: ...146 e-Fatura (1 etiket),
// 1234567890 e-Fatura (2 etiket), diğerleri e-Arşiv.
type stubProvider struct{}

func (stubProvider) LookupTaxpayer(_ context.Context, taxID string) ([]invoice.TaxpayerAlias, error) {
	switch taxID {
	case "10000000146":
		return []invoice.TaxpayerAlias{{TaxID: taxID, Alias: "urn:mail:tek@ornek.com", Title: "Tek Etiket"}}, nil
	case "1234567890":
		return []invoice.TaxpayerAlias{
			{TaxID: taxID, Alias: "urn:mail:a@ornek.com", Title: "A"},
			{TaxID: taxID, Alias: "urn:mail:b@ornek.com", Title: "B"},
		}, nil
	}
	return nil, nil
}

func newCustomer(taxID, name string) url.Values {
	return url.Values{
		"kind": {"PERSON"}, "tax_id": {taxID}, "name": {name}, "city": {"İstanbul"},
		"email": {"ALI@Ornek.com"}, "opening_balance": {"1.250,50"}, "active": {"1"},
	}
}

var customerIDRe = regexp.MustCompile(`^/customers/([0-9a-f-]{36})\?saved=1$`)

func TestCustomerFlow(t *testing.T) {
	a := newTestApp(t)
	require.Equal(t, http.StatusSeeOther, a.login(testPassword).Status)

	// Boş liste
	assert.Contains(t, a.get("/customers").Body, "Henüz müşteri yok")

	// Geçersiz TCKN → 422 ve alan hatası, kayıt yok
	r := a.do(http.MethodPost, "/customers", newCustomer("12345678901", "Ali Veli"), nil)
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)
	assert.Contains(t, r.Body, "TCKN")

	// Geçerli kayıt → yönlendirme
	r = a.do(http.MethodPost, "/customers", newCustomer("10000000146", "Ali Veli"), nil)
	require.Equal(t, http.StatusSeeOther, r.Status)
	m := customerIDRe.FindStringSubmatch(r.Header.Get("Location"))
	require.NotNil(t, m, r.Header.Get("Location"))
	id := m[1]

	// Aynı TCKN ikinci kez → anlaşılır hata
	r = a.do(http.MethodPost, "/customers", newCustomer("10000000146", "Başka Kişi"), nil)
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)
	assert.Contains(t, r.Body, "kayıtlı başka bir müşteri var")

	// Detay: e-posta küçültüldü, bakiye biçimli
	page := a.get("/customers/" + id + "?saved=1")
	assert.Equal(t, http.StatusOK, page.Status)
	assert.Contains(t, page.Body, "ali@ornek.com")
	assert.Contains(t, page.Body, "1.250,50")
	assert.Contains(t, page.Body, "Sorgulanmadı")

	// Arama: tam sayfa ve HTMX parçası
	assert.Contains(t, a.get("/customers?q=veli").Body, "Ali Veli")
	assert.NotContains(t, a.get("/customers?q=yokboyle").Body, "Ali Veli")
	frag := a.do(http.MethodGet, "/customers?q=ali", nil, map[string]string{"HX-Request": "true", "HX-Target": "customer-list"})
	assert.Contains(t, frag.Body, "Ali Veli")
	assert.NotContains(t, frag.Body, "<html")

	// Mükellef sorgusu: tek etiket doğrudan kaydedilir
	r = a.do(http.MethodPost, "/customers/"+id+"/taxpayer", url.Values{}, map[string]string{"HX-Request": "true"})
	assert.Equal(t, http.StatusOK, r.Status)
	assert.Contains(t, r.Body, "urn:mail:tek@ornek.com")
	assert.Contains(t, a.get("/customers/"+id).Body, "e-Fatura")

	// Güncelleme
	upd := newCustomer("10000000146", "Ali Veli Yeni")
	upd.Del("active") // pasif yap
	r = a.do(http.MethodPost, "/customers/"+id, upd, nil)
	assert.Equal(t, http.StatusSeeOther, r.Status)
	assert.Contains(t, a.get("/customers?status=passive").Body, "Ali Veli Yeni")
	assert.NotContains(t, a.get("/customers?status=active").Body, "Ali Veli Yeni")
}

func TestCustomerTaxpayerMultipleAliasesNeedChoice(t *testing.T) {
	a := newTestApp(t)
	require.Equal(t, http.StatusSeeOther, a.login(testPassword).Status)

	r := a.do(http.MethodPost, "/customers", url.Values{
		"kind": {"COMPANY"}, "tax_id": {"1234567890"}, "name": {"Çift Etiket A.Ş."}, "city": {"Ankara"}, "active": {"1"},
	}, nil)
	require.Equal(t, http.StatusSeeOther, r.Status)
	id := customerIDRe.FindStringSubmatch(r.Header.Get("Location"))[1]
	hx := map[string]string{"HX-Request": "true"}

	r = a.do(http.MethodPost, "/customers/"+id+"/taxpayer", url.Values{}, hx)
	assert.Contains(t, r.Body, "Birden fazla posta kutusu")
	assert.Contains(t, r.Body, "urn:mail:b@ornek.com")
	assert.Contains(t, a.get("/customers/"+id).Body, "Sorgulanmadı", "seçim yapılana kadar kaydedilmez")

	r = a.do(http.MethodPost, "/customers/"+id+"/taxpayer/select", url.Values{"alias": {"urn:mail:b@ornek.com"}}, hx)
	assert.Equal(t, http.StatusOK, r.Status)
	assert.Contains(t, a.get("/customers/"+id).Body, "urn:mail:b@ornek.com")
}

func TestCustomerNotFound(t *testing.T) {
	a := newTestApp(t)
	require.Equal(t, http.StatusSeeOther, a.login(testPassword).Status)
	assert.Equal(t, http.StatusNotFound, a.get("/customers/00000000-0000-0000-0000-000000000000").Status)
	assert.Equal(t, http.StatusNotFound, a.get("/customers/degil").Status)
}
