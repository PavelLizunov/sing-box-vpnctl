package v2rayxhttp

import (
	cryptorand "crypto/rand"
	"math/rand"
	"net/http"
	"strings"
	"unsafe"

	"golang.org/x/net/http2/hpack"
)

var (
	zeroPaddingBuf = strings.Repeat("0", 4096)
	xPaddingBuf    = strings.Repeat("X", 4096)
)

func getStaticZeroPadding(n int) string {
	if n <= len(zeroPaddingBuf) {
		return zeroPaddingBuf[:n]
	}
	return strings.Repeat("0", n)
}

func getStaticXPadding(n int) string {
	if n <= len(xPaddingBuf) {
		return xPaddingBuf[:n]
	}
	return strings.Repeat("X", n)
}

func randIntn(n int) int {
	if n <= 0 {
		return 0
	}
	return rand.Intn(n)
}

func (c *Client) applyXPadding(request *http.Request) {
	m := &c.meta
	if !m.xPaddingObfsMode {
		n := c.paddingRange.rand()
		if n <= 0 {
			return
		}
		pad := getStaticZeroPadding(n)
		request.Header.Set("Referer", c.scheme+"://"+c.host+request.URL.Path+"?x_padding="+pad)
		return
	}
	pad := c.generatePadding()
	if pad == "" {
		return
	}
	switch m.xPaddingPlacement {
	case placementCookie:
		request.AddCookie(&http.Cookie{Name: m.xPaddingKey, Value: pad, Path: "/"})
	case placementHeader:
		request.Header.Set(m.xPaddingHeader, pad)
	case placementQuery:
		setQuery(request.URL, m.xPaddingKey, pad)
	case placementQueryInHeader:
		request.Header.Set(m.xPaddingHeader, c.scheme+"://"+c.host+request.URL.Path+"?"+m.xPaddingKey+"="+pad)
	}
}

func (c *Client) generatePadding() string {
	n := c.paddingRange.rand()
	if n <= 0 {
		return ""
	}
	if c.meta.xPaddingMethod == methodTokenish {
		return generateTokenishPaddingBase62(n)
	}
	return getStaticXPadding(n)
}

const base62Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

func generateTokenishPaddingBase62(target int) string {
	if target <= 0 {
		return ""
	}
	initialLen := (target * 10) / 8
	if initialLen < 1 {
		initialLen = 1
	}
	buf := make([]byte, initialLen, initialLen+8)
	if _, err := cryptorand.Read(buf); err != nil {
		for i := range buf {
			buf[i] = byte(randIntn(256))
		}
	}
	for i := range buf {
		buf[i] = base62Alphabet[int(buf[i])%len(base62Alphabet)]
	}

	const maxIter = 150
	for iter := 0; iter < maxIter; iter++ {
		encoded := int(hpack.HuffmanEncodeLength(unsafe.String(unsafe.SliceData(buf), len(buf))))
		switch {
		case encoded < target-2:
			if len(buf)%2 == 0 {
				buf = append(buf, 'X')
			} else {
				buf = append(buf, 'Z')
			}
		case encoded > target+2:
			if len(buf) > 1 {
				buf = buf[:len(buf)-1]
			} else {
				return string(buf)
			}
		default:
			return string(buf)
		}
	}
	return string(buf)
}
