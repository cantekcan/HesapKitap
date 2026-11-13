package httpserver_test

import (
	"context"
	"net/http"
	"net/url"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeMailer records messages instead of sending them (ALTIN KURAL 4).
type fakeMailer struct {
	mu   sync.Mutex
	sent []struct{ To, Subject, Body string }
}

func (m *fakeMailer) Send(_ context.Context, to, subject, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, struct{ To, Subject, Body string }{to, subject, body})
	return nil
}

func TestDashboardTilesAndInvoiceTabs(t *testing.T) {
	a := newTestApp(t)
	require.Equal(t, http.StatusSeeOther, a.login(testPassword).Status)
	id := readyDraft(t, a, "10000000146")

	// Taslak: panodaki kutucukta ve sekmede 1
	home := a.get("/")
	assert.Equal(t, http.StatusOK, home.Status)
	assert.Contains(t, home.Body, "E-ARŞİV")
	assert.Contains(t, home.Body, "E-FATURA")
	assert.Contains(t, home.Body, `href="/invoices?status=DRAFT"`)
	assert.Contains(t, home.Body, `href="/invoices?kind=EARCHIVE&amp;status=ISSUED"`)

	list := a.get("/invoices")
	assert.Contains(t, list.Body, `role="tablist"`)
	assert.Contains(t, list.Body, "Taslak")

	// Kes → e-Arşiv olarak kesildi: tür filtresi çalışır
	require.Equal(t, http.StatusSeeOther, a.do(http.MethodPost, "/invoices/"+id+"/issue", url.Values{}, nil).Status)
	require.NoError(t, a.invoices.Send(t.Context(), uuid.MustParse(id)))
	assert.Contains(t, a.get("/invoices?kind=EARCHIVE").Body, `data-href="/invoices/`+id+`"`)
	assert.NotContains(t, a.get("/invoices?kind=EINVOICE").Body, `data-href="/invoices/`+id+`"`)
	assert.NotContains(t, a.get("/invoices?status=DRAFT").Body, `data-href="/invoices/`+id+`"`)
	assert.Contains(t, a.get("/invoices?kind=EARCHIVE&status=ISSUED").Body, `data-href="/invoices/`+id+`"`)
	// Geçersiz değerler yok sayılır
	assert.Equal(t, http.StatusOK, a.get("/invoices?kind=x&status=y").Status)
}

func TestIncomingPageIsHonestAboutMissingAPI(t *testing.T) {
	a := newTestApp(t)
	require.Equal(t, http.StatusSeeOther, a.login(testPassword).Status)
	p := a.get("/incoming")
	assert.Equal(t, http.StatusOK, p.Status)
	assert.Contains(t, p.Body, "Gelen faturalar henüz bağlanmadı")
	assert.Contains(t, p.Body, "Gelen faturalar", "menüde")
}

func TestEmailInvoiceLinkToCustomer(t *testing.T) {
	a := newTestApp(t)
	require.Equal(t, http.StatusSeeOther, a.login(testPassword).Status)
	id := readyDraft(t, a, "10000000146")
	require.Equal(t, http.StatusSeeOther, a.do(http.MethodPost, "/invoices/"+id+"/issue", url.Values{}, nil).Status)
	require.NoError(t, a.invoices.Send(t.Context(), uuid.MustParse(id)))

	custID := a.customerIDOf(id)
	f := newCustomer("10000000146", "Ali Veli")

	// Müşteri e-postası yokken uyarır, göndermez
	f.Set("email", "")
	require.Equal(t, http.StatusSeeOther, a.do(http.MethodPost, "/customers/"+custID, f, nil).Status)
	r := a.do(http.MethodPost, "/invoices/"+id+"/email", url.Values{}, nil)
	assert.Contains(t, r.Body, "e-posta adresi kayıtlı değil")
	assert.Empty(t, a.mailer.sent)

	// E-posta ekle ve gönder
	assert.Contains(t, a.get("/invoices/"+id).Body, "Müşteriye e-posta gönder")
	f.Set("email", "ali@ornek.com")
	require.Equal(t, http.StatusSeeOther, a.do(http.MethodPost, "/customers/"+custID, f, nil).Status)

	r = a.do(http.MethodPost, "/invoices/"+id+"/email", url.Values{}, nil)
	assert.Equal(t, http.StatusOK, r.Status)
	assert.Contains(t, r.Body, "E-posta gönderildi")
	require.Len(t, a.mailer.sent, 1)
	assert.Equal(t, "ali@ornek.com", a.mailer.sent[0].To)
	assert.Contains(t, a.mailer.sent[0].Body, "https://fatura.example/kalici/")
}

func TestResendAndReplyEndpointsGuardState(t *testing.T) {
	a := newTestApp(t)
	require.Equal(t, http.StatusSeeOther, a.login(testPassword).Status)
	id := readyDraft(t, a, "10000000146")
	require.Equal(t, http.StatusSeeOther, a.do(http.MethodPost, "/invoices/"+id+"/issue", url.Values{}, nil).Status)
	require.NoError(t, a.invoices.Send(t.Context(), uuid.MustParse(id)))

	// e-Arşiv fatura yeniden gönderilemez, yanıt sorgulanamaz
	r := a.do(http.MethodPost, "/invoices/"+id+"/resend", url.Values{}, nil)
	assert.Contains(t, r.Body, "hata alan e-Fatura")
	assert.Contains(t, a.get("/invoices/"+id+"/reply").Body, "text-rose-700")
	assert.NotContains(t, a.get("/invoices/"+id).Body, "Yeniden gönder")
}
