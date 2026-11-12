package trendyol_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hesapkitap/internal/trendyol"
)

const taxpayersPath = "GET /api/invoice/taxpayers/{taxId}"

func TestSignInThenAuthenticatedRequest(t *testing.T) {
	f := newFakeAPI(t)
	token := defaultToken(t)
	f.signin(func(int32) string { return token })
	f.handle(taxpayersPath, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, token, r.Header.Get("x-access-token"))
		assert.Equal(t, "1234567890", r.PathValue("taxId"))
		assert.Equal(t, "false", r.URL.Query().Get("showDeleted"))
		writeJSON(w, `[{"taxId":"1234567890","alias":"urn:mail:defaultpk@x.com","title":"ÖRNEK A.Ş.","gibUserType":"x","aliasType":"INVOICE","postBoxType":"PK","aliasCreationTime":"2020-01-01T00:00:00Z","firstCreationTime":"2020-01-01T00:00:00Z","deletedAt":null}]`)
	})

	aliases, err := f.client().LookupTaxpayer(t.Context(), "1234567890")
	require.NoError(t, err)
	require.Len(t, aliases, 1)
	assert.Equal(t, "urn:mail:defaultpk@x.com", aliases[0].Alias)
	assert.Equal(t, "ÖRNEK A.Ş.", aliases[0].Title)
	assert.EqualValues(t, 1, f.logins.Load())
}

func TestLookupTaxpayerOnlyLiveInvoiceAliases(t *testing.T) {
	f := newFakeAPI(t)
	f.signin(func(int32) string { return defaultToken(t) })
	f.handle(taxpayersPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, `[
			{"taxId":"1","alias":"live","title":"T","aliasType":"INVOICE","deletedAt":null},
			{"taxId":"1","alias":"deleted","title":"T","aliasType":"INVOICE","deletedAt":"2024-01-01T00:00:00Z"},
			{"taxId":"1","alias":"irsaliye","title":"T","aliasType":"DESPATCH_ADVICE","deletedAt":null}]`)
	})

	aliases, err := f.client().LookupTaxpayer(t.Context(), "1")
	require.NoError(t, err)
	require.Len(t, aliases, 1)
	assert.Equal(t, "live", aliases[0].Alias)
}

func TestLookupTaxpayerNotFoundMeansNotEInvoiceUser(t *testing.T) {
	f := newFakeAPI(t)
	f.signin(func(int32) string { return defaultToken(t) })
	f.handle(taxpayersPath, func(w http.ResponseWriter, _ *http.Request) {
		problem(w, http.StatusNotFound, "Not Found", "taxpayer not found")
	})
	c := f.client()

	aliases, err := c.LookupTaxpayer(t.Context(), "11111111111")
	require.NoError(t, err)
	assert.Empty(t, aliases, "404 → e-Fatura mükellefi değil → e-Arşiv")

	_, err = c.TaxpayerRecords(t.Context(), "11111111111")
	assert.True(t, trendyol.IsNotFound(err))
}

func TestUnauthorizedTriggersOneReloginAndRetry(t *testing.T) {
	f := newFakeAPI(t)
	tokenA := fakeJWT(t, map[string]any{"sub": "7", "privs": map[string]any{"42": []string{"X"}}, "n": "A"})
	tokenB := fakeJWT(t, map[string]any{"sub": "7", "privs": map[string]any{"42": []string{"X"}}, "n": "B"})
	f.signin(func(n int32) string {
		if n == 1 {
			return tokenA
		}
		return tokenB
	})
	var seen []string
	f.handle(taxpayersPath, func(w http.ResponseWriter, r *http.Request) {
		tok := r.Header.Get("x-access-token")
		seen = append(seen, tok)
		if tok == tokenA {
			problem(w, http.StatusUnauthorized, "Unauthorized", "token expired")
			return
		}
		writeJSON(w, `[]`)
	})

	_, err := f.client().TaxpayerRecords(t.Context(), "1234567890")
	require.NoError(t, err)
	assert.EqualValues(t, 2, f.logins.Load(), "bir kez yeniden login")
	assert.Equal(t, []string{tokenA, tokenB}, seen, "aynı istek yeni token ile tekrarlandı")
}

func TestUnauthorizedAfterReloginGivesUp(t *testing.T) {
	f := newFakeAPI(t)
	f.signin(func(int32) string { return defaultToken(t) })
	f.handle(taxpayersPath, func(w http.ResponseWriter, _ *http.Request) {
		problem(w, http.StatusUnauthorized, "Unauthorized", "no")
	})

	_, err := f.client().TaxpayerRecords(t.Context(), "1234567890")
	require.Error(t, err)
	assert.ErrorIs(t, err, trendyol.ErrUnauthorized)
	assert.EqualValues(t, 2, f.logins.Load(), "tek seferlik relogin, döngü yok")
	assert.EqualValues(t, 2, f.calls.Load(), "istek en fazla bir kez tekrarlanır")
}

func TestSignInRejectedIsInvalidCredentialsWithoutRetry(t *testing.T) {
	f := newFakeAPI(t)
	f.signin(func(int32) string { return defaultToken(t) })

	_, err := f.client(func(c *trendyol.Config) { c.Password = "yanlis" }).TaxpayerRecords(t.Context(), "1")
	assert.ErrorIs(t, err, trendyol.ErrInvalidCredentials)
	assert.EqualValues(t, 1, f.logins.Load())
	assert.Zero(t, f.calls.Load())
}

func TestForbiddenMapsToIPNotWhitelisted(t *testing.T) {
	f := newFakeAPI(t)
	f.signin(func(int32) string { return defaultToken(t) })
	f.handle(taxpayersPath, func(w http.ResponseWriter, _ *http.Request) {
		// Cloudflare 403 sayfası JSON değil HTML'dir.
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "<html>Attention Required! | Cloudflare</html>")
	})

	_, err := f.client().TaxpayerRecords(t.Context(), "1")
	require.Error(t, err)
	assert.ErrorIs(t, err, trendyol.ErrIPNotWhitelisted)
	var apiErr *trendyol.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusForbidden, apiErr.Status)
	assert.Contains(t, trendyol.UserMessage(err), "IP")
	assert.NotContains(t, err.Error(), "Cloudflare", "HTML gövdesi hataya sızmaz")
}

func TestForbiddenAtSignInMapsToIPNotWhitelisted(t *testing.T) {
	f := newFakeAPI(t)
	f.mux.HandleFunc("POST /api/auth/signin", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	_, err := f.client().TaxpayerRecords(t.Context(), "1")
	assert.ErrorIs(t, err, trendyol.ErrIPNotWhitelisted)
}

func TestProblemJSONIsParsed(t *testing.T) {
	f := newFakeAPI(t)
	f.signin(func(int32) string { return defaultToken(t) })
	f.handle("POST /api/invoice/documents/earchive", func(w http.ResponseWriter, _ *http.Request) {
		problem(w, http.StatusUnprocessableEntity, "Validation Failed", "recipientInfo.city must not be blank")
	})

	_, err := f.client().CreateEArchive(t.Context(), &trendyol.InvoiceRequest{})
	var apiErr *trendyol.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, 422, apiErr.Status)
	assert.Equal(t, "Validation Failed", apiErr.Title)
	assert.Equal(t, "recipientInfo.city must not be blank", apiErr.Detail)
	assert.Equal(t, "/x", apiErr.Instance)
	msg := trendyol.UserMessage(err)
	assert.Contains(t, msg, "recipientInfo.city must not be blank")
	assert.Contains(t, msg, "422")
}

func TestSignInWithoutTokenHeaderFails(t *testing.T) {
	f := newFakeAPI(t)
	f.mux.HandleFunc("POST /api/auth/signin", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	_, err := f.client().TaxpayerRecords(t.Context(), "1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "x-access-token")
}

func TestMissingClaimErrorNamesTheClaimButNotTheToken(t *testing.T) {
	f := newFakeAPI(t)
	token := fakeJWT(t, map[string]any{"sub": "7", "privs": map[string]any{}}) // şirket yok
	f.signin(func(int32) string { return token })

	_, err := f.client().TaxpayerRecords(t.Context(), "1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "privs")
	assert.NotContains(t, err.Error(), token)
}

func TestIDsComeFromSubAndPrivsKey(t *testing.T) {
	f := newFakeAPI(t)
	f.signin(func(int32) string {
		return fakeJWT(t, map[string]any{"sub": "5", "privs": map[string]any{"99": []string{"A"}}})
	})
	f.handle("GET /api/invoice/documents/sent-reply/{companyId}/{uuid}", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "99", r.PathValue("companyId"), "companyId privs anahtarından gelir")
		writeJSON(w, `{"responseType":"APPROVED","note":"tamam","issuedAt":"2026-11-12T10:00:00Z"}`)
	})

	reply, err := f.client().SentReply(t.Context(), "uuid-1")
	require.NoError(t, err)
	assert.Equal(t, trendyol.ResponseApproved, reply.ResponseType)
}

func TestConcurrentFirstCallsSignInOnce(t *testing.T) {
	f := newFakeAPI(t)
	f.signin(func(int32) string { return defaultToken(t) })
	f.handle(taxpayersPath, func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, `[]`) })
	c := f.client()

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.TaxpayerRecords(context.Background(), "1")
			assert.NoError(t, err)
		}()
	}
	wg.Wait()
	assert.EqualValues(t, 1, f.logins.Load(), "eşzamanlı çağrılar tek login paylaşır")
}

func TestSecretsAreNeverLogged(t *testing.T) {
	f := newFakeAPI(t)
	token := defaultToken(t)
	f.signin(func(int32) string { return token })
	f.handle(taxpayersPath, func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, `[]`) })
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	_, err := f.client(func(c *trendyol.Config) { c.Logger = log }).TaxpayerRecords(t.Context(), "1")
	require.NoError(t, err)
	assert.NotEmpty(t, logs.String(), "istekler loglanmalı")
	assert.NotContains(t, logs.String(), token)
	assert.NotContains(t, logs.String(), testPassword)
	assert.NotContains(t, strings.ToLower(logs.String()), "x-access-token")
}

func TestNewValidatesConfig(t *testing.T) {
	_, err := trendyol.New(trendyol.Config{BaseURL: "not a url", Email: "a", Password: "b"})
	assert.Error(t, err)
	_, err = trendyol.New(trendyol.Config{BaseURL: "https://x.example", Email: "", Password: "b"})
	assert.Error(t, err)
}

func TestUserMessageForTimeout(t *testing.T) {
	msg := trendyol.UserMessage(context.DeadlineExceeded)
	assert.Contains(t, msg, "durumu sorgulayın", "belirsiz sonuçta tekrar göndermeden önce sorgulama hatırlatılır")
}
