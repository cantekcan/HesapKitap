package attachment

import (
	"bytes"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectLooksAtContentNotName(t *testing.T) {
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGNgYGD4DwABBAEAwS2OUAAAAABJRU5ErkJggg==")
	require.NoError(t, err)
	ct, err := Detect(png)
	require.NoError(t, err)
	assert.Equal(t, "image/png", ct)

	ct, err = Detect([]byte("%PDF-1.4\n%âãÏÓ\n1 0 obj\n"))
	require.NoError(t, err)
	assert.Equal(t, "application/pdf", ct)

	_, err = Detect([]byte("merhaba dünya"))
	assert.ErrorIs(t, err, ErrUnsupported)
	_, err = Detect([]byte("<html><script>alert(1)</script></html>"))
	assert.ErrorIs(t, err, ErrUnsupported)
	_, err = Detect(nil)
	assert.ErrorIs(t, err, ErrEmpty)
	_, err = Detect(append(png[:8:8], bytes.Repeat([]byte{0}, MaxSize+1)...))
	assert.ErrorIs(t, err, ErrTooLarge)
}

func TestCleanName(t *testing.T) {
	assert.Equal(t, "cek foto.png", CleanName("../../cek foto.png"))
	assert.Equal(t, "dekont.pdf", CleanName(`C:\Users\x\dekont.pdf`))
	assert.NotEmpty(t, CleanName(""))
	assert.NotContains(t, CleanName("a\x00b\nc.png"), "\n")
}
