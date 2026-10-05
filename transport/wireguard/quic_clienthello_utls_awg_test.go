//go:build with_awg && with_utls

package wireguard

import (
	"bytes"
	"testing"

	"github.com/sagernet/sing-box/option"

	"github.com/stretchr/testify/require"
)

func TestQUICInitialBrowserFingerprint(t *testing.T) {
	t.Parallel()
	const sni = "www.google.com"

	gen := func(ib string) decodedInitial {
		spec, err := masqueI1(option.AmneziaWGOptions{Ip: "quic", Id: sni, Ib: ib})
		require.NoError(t, err, "ib=%q", ib)
		pkt := obfuscateCPS(t, spec)
		require.Equal(t, quicInitialTotalLen, len(pkt), "ib=%q packet size", ib)
		d := decryptInitial(t, pkt)
		require.Equal(t, sni, extractSNI(t, d.clientHello), "ib=%q SNI (I4)", ib)
		require.NotEqual(t, uint64(0), d.cryptoFrames[0].offset, "ib=%q first CRYPTO offset≠0 (I1)", ib)
		return d
	}

	generic := gen("")
	chrome := gen("chrome")
	firefox := gen("firefox")
	curl := gen("curl")

	require.Equal(t, len(generic.clientHello), len(curl.clientHello), "curl uses the generic CH")

	require.Greater(t, len(chrome.clientHello), len(generic.clientHello), "chrome CH is the uTLS one")
	require.Greater(t, len(firefox.clientHello), len(generic.clientHello), "firefox CH is the uTLS one")
	require.NotEqual(t, len(chrome.clientHello), len(firefox.clientHello), "chrome and firefox JA3 differ")

	require.True(t, hasGREASECipher(chrome.clientHello), "chrome ClientHello carries GREASE")
	require.False(t, hasGREASECipher(firefox.clientHello), "firefox ClientHello has no GREASE")
}

func hasGREASECipher(ch []byte) bool {
	r := bytes.NewReader(ch[4:])
	skipN(r, 2+32)
	sid, _ := r.ReadByte()
	skipN(r, int(sid))
	hi, _ := r.ReadByte()
	lo, _ := r.ReadByte()
	csLen := int(hi)<<8 | int(lo)
	cs := make([]byte, csLen)
	if _, err := r.Read(cs); err != nil {
		return false
	}
	for i := 0; i+1 < len(cs); i += 2 {
		if cs[i] == cs[i+1] && cs[i]&0x0f == 0x0a {
			return true
		}
	}
	return false
}

func skipN(r *bytes.Reader, n int) {
	for i := 0; i < n; i++ {
		_, _ = r.ReadByte()
	}
}
