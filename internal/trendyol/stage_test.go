//go:build stage

package trendyol_test

// Bu testler GERÇEK Trendyol stage ortamına istek atar (ALTIN KURAL 4: varsayılan `go test` bunları derlemez).
// Çalıştırma:  task test:stage   (veya: go test -tags=stage ./internal/trendyol/...)
//
// Gerekli ortam değişkenleri (repo kökündeki .env dosyasından da okunur, o dosya gitignore'dadır):
//   TRENDYOL_BASE_URL   stage adresi (host adında "stage" geçmeli; canlıya karşı çalışmayı reddeder)
//   TRENDYOL_EMAIL, TRENDYOL_PASSWORD
//   TRENDYOL_STAGE_TAXID   (isteğe bağlı) mükellef sorgusu için VKN/TCKN
//
// Testler token, şifre veya claim DEĞERİ yazdırmaz.

import (
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/require"

	"hesapkitap/internal/trendyol"
)

func stageClient(t *testing.T) *trendyol.Client {
	t.Helper()
	_ = godotenv.Load("../../.env") // yoksa sorun değil; ortamdaki değişkenler kullanılır

	base, email, pass := os.Getenv("TRENDYOL_BASE_URL"), os.Getenv("TRENDYOL_EMAIL"), os.Getenv("TRENDYOL_PASSWORD")
	if base == "" || email == "" || pass == "" {
		t.Skip("TRENDYOL_BASE_URL, TRENDYOL_EMAIL ve TRENDYOL_PASSWORD tanımlı değil (.env)")
	}
	u, err := url.Parse(base)
	require.NoError(t, err)
	if !strings.Contains(strings.ToLower(u.Hostname()), "stage") {
		t.Fatalf("stage testi stage olmayan bir adrese yönlendirilmiş (%s); canlıya karşı çalıştırılmaz", u.Hostname())
	}
	c, err := trendyol.New(trendyol.Config{BaseURL: base, Email: email, Password: pass})
	require.NoError(t, err)
	return c
}

func TestStageSignIn(t *testing.T) {
	c := stageClient(t)
	ctx := t.Context()

	err := c.Authenticate(ctx)
	if err != nil {
		// Kullanıcıya anlaşılır mesajı göster (IP / şifre / claim adı sorunları burada ayrışır).
		t.Fatalf("stage girişi başarısız: %s\n(ayrıntı: %v)", trendyol.UserMessage(err), err)
	}
}

func TestStageTaxpayerLookup(t *testing.T) {
	c := stageClient(t)
	taxID := os.Getenv("TRENDYOL_STAGE_TAXID")
	if taxID == "" {
		t.Skip("TRENDYOL_STAGE_TAXID tanımlı değil")
	}
	aliases, err := c.LookupTaxpayer(t.Context(), taxID)
	require.NoError(t, err, trendyol.UserMessage(err))
	if len(aliases) == 0 {
		t.Logf("%s e-Fatura mükellefi değil (404) → e-Arşiv kesilir", taxID)
		return
	}
	t.Logf("%s e-Fatura mükellefi: %d etiket", taxID, len(aliases))
}
