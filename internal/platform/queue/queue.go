// Package queue wraps the River (Postgres-backed) job queue.
//
// Faz 1'de yalnızca iskelet vardır. İş türleri (fatura gönderme, durum sorgulama) Faz 5'te
// workers paketine eklenir. Gönderim işleri faturayla aynı transaction'da InsertTx ile kuyruğa yazılır.
package queue

import (
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

// Client is the River client bound to pgx transactions.
type Client = river.Client[pgx.Tx]

// New creates a River client.
//
// workers == nil ise istemci yalnızca iş ekleyebilir (insert-only) ve işleme başlatılamaz: River en az
// bir worker kayıtlı olmadan kuyruk işlemeyi reddeder. İlk iş türü eklenince çağıran workers verir ve Start eder.
func New(pool *pgxpool.Pool, workers *river.Workers, log *slog.Logger) (*Client, error) {
	cfg := &river.Config{Logger: log}
	if workers != nil {
		cfg.Workers = workers
		cfg.PeriodicJobs = PeriodicJobs()
		cfg.Queues = map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 5}}
	}
	c, err := river.NewClient(riverpgxv5.New(pool), cfg)
	if err != nil {
		return nil, fmt.Errorf("river istemcisi oluşturulamadı: %w", err)
	}
	return c, nil
}
