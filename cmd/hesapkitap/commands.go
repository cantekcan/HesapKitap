package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"hesapkitap/internal/platform/auth"
	"hesapkitap/internal/platform/config"
	"hesapkitap/internal/platform/db"
	"hesapkitap/internal/platform/db/dbgen"
	httpserver "hesapkitap/internal/platform/http"
	"hesapkitap/internal/platform/queue"
)

const shutdownTimeout = 30 * time.Second

func serve(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	authSvc, err := auth.NewService(dbgen.New(pool))
	if err != nil {
		return err
	}

	// Arka plan işleri (River). Faz 5'te fatura gönderme ve durum sorgulama worker'ları eklenince
	// queue.New'e workers verilir ve jobs.Start/Stop burada çağrılır. Şimdilik insert-only istemci.
	if _, err := queue.New(pool, nil, log); err != nil {
		return err
	}

	handler, err := httpserver.New(httpserver.Deps{
		Logger:   log,
		Pool:     pool,
		Sessions: auth.NewSessionManager(pool, cfg.IsProduction()),
		Auth:     authSvc,
	})
	if err != nil {
		return fmt.Errorf("http sunucusu kurulamadı: %w", err)
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelError),
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("sunucu başlıyor", "addr", cfg.HTTPAddr, "env", string(cfg.Env))
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("sunucu durdu: %w", err)
	case <-ctx.Done():
		log.Info("kapanma sinyali alındı, bağlantılar boşaltılıyor")
	}

	// ctx iptal edildi; kapanış için iptalden bağımsız ama değerleri koruyan yeni bir süre sınırı.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("sunucu düzgün kapatılamadı: %w", err)
	}
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	log.Info("sunucu kapandı")
	return nil
}

func migrate(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	if err := db.Migrate(ctx, cfg.DatabaseURL); err != nil {
		return err
	}
	log.Info("migration'lar uygulandı")
	return nil
}

func createUser(ctx context.Context, cfg config.Config, args []string) error {
	if len(args) != 1 {
		return errors.New("kullanım: hesapkitap create-user <e-posta>")
	}
	password, err := readPassword()
	if err != nil {
		return err
	}

	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	svc, err := auth.NewService(dbgen.New(pool))
	if err != nil {
		return err
	}
	u, err := svc.CreateUser(ctx, args[0], password)
	if err != nil {
		return err
	}
	fmt.Printf("Kullanıcı oluşturuldu: %s\n", u.Email)
	return nil
}

// readPassword asks for the password twice on a terminal (no echo); when stdin is piped it reads one line.
func readPassword() (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("şifre okunamadı: %w", err)
		}
		// Windows PowerShell 5.1 pipe'ı metnin başına UTF-8 BOM ekleyebilir; şifrenin parçası olmamalı.
		return strings.TrimRight(strings.TrimPrefix(line, string(rune(0xFEFF))), "\r\n"), nil
	}

	fmt.Fprint(os.Stderr, "Şifre: ")
	first, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("şifre okunamadı: %w", err)
	}
	fmt.Fprint(os.Stderr, "Şifre (tekrar): ")
	second, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("şifre okunamadı: %w", err)
	}
	if string(first) != string(second) {
		return "", errors.New("şifreler eşleşmiyor")
	}
	return string(first), nil
}
