// Package config reads and validates the process configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
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

	// EInvoiceEnabled: firmanın kendisi e-Fatura mükellefi (e-Fatura kesilebilir). Değilse hep e-Arşiv kesilir.
	EInvoiceEnabled bool
	// EInvoiceScenario: TEMELFATURA veya TICARIFATURA.
	EInvoiceScenario string
	// InvoicePrefix: portalda kullanılacak 3 karakterli fatura serisi (örn. ABC).
	InvoicePrefix string
	// Company: satıcı (firma) bilgileri; yalnızca taslak fatura PDF önizlemesinde görünür. Hepsi isteğe bağlı.
	Company Company
	// SMTP: müşteriye fatura bağlantısı e-postası için; Host ve From doluysa özellik açılır.
	SMTP SMTP
}

// SMTP is the outgoing mail account.
type SMTP struct {
	Host, Port, User, Password, From string
}

// Company is the seller block printed on the draft invoice preview.
type Company struct {
	Name, TaxID, TaxOffice, Address, City, Phone, Email string
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
		EInvoiceEnabled:  get("EINVOICE_ENABLED", "false") == "true",
		EInvoiceScenario: get("EINVOICE_SCENARIO", "TEMELFATURA"),
		InvoicePrefix:    get("INVOICE_PREFIX", ""),
		SMTP: SMTP{
			Host: get("SMTP_HOST", ""), Port: get("SMTP_PORT", "587"), User: get("SMTP_USER", ""),
			Password: get("SMTP_PASSWORD", ""), From: get("SMTP_FROM", ""),
		},
		Company: Company{
			Name: get("COMPANY_NAME", ""), TaxID: get("COMPANY_TAX_ID", ""), TaxOffice: get("COMPANY_TAX_OFFICE", ""),
			Address: get("COMPANY_ADDRESS", ""), City: get("COMPANY_CITY", ""), Phone: get("COMPANY_PHONE", ""), Email: get("COMPANY_EMAIL", ""),
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
	if p := cfg.InvoicePrefix; p != "" && !regexp.MustCompile(`^[A-Z0-9]{3}$`).MatchString(p) {
		errs = append(errs, fmt.Errorf("INVOICE_PREFIX %q geçersiz (3 büyük harf veya rakam olmalı)", p))
	}
	if cfg.EInvoiceScenario != "TEMELFATURA" && cfg.EInvoiceScenario != "TICARIFATURA" {
		errs = append(errs, fmt.Errorf("EINVOICE_SCENARIO %q geçersiz (TEMELFATURA veya TICARIFATURA olmalı)", cfg.EInvoiceScenario))
	}

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
