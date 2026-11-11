package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"hesapkitap/internal/platform/config"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoad(t *testing.T) {
	const stage = "https://stage-apigateway.trendyolefaturam.com"
	const prod = "https://apigateway.trendyolecozum.com"

	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{
			name: "geliştirme: minimum ayarlar",
			env:  map[string]string{"DATABASE_URL": "postgres://x", "TRENDYOL_BASE_URL": stage},
		},
		{
			name:    "DATABASE_URL eksik",
			env:     map[string]string{"TRENDYOL_BASE_URL": stage},
			wantErr: "DATABASE_URL zorunlu",
		},
		{
			name:    "TRENDYOL_BASE_URL eksik",
			env:     map[string]string{"DATABASE_URL": "postgres://x"},
			wantErr: "TRENDYOL_BASE_URL zorunlu",
		},
		{
			name:    "geçersiz APP_ENV",
			env:     map[string]string{"APP_ENV": "staging", "DATABASE_URL": "postgres://x", "TRENDYOL_BASE_URL": stage},
			wantErr: "APP_ENV",
		},
		{
			name: "production: stage URL reddedilir",
			env: map[string]string{
				"APP_ENV": "production", "DATABASE_URL": "postgres://x", "TRENDYOL_BASE_URL": stage,
				"TRENDYOL_EMAIL": "a@b.c", "TRENDYOL_PASSWORD": "p",
			},
			wantErr: "stage",
		},
		{
			name:    "production: e-posta ve şifre zorunlu",
			env:     map[string]string{"APP_ENV": "production", "DATABASE_URL": "postgres://x", "TRENDYOL_BASE_URL": prod},
			wantErr: "TRENDYOL_EMAIL zorunlu",
		},
		{
			name: "production: geçerli",
			env: map[string]string{
				"APP_ENV": "production", "DATABASE_URL": "postgres://x", "TRENDYOL_BASE_URL": prod,
				"TRENDYOL_EMAIL": "a@b.c", "TRENDYOL_PASSWORD": "p",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := config.Load(env(tt.env))
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, ":8080", cfg.HTTPAddr)
		})
	}
}

func TestLoadReportsAllProblemsTogether(t *testing.T) {
	_, err := config.Load(env(map[string]string{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DATABASE_URL")
	assert.Contains(t, err.Error(), "TRENDYOL_BASE_URL")
}
