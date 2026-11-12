package queue

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

// Sender is what the workers need from the invoice domain. Both methods are idempotent.
type Sender interface {
	Send(ctx context.Context, id uuid.UUID) error
	Refresh(ctx context.Context) error
}

// SendInvoiceArgs is the payload of the send job (River kind "send_invoice").
type SendInvoiceArgs struct {
	InvoiceID uuid.UUID `json:"invoice_id"`
}

func (SendInvoiceArgs) Kind() string { return "send_invoice" }

// InsertOpts: bir fatura için aynı anda tek gönderim işi (çift kuyruklamaya karşı ikinci güvence).
func (SendInvoiceArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}, MaxAttempts: 25}
}

type sendWorker struct {
	river.WorkerDefaults[SendInvoiceArgs]
	sender Sender
}

func (w *sendWorker) Work(ctx context.Context, job *river.Job[SendInvoiceArgs]) error {
	return w.sender.Send(ctx, job.Args.InvoiceID)
}

// RefreshStatusArgs is the periodic status polling job.
type RefreshStatusArgs struct{}

func (RefreshStatusArgs) Kind() string { return "refresh_invoice_status" }

type refreshWorker struct {
	river.WorkerDefaults[RefreshStatusArgs]
	sender Sender
}

func (w *refreshWorker) Work(ctx context.Context, _ *river.Job[RefreshStatusArgs]) error {
	return w.sender.Refresh(ctx)
}

// NewWorkers registers the invoice workers.
func NewWorkers(sender Sender) (*river.Workers, error) {
	w := river.NewWorkers()
	if err := river.AddWorkerSafely(w, &sendWorker{sender: sender}); err != nil {
		return nil, fmt.Errorf("gönderim worker'ı eklenemedi: %w", err)
	}
	if err := river.AddWorkerSafely(w, &refreshWorker{sender: sender}); err != nil {
		return nil, fmt.Errorf("durum worker'ı eklenemedi: %w", err)
	}
	return w, nil
}

// PeriodicJobs polls non-final invoices every two minutes.
func PeriodicJobs() []*river.PeriodicJob {
	return []*river.PeriodicJob{
		river.NewPeriodicJob(
			river.PeriodicInterval(2*time.Minute),
			func() (river.JobArgs, *river.InsertOpts) { return RefreshStatusArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true},
		),
	}
}

// Enqueuer inserts send jobs inside the caller's transaction. It implements db.SendEnqueuer.
type Enqueuer struct{ Client *Client }

func (e Enqueuer) EnqueueSend(ctx context.Context, tx pgx.Tx, invoiceID uuid.UUID) error {
	_, err := e.Client.InsertTx(ctx, tx, SendInvoiceArgs{InvoiceID: invoiceID}, nil)
	return err
}
