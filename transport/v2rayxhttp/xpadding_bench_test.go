//go:build with_xhttp

package v2rayxhttp

import (
	"net/http"
	"net/url"
	"testing"
)

func BenchmarkApplyXPaddingLegacy(b *testing.B) {
	c := &Client{
		scheme:       "https",
		host:         "example.com",
		paddingRange: intRange{100, 100},
		meta: metaConfig{
			xPaddingObfsMode: false,
		},
	}
	req, _ := http.NewRequest(http.MethodGet, "https://example.com/test", nil)
	req.URL = &url.URL{Path: "/test"}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		c.applyXPadding(req)
	}
}

func BenchmarkGenerateTokenishPadding(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = generateTokenishPaddingBase62(120)
	}
}

func BenchmarkApplyXPaddingHeader(b *testing.B) {
	c := &Client{
		scheme:       "https",
		host:         "example.com",
		paddingRange: intRange{100, 100},
		meta: metaConfig{
			xPaddingObfsMode:  true,
			xPaddingPlacement: placementHeader,
			xPaddingHeader:    "X-Padding",
			xPaddingMethod:    methodRepeatX,
		},
	}
	req, _ := http.NewRequest(http.MethodGet, "https://example.com/test", nil)
	req.URL = &url.URL{Path: "/test"}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		c.applyXPadding(req)
	}
}

func BenchmarkApplyXPaddingCookie(b *testing.B) {
	c := &Client{
		scheme:       "https",
		host:         "example.com",
		paddingRange: intRange{100, 100},
		meta: metaConfig{
			xPaddingObfsMode:  true,
			xPaddingPlacement: placementCookie,
			xPaddingKey:       "pad",
			xPaddingMethod:    methodRepeatX,
		},
	}
	req, _ := http.NewRequest(http.MethodGet, "https://example.com/test", nil)
	req.URL = &url.URL{Path: "/test"}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		c.applyXPadding(req)
	}
}

