package trendyol

import (
	"context"
	"errors"
	"fmt"
	"net"
)

var (
	// ErrIPNotWhitelisted is returned on HTTP 403: Cloudflare blocks IPs that are not registered in the portal.
	ErrIPNotWhitelisted = errors.New("IP adresi Trendyol'da tanımlı değil")
	// ErrInvalidCredentials is returned when signin itself answers 401.
	ErrInvalidCredentials = errors.New("Trendyol e-posta veya şifreyi kabul etmedi") //nolint:staticcheck // kullanıcıya gösterilen Türkçe metin
	// ErrUnauthorized is returned when a request is still 401 after one re-login.
	ErrUnauthorized = errors.New("Trendyol oturumu yenilenemedi") //nolint:staticcheck // kullanıcıya gösterilen Türkçe metin
)

// APIError is a non-2xx answer. Trendyol sends application/problem+json: { type, title, status, detail, instance }.
type APIError struct {
	Status   int
	Type     string
	Title    string
	Detail   string
	Instance string
}

func (e *APIError) Error() string {
	switch {
	case e.Detail != "":
		return fmt.Sprintf("trendyol: HTTP %d: %s: %s", e.Status, e.Title, e.Detail)
	case e.Title != "":
		return fmt.Sprintf("trendyol: HTTP %d: %s", e.Status, e.Title)
	default:
		return fmt.Sprintf("trendyol: HTTP %d", e.Status)
	}
}

// IsNotFound reports whether err is an HTTP 404 answer from Trendyol.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status == 404
}

// UserMessage turns an error from this package into a Turkish message for the screen.
// For API errors the provider's own `detail` is appended after a Turkish explanation (ALTIN KURAL 8).
func UserMessage(err error) string {
	var apiErr *APIError
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrIPNotWhitelisted):
		return "Trendyol bu IP adresinden gelen isteği reddetti (403). Bilgisayarın/sunucunun dış IPv4 adresi " +
			"E-Faturam portalındaki IP listesinde yok ya da değişmiş olabilir."
	case errors.Is(err, ErrInvalidCredentials):
		return "Trendyol E-Faturam e-posta veya şifreyi kabul etmedi. Ayarlardaki giriş bilgilerini kontrol edin."
	case errors.Is(err, ErrUnauthorized):
		return "Trendyol E-Faturam oturumu yenilenemedi. Biraz sonra tekrar deneyin; sürerse giriş bilgilerini kontrol edin."
	case errors.As(err, &apiErr):
		if apiErr.Detail != "" {
			return fmt.Sprintf("Trendyol E-Faturam isteği kabul etmedi (HTTP %d): %s", apiErr.Status, apiErr.Detail)
		}
		return fmt.Sprintf("Trendyol E-Faturam isteği kabul etmedi (HTTP %d).", apiErr.Status)
	case errors.Is(err, context.DeadlineExceeded):
		return "Trendyol E-Faturam zamanında yanıt vermedi. İşlemin sonucu belirsiz olabilir; tekrar göndermeden önce durumu sorgulayın."
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return "Trendyol E-Faturam'a bağlanılamadı. İnternet bağlantısını kontrol edin."
	}
	return "Trendyol E-Faturam ile iletişimde beklenmeyen bir hata oluştu."
}
