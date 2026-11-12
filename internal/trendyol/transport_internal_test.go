package trendyol

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ALTIN KURAL 3: istemci yalnızca IPv4 ile bağlanır.
func TestTransportConnectsOverIPv4Only(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	// IPv4'te dinleyen sunucuya erişilir.
	v4 := httptest.NewServer(handler)
	defer v4.Close()
	client := &http.Client{Transport: newTransport()}
	resp, err := client.Get(v4.URL) //nolint:noctx // test
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	// Yalnızca IPv6'da dinleyen sunucuya erişilemez (tcp4 dial).
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp6", "[::1]:0")
	if err != nil {
		t.Skip("bu makinede IPv6 loopback yok:", err)
	}
	v6 := httptest.NewUnstartedServer(handler)
	v6.Listener = ln
	v6.Start()
	defer v6.Close()

	resp6, err := client.Get(v6.URL) //nolint:noctx // test
	if resp6 != nil {
		_ = resp6.Body.Close()
	}
	require.Error(t, err, "IPv6 adresine bağlanılmamalı")
}

func TestTransportHasExplicitTimeouts(t *testing.T) {
	tr := newTransport()
	assert.NotZero(t, tr.TLSHandshakeTimeout)
	assert.NotZero(t, tr.ResponseHeaderTimeout)
	assert.Nil(t, tr.Proxy, "ortam proxy'si çıkış IP'sini değiştirebilir; kullanılmaz")
}
