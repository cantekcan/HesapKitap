package httpserver_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPaymentFlow(t *testing.T) {
	a := newTestApp(t)
	require.Equal(t, http.StatusSeeOther, a.login(testPassword).Status)

	custID := idFrom(t, a.do(http.MethodPost, "/customers", newCustomer("10000000146", "Ali Veli"), nil))
	// Açılış bakiyesi 1.250,50 → bakiye panelinde görünür
	page := a.get("/customers/" + custID)
	assert.Contains(t, page.Body, "Cari durum")
	assert.Contains(t, page.Body, "1.250,50")
	assert.Contains(t, page.Body, "Açılış bakiyesi")

	// Hatalı tutar ve gelecek tarih reddedilir
	r := a.do(http.MethodPost, "/payments", url.Values{"customer_id": {custID}, "paid_at": {"2999-01-01"}, "amount": {"abc"}, "method": {"CASH"}}, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, r.Status)
	assert.Contains(t, r.Body, "1.234,56")
	assert.Contains(t, r.Body, "gelecekte")

	// Kısmi tahsilat
	r = a.do(http.MethodPost, "/payments", url.Values{"customer_id": {custID}, "paid_at": {"2026-01-05"}, "amount": {"250,50"}, "method": {"TRANSFER"}, "note": {"ocak havalesi"}}, nil)
	require.Equal(t, http.StatusSeeOther, r.Status)
	page = a.get("/customers/" + custID + "?paid=1")
	assert.Contains(t, page.Body, "Tahsilat kaydedildi")
	assert.Contains(t, page.Body, "1.000,00")

	list := a.get("/payments")
	assert.Contains(t, list.Body, "Ali Veli")
	assert.Contains(t, list.Body, "ocak havalesi")

	st := a.get("/customers/" + custID + "/statement")
	assert.Equal(t, http.StatusOK, st.Status)
	assert.Contains(t, st.Body, "Açılış bakiyesi")
	assert.Contains(t, st.Body, "Havale/EFT")
	assert.Equal(t, http.StatusNotFound, a.get("/customers/00000000-0000-0000-0000-000000000000/statement").Status)
}

func TestDashboardReportsAndExcelExports(t *testing.T) {
	a := newTestApp(t)
	require.Equal(t, http.StatusSeeOther, a.login(testPassword).Status)
	custID := idFrom(t, a.do(http.MethodPost, "/customers", newCustomer("10000000146", "Ali Veli"), nil))
	a.do(http.MethodPost, "/payments", url.Values{"customer_id": {custID}, "paid_at": {"2026-01-05"}, "amount": {"250,50"}, "method": {"CASH"}}, nil)

	home := a.get("/")
	assert.Equal(t, http.StatusOK, home.Status)
	assert.Contains(t, home.Body, "Toplam alacak")
	assert.Contains(t, home.Body, "<svg")

	for _, p := range []string{"/reports", "/reports/sales?from=2026-01-01&to=2026-12-31", "/reports/aging"} {
		r := a.get(p)
		assert.Equal(t, http.StatusOK, r.Status, p)
	}
	for _, p := range []string{"/reports/sales.xlsx", "/reports/aging.xlsx", "/payments.xlsx", "/customers.xlsx", "/customers/" + custID + "/statement.xlsx"} {
		r := a.get(p)
		assert.Equal(t, http.StatusOK, r.Status, p)
		assert.Contains(t, r.Header.Get("Content-Type"), "spreadsheetml", p)
		assert.True(t, len(r.Body) > 2 && r.Body[:2] == "PK", p) // xlsx bir zip'tir
	}
}
