//go:build with_awg

package wireguard

import (
	"testing"

	"github.com/sagernet/sing-box/option"
)

func BenchmarkAwgIpcLinesAWG2(b *testing.B) {
	opts := option.AmneziaWGOptions{
		Jc:   4,
		Jmin: 40,
		Jmax: 70,
		S1:   20,
		S2:   30,
		S3:   20,
		S4:   30,
		H1:   "12345678",
		H2:   "23456789",
		H3:   "34567890",
		H4:   "45678901",
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = awgIpcLines(opts)
	}
}

func BenchmarkAwgIpcLinesAWG31(b *testing.B) {
	bTrue := true
	opts := option.AmneziaWGOptions{
		Jc:                     4,
		Jmin:                   40,
		Jmax:                   70,
		S1:                     20,
		S2:                     30,
		S3:                     20,
		S4:                     30,
		H1:                     "12345678",
		H2:                     "23456789",
		H3:                     "34567890",
		H4:                     "45678901",
		RandomTrailers:         &bTrue,
		DisableCookie:          &bTrue,
		HeaderProtectionKey:    "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ContentPaddingAddition: "10-50",
		RekeyAfterTime:         "60-120",
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = awgIpcLines(opts)
	}
}

func BenchmarkMasqueCPSQUIC(b *testing.B) {
	opts := option.AmneziaWGOptions{
		Id: "example.com",
		Ip: "quic",
		Ib: "chrome",
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = masqueI1(opts)
	}
}
