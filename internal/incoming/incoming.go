// Package incoming is the use case of invoices that suppliers sent us (gelen e-Fatura): list them, show the
// counts on the dashboard, open the PDF and answer commercial invoices (kabul / ret).
// It does not know Trendyol; internal/trendyol implements Provider.
package incoming

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"hesapkitap/internal/invoice"
)

// Invoice is one incoming invoice as the provider reports it.
type Invoice struct {
	UUID        uuid.UUID
	Number      string
	Sender      string // ünvan, yoksa ad soyad
	SenderTaxID string
	Scenario    string // TEMELFATURA | TICARIFATURA | ...
	TypeCode    string // SATIS | IADE | ...
	Status      invoice.ProviderStatus
	Payable     int64 // kuruş
	Currency    string
	IssuedAt    time.Time
	ReplyType   invoice.ReplyType // boş: henüz yanıtlanmadı
	ReplyNote   string
}

// IsCommercial reports whether the invoice is a ticari fatura (the only kind that is answered).
func (i Invoice) IsCommercial() bool { return i.Scenario == ScenarioCommercial }

// AwaitingReply reports whether a ticari fatura has not been answered yet.
func (i Invoice) AwaitingReply() bool { return i.IsCommercial() && i.ReplyType == "" }

// Scenario values used in filters. Plain ASCII constants (ALTIN KURAL 9).
const (
	ScenarioCommercial = "TICARIFATURA"
	ScenarioBasic      = "TEMELFATURA"
)

// Filter selects incoming invoices.
type Filter struct {
	Scenario string // "", TEMELFATURA veya TICARIFATURA
	Unread   bool   // yalnızca okunmamışlar
	Sender   string // gönderen ünvanı
	Page     int    // 0'dan başlar
	Size     int
}

// Page is one page of results.
type Page struct {
	Items []Invoice
	Total int
	Pages int
	Page  int
}

// Provider is the port to the e-invoice service provider's incoming-invoice endpoints.
type Provider interface {
	Search(ctx context.Context, f Filter) (Page, error)
	Reply(ctx context.Context, id uuid.UUID, answer invoice.ReplyType, note string) error
	// DocumentURL returns a temporary link to the PDF of the incoming invoice.
	DocumentURL(ctx context.Context, id uuid.UUID) (string, error)
}

// Counts are the numbers of the dashboard tiles.
type Counts struct {
	All           int
	Unread        int
	AwaitingReply int  // onay bekleyen ticari
	Approximate   bool // bekleyen sayısı taranan üst sınırı aştı (en az bu kadar)
}

// Errors.
var (
	ErrNoProvider = errors.New("e-Fatura sağlayıcı bağlantısı ayarlı değil")
	ErrBadAnswer  = errors.New("yanıt yalnızca kabul veya ret olabilir")
	ErrNoteNeeded = errors.New("yanıt için not yazın")
)

const (
	scanPageSize  = 100
	scanMaxPages  = 5
	countCacheTTL = 60 * time.Second
	maxNoteLength = 500
)

// Service wraps the provider with the rules of the screen.
type Service struct {
	Provider Provider

	mu      sync.Mutex
	cached  Counts
	cachedT time.Time
}

// List returns one page. "Onay bekleyen" is not a provider filter: the commercial invoices are scanned and the
// unanswered ones kept (bounded by scanMaxPages).
func (s *Service) List(ctx context.Context, f Filter, awaitingOnly bool) (Page, error) {
	if s.Provider == nil {
		return Page{}, ErrNoProvider
	}
	if f.Size <= 0 {
		f.Size = 20
	}
	if !awaitingOnly {
		return s.Provider.Search(ctx, f)
	}
	items, _, err := s.scanAwaiting(ctx, f)
	if err != nil {
		return Page{}, err
	}
	return Page{Items: items, Total: len(items), Pages: 1}, nil
}

// scanAwaiting reads commercial invoices page by page and keeps the unanswered ones.
func (s *Service) scanAwaiting(ctx context.Context, f Filter) (items []Invoice, truncated bool, err error) {
	f.Scenario, f.Size = ScenarioCommercial, scanPageSize
	for p := 0; p < scanMaxPages; p++ {
		f.Page = p
		pg, err := s.Provider.Search(ctx, f)
		if err != nil {
			return nil, false, err
		}
		for _, it := range pg.Items {
			if it.AwaitingReply() {
				items = append(items, it)
			}
		}
		if p+1 >= pg.Pages {
			return items, false, nil
		}
	}
	return items, true, nil
}

// Counts returns the tile numbers; the result is cached for a minute so the dashboard stays fast.
func (s *Service) Counts(ctx context.Context) (Counts, error) {
	if s.Provider == nil {
		return Counts{}, ErrNoProvider
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.cachedT) < countCacheTTL {
		return s.cached, nil
	}
	all, err := s.Provider.Search(ctx, Filter{Size: 1})
	if err != nil {
		return Counts{}, err
	}
	unread, err := s.Provider.Search(ctx, Filter{Size: 1, Unread: true})
	if err != nil {
		return Counts{}, err
	}
	awaiting, truncated, err := s.scanAwaiting(ctx, Filter{})
	if err != nil {
		return Counts{}, err
	}
	s.cached = Counts{All: all.Total, Unread: unread.Total, AwaitingReply: len(awaiting), Approximate: truncated}
	s.cachedT = time.Now()
	return s.cached, nil
}

// Reply answers a commercial invoice with kabul (APPROVED) or ret (DENIED) and a note.
func (s *Service) Reply(ctx context.Context, id uuid.UUID, answer invoice.ReplyType, note string) error {
	if s.Provider == nil {
		return ErrNoProvider
	}
	if answer != invoice.ReplyApproved && answer != invoice.ReplyDenied {
		return ErrBadAnswer
	}
	note = strings.TrimSpace(note)
	if note == "" {
		return ErrNoteNeeded
	}
	if len([]rune(note)) > maxNoteLength {
		note = string([]rune(note)[:maxNoteLength])
	}
	if err := s.Provider.Reply(ctx, id, answer, note); err != nil {
		return err
	}
	s.mu.Lock()
	s.cachedT = time.Time{}
	s.mu.Unlock()
	return nil
}

// DocumentURL returns the temporary PDF link.
func (s *Service) DocumentURL(ctx context.Context, id uuid.UUID) (string, error) {
	if s.Provider == nil {
		return "", ErrNoProvider
	}
	return s.Provider.DocumentURL(ctx, id)
}
