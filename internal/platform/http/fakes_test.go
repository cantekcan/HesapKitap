package httpserver_test

import (
	"context"
	"errors"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"hesapkitap/internal/invoice"
)

// fakeProvider is an in-memory e-invoice provider: nothing leaves the process (ALTIN KURAL 4).
// 10000000146 → e-Fatura (1 etiket), 1234567890 → e-Fatura (2 etiket), others → e-Arşiv.
type fakeProvider struct {
	mu      sync.Mutex
	created map[uuid.UUID]invoice.IssueRequest
	issues  int // Issue çağrı sayısı (çift fatura kontrolü için)

	failAfterCreate bool   // sağlayıcı faturayı oluşturur ama cevap zaman aşımına uğrar
	reject          string // doluysa Issue kesin red döner
	cancelled       []uuid.UUID
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{created: map[uuid.UUID]invoice.IssueRequest{}}
}

func (f *fakeProvider) LookupTaxpayer(_ context.Context, taxID string) ([]invoice.TaxpayerAlias, error) {
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

func (f *fakeProvider) Issue(_ context.Context, req invoice.IssueRequest) (invoice.IssueResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.issues++
	if f.reject != "" {
		return invoice.IssueResult{}, &invoice.RejectedError{Message: f.reject}
	}
	f.created[req.ETTN] = req
	if f.failAfterCreate {
		f.failAfterCreate = false
		return invoice.IssueResult{}, errors.New("zaman aşımı")
	}
	return invoice.IssueResult{Number: "ABC2026000000001", Status: invoice.StatusCreated}, nil
}

func (f *fakeProvider) Status(_ context.Context, _ invoice.Kind, ettn uuid.UUID) (invoice.ProviderStatus, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.created[ettn]; ok {
		return invoice.StatusApproved, true, nil
	}
	return 0, false, nil
}

func (f *fakeProvider) Cancel(_ context.Context, ettn uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelled = append(f.cancelled, ettn)
	return nil
}

func (f *fakeProvider) DocumentURL(_ context.Context, _ invoice.Kind, ettn uuid.UUID, permanent bool) (string, error) {
	if permanent {
		return "https://fatura.example/kalici/" + ettn.String(), nil
	}
	return "https://fatura.example/gecici/" + ettn.String(), nil
}

// fakeEnqueuer records the send jobs the outbox would have written.
type fakeEnqueuer struct {
	mu  sync.Mutex
	ids []uuid.UUID
}

func (e *fakeEnqueuer) EnqueueSend(_ context.Context, _ pgx.Tx, id uuid.UUID) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ids = append(e.ids, id)
	return nil
}

func (e *fakeEnqueuer) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.ids)
}
