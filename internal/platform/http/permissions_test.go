package httpserver_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hesapkitap/internal/platform/auth"
)

const limitedEmail, limitedPassword = "kisitli@ornek.com", "kisitli-sifre-123"

func TestPermissionsLimitWhatAUserCanDo(t *testing.T) {
	admin := newTestApp(t)
	require.Equal(t, http.StatusSeeOther, admin.login(testPassword).Status)
	custID := idFrom(t, admin.do(http.MethodPost, "/customers", newCustomer("10000000146", "Ali Veli"), nil))
	draftID := readyDraftFor(t, admin, custID)
	admin.do(http.MethodPost, "/payments", url.Values{"customer_id": {custID}, "paid_at": {"2026-01-05"}, "amount": {"10,00"}, "method": {"CASH"}}, nil)

	// Kısıtlı kullanıcı: yalnızca taslak oluşturabilir ve tahsilat girebilir (varsayılan izinler değil, bilerek dar).
	_, err := admin.authSvc.CreateUserWithAccess(t.Context(), limitedEmail, limitedPassword, false,
		[]auth.Permission{auth.PermInvoiceDraft, auth.PermPaymentCreate})
	require.NoError(t, err)
	u := admin.other()
	require.Equal(t, http.StatusSeeOther, u.loginAs(limitedEmail, limitedPassword).Status)

	// Görüntüleme serbest
	assert.Equal(t, http.StatusOK, u.get("/invoices").Status)
	assert.Equal(t, http.StatusOK, u.get("/customers/"+custID).Status)
	pdfRes := u.get("/invoices/" + draftID + "/preview.pdf")
	assert.Equal(t, http.StatusOK, pdfRes.Status)
	assert.Equal(t, "application/pdf", pdfRes.Header.Get("Content-Type"))
	assert.Equal(t, "%PDF", pdfRes.Body[:4])

	// Yetkili olduğu işlemler
	assert.Equal(t, http.StatusOK, u.get("/invoices/new").Status)
	assert.Equal(t, http.StatusSeeOther, u.do(http.MethodPost, "/payments", url.Values{"customer_id": {custID}, "paid_at": {"2026-01-06"}, "amount": {"5,00"}, "method": {"CASH"}}, nil).Status)

	// Yetkisiz işlemler → 403, hiçbir şey değişmez
	forbidden := []struct{ method, path string }{
		{http.MethodPost, "/invoices/" + draftID + "/issue"},
		{http.MethodPost, "/invoices/" + draftID + "/delete"},
		{http.MethodPost, "/invoices/" + draftID + "/cancel"},
		{http.MethodGet, "/customers/new"},
		{http.MethodGet, "/products/new"},
		{http.MethodGet, "/customers/import"},
		{http.MethodGet, "/reports/sales"},
		{http.MethodGet, "/customers.xlsx"},
		{http.MethodGet, "/audit"},
		{http.MethodGet, "/users"},
	}
	for _, f := range forbidden {
		var form url.Values
		if f.method == http.MethodPost {
			form = url.Values{}
		}
		assert.Equal(t, http.StatusForbidden, u.do(f.method, f.path, form, nil).Status, f.method+" "+f.path)
	}
	assert.Zero(t, admin.jobs.count(), "kısıtlı kullanıcı fatura kesemedi")

	// Silme butonları ve menü öğeleri gizli
	inv := u.get("/invoices/" + draftID)
	assert.NotContains(t, inv.Body, "Faturayı Kes")
	assert.NotContains(t, inv.Body, "Taslağı sil")
	assert.Contains(t, inv.Body, "PDF önizleme")
	home := u.get("/")
	assert.Equal(t, http.StatusSeeOther, home.Status) // pano rapor izni ister → faturalara yönlenir
	assert.NotContains(t, u.get("/payments").Body, `/delete"`)
	assert.NotContains(t, u.get("/invoices").Body, "Kullanıcılar")

	// Yönetici izni verince hemen geçerli olur (oturum yenilemeden)
	users, err := admin.authSvc.ListUsers(t.Context())
	require.NoError(t, err)
	var limitedID string
	for _, usr := range users {
		if usr.Email == limitedEmail {
			limitedID = usr.ID.String()
		}
	}
	require.NotEmpty(t, limitedID)
	r := admin.do(http.MethodPost, "/users/"+limitedID, url.Values{"perm": {"invoice.draft", "invoice.delete"}, "active": {"1"}}, nil)
	assert.Equal(t, http.StatusSeeOther, r.Status)
	assert.Equal(t, http.StatusSeeOther, u.do(http.MethodPost, "/invoices/"+draftID+"/delete", url.Values{}, nil).Status)

	// Pasif kullanıcı oturumu anında düşer
	admin.do(http.MethodPost, "/users/"+limitedID, url.Values{"perm": {"invoice.draft"}}, nil) // active işaretsiz → pasif
	assert.Equal(t, http.StatusSeeOther, u.get("/invoices").Status)
	assert.Equal(t, http.StatusUnauthorized, u.loginAs(limitedEmail, limitedPassword).Status)
}

func TestAdminUserManagement(t *testing.T) {
	a := newTestApp(t)
	require.Equal(t, http.StatusSeeOther, a.login(testPassword).Status)

	assert.Contains(t, a.get("/users").Body, testEmail)
	// Yeni kullanıcı (yönetici olmayan)
	r := a.do(http.MethodPost, "/users", url.Values{"email": {"yeni@ornek.com"}, "password": {"yeni-kullanici-sifre"}, "perm": {"report.view"}}, nil)
	assert.Equal(t, http.StatusSeeOther, r.Status)
	assert.Contains(t, a.get("/users").Body, "yeni@ornek.com")
	// Kısa şifre reddedilir
	r = a.do(http.MethodPost, "/users", url.Values{"email": {"kisa@ornek.com"}, "password": {"kisa"}}, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)
	assert.True(t, strings.Contains(r.Body, "en az"))

	// Son yönetici kendini düşüremez (form değerleri yok sayılır, hata/işlem yok)
	users, err := a.authSvc.ListUsers(t.Context())
	require.NoError(t, err)
	var selfID string
	for _, u := range users {
		if u.Email == testEmail {
			selfID = u.ID.String()
		}
	}
	a.do(http.MethodPost, "/users/"+selfID, url.Values{}, nil)
	assert.Equal(t, http.StatusOK, a.get("/users").Status) // hâlâ yönetici
	require.ErrorIs(t, a.authSvc.UpdateAccess(t.Context(), users[0].ID, false, true, nil), auth.ErrLastAdmin)
}

// readyDraftFor saves a one-line draft for an existing customer and returns its id.
func readyDraftFor(t *testing.T, a *testApp, custID string) string {
	t.Helper()
	return idFrom(t, a.do(http.MethodPost, "/invoices", url.Values{
		"customer_id": {custID}, "issue_date": {"2026-11-13"}, "notes": {"ğüşiöçİ deneme notu"},
		"line_product": {""}, "line_name": {"Danışmanlık hizmeti"}, "line_unit": {"HUR"}, "line_qty": {"2"},
		"line_price": {"500,00"}, "line_discount": {""}, "line_tax": {"20"},
	}, nil))
}
