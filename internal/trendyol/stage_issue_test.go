//go:build stage

package trendyol_test

// Gerçek stage'de 1 kalemli e-Arşiv keser, durumu izler, PDF bağlantısını alır ve faturayı iptal eder.
// Kontör harcayabileceği için YALNIZCA açıkça istenirse çalışır:
//
//	TRENDYOL_STAGE_ISSUE=1 go test -tags=stage -run TestStageIssueEArchive -v ./internal/trendyol/
//
// Alıcı olarak test TCKN'si kullanılır; gerçek bir kişiye fatura gitmez (stage).

import (
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"hesapkitap/internal/invoice"
)

func TestStageIssueEArchive(t *testing.T) {
	if os.Getenv("TRENDYOL_STAGE_ISSUE") != "1" {
		t.Skip("TRENDYOL_STAGE_ISSUE=1 verilmedi (fatura kesmek kontör harcayabilir)")
	}
	c := stageClient(t)
	ctx := t.Context()

	lines := []invoice.Line{{ItemName: "Stage test hizmeti", Unit: "C62", Quantity: decimal.NewFromInt(1), UnitPrice: 10000, TaxPercent: 20}}
	totals, err := invoice.InvoiceCalculator{}.Calculate([]invoice.LineInput{lines[0].Input()})
	require.NoError(t, err)
	ettn := uuid.New()

	res, err := c.Issue(ctx, invoice.IssueRequest{
		ETTN: ettn, LocalRef: "stage-test-" + ettn.String()[:8], Kind: invoice.KindEArchive, Scenario: "EARSIVFATURA",
		IssueDate: time.Now(), Prefix: os.Getenv("TRENDYOL_STAGE_PREFIX"), Recipient: invoice.Recipient{Person: true, TaxID: "10000000146", Name: "Test Kullanıcı", City: "İstanbul", District: "Kadıköy"},
		Lines: lines, Totals: totals,
	})
	require.NoError(t, err, "fatura kesilemedi")
	t.Logf("fatura no: %q, durum: %s", res.Number, res.Status)

	var status invoice.ProviderStatus
	for range 30 {
		info, found, err := c.Status(ctx, invoice.KindEArchive, ettn)
		require.NoError(t, err)
		require.True(t, found, "kesilen fatura durum sorgusunda bulunamadı")
		status = info.Status
		t.Logf("durum: %s (%d), no: %s", status, int(status), info.Number)
		if status.IsFinal() {
			break
		}
		time.Sleep(2 * time.Second)
	}
	t.Logf("son durum: %s (%d)", status, int(status))
	require.False(t, status.IsFailure(), "fatura hata durumuna düştü")

	link, err := c.DocumentURL(ctx, invoice.KindEArchive, ettn, false)
	require.NoError(t, err)
	require.NotEmpty(t, link)
	t.Log("PDF bağlantısı alındı")

	require.NoError(t, c.Cancel(ctx, ettn), "iptal edilemedi")
}
