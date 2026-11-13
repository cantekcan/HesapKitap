package invoice

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// Errors of the issue flow. Messages are shown to the user.
var (
	ErrNoProvider     = errors.New("e-Fatura sağlayıcısı (Trendyol) ayarlı değil")
	ErrNotIssued      = errors.New("fatura henüz kesilmedi")
	ErrAliasNotChosen = errors.New("müşterinin birden fazla e-Fatura etiketi var; önce müşteri kartından etiketi seçin")
	ErrNoETTN         = errors.New("faturanın ETTN'si yok")
	ErrCannotCancel   = errors.New("bu fatura iptal edilemez")
	ErrAlreadyHandled = errors.New("fatura zaten gönderildi")
)

// RecipientSource gives the frozen recipient and the e-Fatura registration of a customer.
// The implementation refreshes the registration when it is older than 7 days (ROADMAP Faz 5).
type RecipientSource interface {
	ResolveRecipient(ctx context.Context, customerID uuid.UUID) (Recipient, Taxpayer, error)
}

// Settings are the issue options from the configuration.
type Settings struct {
	// EInvoiceEnabled: the company itself is an e-Fatura taxpayer, so e-Fatura may be issued to e-Fatura users.
	// If false, every invoice is an e-Arşiv.
	EInvoiceEnabled bool
	// EInvoiceScenario is TEMELFATURA or TICARIFATURA.
	EInvoiceScenario string
	// Prefix is the invoice series (3 chars) defined in the E-Faturam portal; empty = provider default series.
	Prefix string
}

// SendPlan is written together with the outbox job when the user presses "Faturayı Kes".
type SendPlan struct {
	ID          uuid.UUID
	Kind        Kind
	Scenario    string
	ETTN        uuid.UUID
	TargetAlias string
	Recipient   Recipient
}

// ResultUpdate records the provider's answer.
type ResultUpdate struct {
	Status         Status
	ProviderStatus *ProviderStatus
	Number         string
	LastError      string
}

// IssueStore is the persistence of the issue flow, implemented in internal/platform/db.
type IssueStore interface {
	// BeginSend atomically turns the DRAFT into SENDING and enqueues the send job in the same transaction
	// (transactional outbox). ErrNotEditable if the invoice is not a draft any more.
	BeginSend(ctx context.Context, p SendPlan) error
	RecordResult(ctx context.Context, id uuid.UUID, u ResultUpdate) error
	// RevertToDraft is used when the provider definitely refused the invoice.
	RevertToDraft(ctx context.Context, id uuid.UUID, reason string) error
	ListPending(ctx context.Context, limit int) ([]uuid.UUID, error)
}

// Issue validates the draft, decides e-Fatura/e-Arşiv, generates the ETTN and hands the invoice to the outbox.
// It does not call the provider: the River worker does (Send).
func (s *Service) Issue(ctx context.Context, id uuid.UUID) error {
	if s.Provider == nil {
		return ErrNoProvider
	}
	d, err := s.Store.Get(ctx, id)
	if err != nil {
		return err
	}
	if d.Status != StatusDraft {
		return ErrNotEditable
	}
	if errs := d.Validate(); len(errs) > 0 {
		for _, m := range errs {
			return errors.New(m)
		}
	}
	rec, tp, err := s.Recipients.ResolveRecipient(ctx, d.CustomerID)
	if err != nil {
		return err
	}

	plan := SendPlan{ID: id, Kind: KindEArchive, Scenario: "EARSIVFATURA", ETTN: uuid.New(), Recipient: rec}
	if s.Settings.EInvoiceEnabled && tp.IsEInvoiceUser {
		if tp.Alias == "" {
			return ErrAliasNotChosen
		}
		plan.Kind, plan.Scenario, plan.TargetAlias = KindEInvoice, s.Settings.EInvoiceScenario, tp.Alias
	}
	return s.Issues.BeginSend(ctx, plan)
}

// Send is the body of the River job. It is IDEMPOTENT: River runs it at least once, so before creating anything it
// asks the provider whether the ETTN already exists. It creates the invoice only on a clear "not found".
// A returned error makes River retry; a nil return means the job is finished.
func (s *Service) Send(ctx context.Context, id uuid.UUID) error {
	d, err := s.Store.Get(ctx, id)
	if err != nil {
		return err
	}
	if d.Status != StatusSending {
		return nil // zaten işlendi (tekrar çalışan iş)
	}
	if d.ETTN == uuid.Nil || d.Recipient == nil {
		return s.fail(ctx, id, ErrNoETTN)
	}

	info, found, err := s.Provider.Status(ctx, d.Kind, d.ETTN)
	if err != nil {
		return s.retry(ctx, id, fmt.Errorf("durum sorgulanamadı: %w", err))
	}
	if found {
		return s.record(ctx, id, info.Number, info.Status)
	}

	t, err := d.Totals()
	if err != nil {
		return s.fail(ctx, id, err)
	}
	res, err := s.Provider.Issue(ctx, IssueRequest{
		ETTN: d.ETTN, LocalRef: id.String(), Kind: d.Kind, Prefix: s.Settings.Prefix, Scenario: d.Scenario, TargetAlias: d.TargetAlias,
		IssueDate: d.IssueDate, Notes: d.Notes, Recipient: *d.Recipient, Lines: d.Lines, Totals: t,
	})
	var rejected *RejectedError
	switch {
	case errors.As(err, &rejected):
		// Kesin red: fatura oluşmadı, düzeltilebilsin diye taslağa dön.
		return s.Issues.RevertToDraft(ctx, id, rejected.Message)
	case err != nil:
		// Sonuç belirsiz (zaman aşımı vb.): işi tekrar çalıştır; ilk adım durumu sorgular, 404 değilse tekrar göndermez.
		return s.retry(ctx, id, err)
	}
	return s.record(ctx, id, res.Number, res.Status)
}

// retry stores the error for the screen and returns it so River retries the job.
func (s *Service) retry(ctx context.Context, id uuid.UUID, cause error) error {
	_ = s.Issues.RecordResult(ctx, id, ResultUpdate{Status: StatusSending, LastError: cause.Error()})
	return cause
}

func (s *Service) fail(ctx context.Context, id uuid.UUID, cause error) error {
	return s.Issues.RecordResult(ctx, id, ResultUpdate{Status: StatusErrored, LastError: cause.Error()})
}

// record maps the provider status to the local status and stores it.
func (s *Service) record(ctx context.Context, id uuid.UUID, number string, ps ProviderStatus) error {
	st := StatusIssued
	switch {
	case ps.IsFailure():
		st = StatusErrored
	case ps == StatusCancelled:
		st = StatusAnnulled
	}
	return s.Issues.RecordResult(ctx, id, ResultUpdate{Status: st, ProviderStatus: &ps, Number: number})
}

// Refresh polls the provider for issued invoices that are not in a final status (River periodic job).
func (s *Service) Refresh(ctx context.Context) error {
	if s.Provider == nil {
		return nil
	}
	ids, err := s.Issues.ListPending(ctx, 50)
	if err != nil {
		return err
	}
	var errs []error
	for _, id := range ids {
		d, err := s.Store.Get(ctx, id)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		info, found, err := s.Provider.Status(ctx, d.Kind, d.ETTN)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", id, err))
			continue
		}
		if !found {
			continue // sağlayıcıda görünmüyor: elle bakılmalı; durumu değiştirme
		}
		if err := s.record(ctx, id, info.Number, info.Status); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Cancel cancels an issued e-Arşiv invoice. e-Fatura cannot be cancelled (iade faturası, Faz 9).
func (s *Service) Cancel(ctx context.Context, id uuid.UUID) error {
	if s.Provider == nil {
		return ErrNoProvider
	}
	d, err := s.Store.Get(ctx, id)
	if err != nil {
		return err
	}
	if d.Kind != KindEArchive {
		return ErrNotEArchive
	}
	if d.Status != StatusIssued {
		return ErrCannotCancel
	}
	if err := s.Provider.Cancel(ctx, d.ETTN); err != nil {
		return err
	}
	ps := StatusCancelled
	return s.Issues.RecordResult(ctx, id, ResultUpdate{Status: StatusAnnulled, ProviderStatus: &ps, Number: d.Number})
}

// ErrCannotResend is returned when the invoice is not an e-Fatura that failed at the provider.
var ErrCannotResend = errors.New("yalnızca GİB'de hata alan e-Fatura yeniden gönderilebilir")

// CanResend reports whether the invoice is an e-Fatura that failed at the provider (doküman hatası / hatalı).
func (d Draft) CanResend() bool {
	return d.Kind == KindEInvoice && d.Status == StatusIssued && d.ProviderStatus != nil &&
		(*d.ProviderStatus == StatusDocumentError || *d.ProviderStatus == StatusFailed)
}

// Reply asks for the recipient's answer to a commercial e-Fatura.
func (s *Service) Reply(ctx context.Context, id uuid.UUID) (Reply, bool, error) {
	if s.Provider == nil {
		return Reply{}, false, ErrNoProvider
	}
	d, err := s.Store.Get(ctx, id)
	if err != nil {
		return Reply{}, false, err
	}
	if d.Kind != KindEInvoice || d.Status != StatusIssued {
		return Reply{}, false, ErrNotIssued
	}
	return s.Provider.SentReply(ctx, d.ETTN)
}

// Resend sends a failed e-Fatura again to the same alias and records the provider's new status.
func (s *Service) Resend(ctx context.Context, id uuid.UUID) error {
	if s.Provider == nil {
		return ErrNoProvider
	}
	d, err := s.Store.Get(ctx, id)
	if err != nil {
		return err
	}
	if !d.CanResend() {
		return ErrCannotResend
	}
	if err := s.Provider.Resend(ctx, d.ETTN, ""); err != nil {
		return err
	}
	info, found, err := s.Provider.Status(ctx, d.Kind, d.ETTN)
	if err != nil || !found {
		return err
	}
	return s.record(ctx, id, info.Number, info.Status)
}

// DocumentURL returns the PDF link of an issued invoice.
func (s *Service) DocumentURL(ctx context.Context, id uuid.UUID, permanent bool) (string, error) {
	if s.Provider == nil {
		return "", ErrNoProvider
	}
	d, err := s.Store.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if d.ETTN == uuid.Nil || (d.Status != StatusIssued && d.Status != StatusAnnulled) {
		return "", ErrNotIssued
	}
	return s.Provider.DocumentURL(ctx, d.Kind, d.ETTN, permanent)
}

// RefreshOne asks the provider about one issued invoice that is not in a final status, so an open invoice page
// shows "Onaylandı" within seconds instead of waiting for the periodic job.
func (s *Service) RefreshOne(ctx context.Context, id uuid.UUID) error {
	if s.Provider == nil {
		return nil
	}
	d, err := s.Store.Get(ctx, id)
	if err != nil {
		return err
	}
	if d.Status != StatusIssued || (d.ProviderStatus != nil && d.ProviderStatus.IsFinal()) {
		return nil
	}
	info, found, err := s.Provider.Status(ctx, d.Kind, d.ETTN)
	if err != nil || !found {
		return err
	}
	return s.record(ctx, id, info.Number, info.Status)
}

// IsPolling reports whether an invoice page should keep refreshing itself.
func (d Draft) IsPolling() bool {
	switch d.Status {
	case StatusSending:
		return true
	case StatusIssued:
		return d.ProviderStatus == nil || !d.ProviderStatus.IsFinal()
	}
	return false
}
