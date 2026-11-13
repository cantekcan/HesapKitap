package httpserver_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hesapkitap/internal/incoming"
	"hesapkitap/internal/invoice"
	"hesapkitap/internal/platform/auth"
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

func TestIncomingInvoicesListAndReply(t *testing.T) {
	a := newTestApp(t)
	require.Equal(t, http.StatusSeeOther, a.login(testPassword).Status)
	waiting := incoming.Invoice{UUID: uuid.New(), Number: "ABC2026000000001", Sender: "Tedarik Ltd.", SenderTaxID: "1234567890",
		Scenario: incoming.ScenarioCommercial, Payable: 123456, Currency: "TRY", IssuedAt: time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)}
	answered := incoming.Invoice{UUID: uuid.New(), Number: "XYZ2026000000002", Sender: "Başka A.Ş.", Scenario: incoming.ScenarioCommercial,
		Payable: 5000, Currency: "TRY", ReplyType: invoice.ReplyApproved}
	basic := incoming.Invoice{UUID: uuid.New(), Number: "BSC2026000000003", Sender: "Temel Fatura Ltd.", Scenario: incoming.ScenarioBasic, Payable: 100}
	a.incoming.items = []incoming.Invoice{waiting, answered, basic}

	// Varsayılan sekme: yalnızca yanıt bekleyen ticari faturalar
	p := a.get("/incoming")
	assert.Equal(t, http.StatusOK, p.Status)
	assert.Contains(t, p.Body, "Tedarik Ltd.")
	assert.Contains(t, p.Body, "1.234,56 ₺")
	assert.Contains(t, p.Body, "Yanıt bekliyor")
	assert.NotContains(t, p.Body, "Başka A.Ş.")
	assert.NotContains(t, p.Body, "Temel Fatura Ltd.")

	// Tümü: üçü de görünür; yanıtlanmış olanın durumu yazar
	all := a.get("/incoming?f=all")
	assert.Contains(t, all.Body, "Başka A.Ş.")
	assert.Contains(t, all.Body, "Temel Fatura Ltd.")
	assert.NotContains(t, a.get("/incoming?f=basic").Body, "Tedarik Ltd.")

	// Sayılar HTMX parçasıyla gelir
	tiles := a.get("/incoming/tiles")
	assert.Equal(t, http.StatusOK, tiles.Status)
	assert.Contains(t, tiles.Body, "Onay bekleyen")

	// PDF: geçici bağlantıya yönlenir
	pdf := a.get("/incoming/" + waiting.UUID.String() + "/pdf")
	assert.Equal(t, http.StatusSeeOther, pdf.Status)
	assert.Equal(t, "https://dosya.example.test/"+waiting.UUID.String()+".pdf", pdf.Header.Get("Location"))
	assert.Equal(t, http.StatusNotFound, a.get("/incoming/not-a-uuid/pdf").Status)

	// Yanıt: not zorunlu, yalnızca kabul/ret
	reply := func(answer, note string) result {
		return a.do(http.MethodPost, "/incoming/"+waiting.UUID.String()+"/reply", url.Values{"answer": {answer}, "note": {note}}, nil)
	}
	assert.Contains(t, reply("APPROVED", "  ").Body, "yanıt için not yazın")
	assert.Contains(t, reply("RETURNED", "x").Body, "yanıt yalnızca kabul veya ret olabilir")
	assert.Empty(t, a.incoming.replies, "geçersiz yanıtlar sağlayıcıya gitmedi")
	ok := reply("DENIED", "Sipariş vermedik")
	assert.Equal(t, http.StatusSeeOther, ok.Status)
	require.Len(t, a.incoming.replies, 1)
	assert.Equal(t, invoice.ReplyDenied, a.incoming.replies[0].Answer)
	assert.Equal(t, "Sipariş vermedik", a.incoming.replies[0].Note)
	assert.Contains(t, a.get("/audit").Body, "Gelen faturaya yanıt verildi")
}

func TestIncomingReplyNeedsPermissionAndProviderErrorsAreFriendly(t *testing.T) {
	a := newTestApp(t)
	require.Equal(t, http.StatusSeeOther, a.login(testPassword).Status)
	id := uuid.New()
	a.incoming.items = []incoming.Invoice{{UUID: id, Number: "N1", Sender: "Tedarik Ltd.", Scenario: incoming.ScenarioCommercial}}

	_, err := a.authSvc.CreateUserWithAccess(t.Context(), limitedEmail, limitedPassword, false, []auth.Permission{auth.PermInvoiceDraft})
	require.NoError(t, err)
	u := a.other()
	require.Equal(t, http.StatusSeeOther, u.loginAs(limitedEmail, limitedPassword).Status)
	list := u.get("/incoming")
	assert.Equal(t, http.StatusOK, list.Status, "görüntüleme serbest")
	assert.Contains(t, list.Body, "Tedarik Ltd.")
	assert.NotContains(t, list.Body, "Faturayı kabul et", "yetkisiz kullanıcı düğmeyi görmez")
	assert.Equal(t, http.StatusForbidden,
		u.do(http.MethodPost, "/incoming/"+id.String()+"/reply", url.Values{"answer": {"APPROVED"}, "note": {"x"}}, nil).Status)
	assert.Empty(t, a.incoming.replies)
	assert.Contains(t, a.get("/incoming").Body, "Faturayı kabul et", "yönetici düğmeyi görür")

	// Sağlayıcı hatası: sayfa açılır, ham hata sızmaz
	a.incoming.fail = errors.New("dial tcp 10.0.0.1: i/o timeout (gizli ayrıntı)")
	r := a.get("/incoming")
	assert.Equal(t, http.StatusBadGateway, r.Status)
	assert.Contains(t, r.Body, "şu anda alınamadı")
	assert.NotContains(t, r.Body, "gizli ayrıntı")
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
