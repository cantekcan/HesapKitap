// Package queue wraps the River (Postgres-backed) job queue.
//
// Faz 1'de sadece iskelet vardır: iş türleri (fatura gönderme, durum sorgulama) Faz 5'te
// River.AddWorker ile eklenir. Gönderim işleri faturayla aynı transaction'da InsertTx ile kuyruğa yazılır.
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

// New creates a River client. Call Start to begin processing and Stop on shutdown.
func New(pool *pgxpool.Pool, workers *river.Workers, log *slog.Logger) (*Client, error) {
	c, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Logger:  log,
		Workers: workers,
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 5},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("river istemcisi oluşturulamadı: %w", err)
	}
	return c, nil
}
