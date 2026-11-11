// Command hesapkitap is the Hesap Kitap server and its admin commands.
//
//	hesapkitap [serve]            HTTP sunucusunu başlatır (varsayılan)
//	hesapkitap migrate            bekleyen veritabanı migration'larını uygular
//	hesapkitap create-user <e-posta>   kullanıcı ekler (şifre terminalden sorulur)
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"hesapkitap/internal/platform/config"
	"hesapkitap/internal/platform/logging"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	// .env yalnızca geliştirme kolaylığı; yoksa sorun değil. Ortamda zaten tanımlı değişkenler ezilmez.
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(os.Stderr, "hata: .env okunamadı:", err)
		return 1
	}

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hata:", err)
		return 1
	}

	level := slog.LevelInfo
	if !cfg.IsProduction() {
		level = slog.LevelDebug
	}
	log := logging.New(os.Stderr, level)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
	}

	switch cmd {
	case "serve":
		err = serve(ctx, cfg, log)
	case "migrate":
		err = migrate(ctx, cfg, log)
	case "create-user":
		err = createUser(ctx, cfg, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "bilinmeyen komut %q (serve, migrate, create-user)\n", cmd)
		return 2
	}
	if err != nil {
		log.Error("komut başarısız", "command", cmd, "err", err)
		return 1
	}
	return 0
}
