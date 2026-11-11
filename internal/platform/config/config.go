// Package config reads and validates the process configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Env is the deployment mode of the process.
type Env string

const (
	EnvDevelopment Env = "development"
	EnvProduction  Env = "production"
)

// Trendyol holds the Trendyol E-Faturam connection settings.
type Trendyol struct {
	BaseURL  string
	Email    string
	Password string
}

// Config is the validated application configuration.
type Config struct {
	Env         Env
	HTTPAddr    string
	DatabaseURL string
	Trendyol    Trendyol
}

// IsProduction reports whether the process runs in production mode.
func (c Config) IsProduction() bool { return c.Env == EnvProduction }

// Load reads the configuration using getenv (usually os.Getenv) and validates it.
// All problems are reported together so a misconfigured deploy is fixed in one round.
func Load(getenv func(string) string) (Config, error) {
	get := func(key, def string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return def
	}

	cfg := Config{
		Env:         Env(get("APP_ENV", string(EnvDevelopment))),
		HTTPAddr:    get("HTTP_ADDR", ":8080"),
		DatabaseURL: get("DATABASE_URL", ""),
		Trendyol: Trendyol{
			BaseURL:  strings.TrimRight(get("TRENDYOL_BASE_URL", ""), "/"),
			Email:    get("TRENDYOL_EMAIL", ""),
			Password: get("TRENDYOL_PASSWORD", ""),
		},
	}

	var errs []error
	if cfg.Env != EnvDevelopment && cfg.Env != EnvProduction {
		errs = append(errs, fmt.Errorf("APP_ENV %q geçersiz (development veya production olmalı)", cfg.Env))
	}
	if cfg.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL zorunlu"))
	}
	errs = append(errs, validateTrendyol(cfg)...)

	if err := errors.Join(errs...); err != nil {
		return Config{}, fmt.Errorf("config doğrulanamadı: %w", err)
	}
	return cfg, nil
}

func validateTrendyol(cfg Config) []error {
	var errs []error
	t := cfg.Trendyol

	if t.BaseURL == "" {
		return append(errs, errors.New("TRENDYOL_BASE_URL zorunlu"))
	}
	u, err := url.Parse(t.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return append(errs, fmt.Errorf("TRENDYOL_BASE_URL geçerli bir URL değil: %q", t.BaseURL))
	}

	if cfg.IsProduction() {
		// Prod'da stage ortamına bağlanmak (veya düz http kullanmak) kabul edilmez.
		if strings.Contains(strings.ToLower(u.Hostname()), "stage") {
			errs = append(errs, errors.New("production modunda stage Trendyol adresi kullanılamaz"))
		}
		if u.Scheme != "https" {
			errs = append(errs, errors.New("production modunda TRENDYOL_BASE_URL https olmalı"))
		}
		if t.Email == "" {
			errs = append(errs, errors.New("production modunda TRENDYOL_EMAIL zorunlu"))
		}
		if t.Password == "" {
			errs = append(errs, errors.New("production modunda TRENDYOL_PASSWORD zorunlu"))
		}
	}
	return errs
}
