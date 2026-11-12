package trendyol

import (
	"context"
	"net"
	"net/http"
	"time"
)

const (
	dialTimeout           = 5 * time.Second
	tlsHandshakeTimeout   = 5 * time.Second
	responseHeaderTimeout = 20 * time.Second
	requestTimeout        = 30 * time.Second
	maxResponseBytes      = 8 << 20
)

// newTransport returns an http.Transport that only ever connects over IPv4 (ALTIN KURAL 3).
//
// Trendyol, IPv6'dan gelen istekleri Cloudflare'de 403 ile reddeder; portala yalnızca IPv4 tanımlanabilir.
// Proxy bilerek ayarlanmaz: ortamdaki HTTP(S)_PROXY, çıkış IP'sini sessizce değiştirebilir.
func newTransport() *http.Transport {
	dialer := &net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}
	return &http.Transport{
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp4", addr)
		},
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ResponseHeaderTimeout: responseHeaderTimeout,
		ExpectContinueTimeout: time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          10,
		ForceAttemptHTTP2:     true,
	}
}
