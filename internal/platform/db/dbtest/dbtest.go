// Package dbtest starts a throwaway PostgreSQL container for integration tests.
package dbtest

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"hesapkitap/internal/platform/db"
)

// NewPool starts Postgres in Docker, applies migrations and returns a pool.
// The test is skipped when Docker is not available, so plain `go test` keeps working everywhere.
func NewPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	ctr, err := postgres.Run(ctx, "postgres:17",
		postgres.WithDatabase("hesapkitap_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		postgres.BasicWaitStrategies(),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(time.Minute)),
	)
	if err != nil {
		t.Fatalf("postgres konteyneri başlatılamadı: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		_ = ctr.Terminate(stopCtx)
	})

	url, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("bağlantı adresi alınamadı: %v", err)
	}
	if err := db.Migrate(ctx, url); err != nil {
		t.Fatalf("migration uygulanamadı: %v", err)
	}
	pool, err := db.NewPool(ctx, url)
	if err != nil {
		t.Fatalf("havuz açılamadı: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
