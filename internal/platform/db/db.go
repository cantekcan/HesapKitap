// Package db owns the PostgreSQL connection pool and schema migrations.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// NewPool opens a pgx pool and verifies the connection.
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("veritabanı havuzu açılamadı: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("veritabanına bağlanılamadı: %w", err)
	}
	return pool, nil
}

// Migrate applies all pending goose migrations.
func Migrate(ctx context.Context, databaseURL string) error {
	sqlDB, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("migration bağlantısı açılamadı: %w", err)
	}
	defer func() { _ = sqlDB.Close() }()

	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("migration dosyaları okunamadı: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, sub)
	if err != nil {
		return fmt.Errorf("goose başlatılamadı: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("migration uygulanamadı: %w", err)
	}

	// River'ın kendi tabloları (kuyruk, işler) kendi migration sistemiyle gelir.
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("river migration bağlantısı açılamadı: %w", err)
	}
	defer pool.Close()
	rm, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return fmt.Errorf("river migrator oluşturulamadı: %w", err)
	}
	if _, err := rm.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("river migration uygulanamadı: %w", err)
	}
	return nil
}

// ensure the pgx stdlib driver is registered as "pgx".
var _ = stdlib.GetDefaultDriver
