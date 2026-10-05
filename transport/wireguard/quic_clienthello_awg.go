//go:build with_awg

package wireguard

import (
	"encoding/binary"

	E "github.com/sagernet/sing/common/exceptions"
)

// Invariant: ClientHello must exceed 290 bytes so the final fragment at the fixed cutpoints is non-empty.
const quicCHTargetLen = 294

const quicCHMinLen = 291

func appendVec16(dst, body []byte) []byte {
	dst = binary.BigEndian.AppendUint16(dst, uint16(len(body)))
	return append(dst, body...)
}

func appendExtension(dst []byte, extType uint16, data []byte) []byte {
	dst = binary.BigEndian.AppendUint16(dst, extType)
	dst = binary.BigEndian.AppendUint16(dst, uint16(len(data)))
	return append(dst, data...)
}

// Quirk: uTLS has no curl QUIC fingerprint, so curl uses the generic ClientHello.
func buildClientHello(sni string, tlsRandom [32]byte, x25519Pub []byte, browser string) ([]byte, error) {
	switch browser {
	case masqueBrowserChrome, masqueBrowserFirefox:
		return buildBrowserClientHello(sni, browser)
	default:
		return buildGenericClientHello(sni, tlsRandom, x25519Pub)
	}
}

func buildGenericClientHello(sni string, tlsRandom [32]byte, x25519Pub []byte) ([]byte, error) {
	if sni == "" {
		return nil, E.New("amneziawg: ip=quic requires a non-empty id (SNI) for the ClientHello")
	}
	if len(x25519Pub) != 32 {
		return nil, E.New("amneziawg: x25519 public key must be 32 bytes")
	}

	var exts []byte

	exts = appendExtension(exts, 0x000a, appendVec16(nil, []byte{0x00, 0x1d}))

	sigAlgs := []byte{
		0x04, 0x03,
		0x08, 0x04,
		0x04, 0x01,
		0x08, 0x05,
		0x05, 0x01,
		0x08, 0x06,
		0x06, 0x01,
		0x02, 0x01,
	}
	exts = appendExtension(exts, 0x000d, appendVec16(nil, sigAlgs))

	var sniEntry []byte
	sniEntry = append(sniEntry, 0x00)
	sniEntry = appendVec16(sniEntry, []byte(sni))
	exts = appendExtension(exts, 0x0000, appendVec16(nil, sniEntry))

	var ks []byte
	ks = binary.BigEndian.AppendUint16(ks, 0x001d)
	ks = appendVec16(ks, x25519Pub)
	exts = appendExtension(exts, 0x0033, appendVec16(nil, ks))

	qtp := buildQUICTransportParams()
	exts = appendExtension(exts, 0x0039, qtp)

	exts = appendExtension(exts, 0x0a0a, []byte{0x00, 0x00, 0x00})

	exts = appendExtension(exts, 0x002d, []byte{0x01, 0x01})

	var alpn []byte
	alpn = append(alpn, 0x02, 'h', '3')
	exts = appendExtension(exts, 0x0010, appendVec16(nil, alpn))

	exts = appendExtension(exts, 0x001b, []byte{0x02, 0x00, 0x02})

	exts = appendExtension(exts, 0x002b, []byte{0x02, 0x03, 0x04})

	var body []byte
	body = append(body, 0x03, 0x03)
	body = append(body, tlsRandom[:]...)
	body = append(body, 0x00)
	body = appendVec16(body, []byte{0x13, 0x01})
	body = append(body, 0x01, 0x00)

	const handshakeHdr = 4
	const extsLenPrefix = 2
	const padExtHdr = 4
	current := handshakeHdr + len(body) + extsLenPrefix + len(exts)
	pad := quicCHTargetLen - current - padExtHdr
	if pad < 0 {
		pad = 0
	}
	exts = appendExtension(exts, 0x0015, make([]byte, pad))

	body = appendVec16(body, exts)

	out := make([]byte, 0, handshakeHdr+len(body))
	out = append(out, 0x01)
	out = append(out, byte(len(body)>>16), byte(len(body)>>8), byte(len(body)))
	out = append(out, body...)

	if len(out) < quicCHMinLen {
		return nil, E.New("amneziawg: ClientHello assembled shorter than the minimum fragmentable length")
	}
	return out, nil
}

func buildQUICTransportParams() []byte {
	var p []byte
	appendParam := func(id uint64, value []byte) {
		p = appendQUICVarint(p, id)
		p = appendQUICVarint(p, uint64(len(value)))
		p = append(p, value...)
	}
	appendParam(0x01, appendQUICVarint(nil, 30000))
	appendParam(0x04, appendQUICVarint(nil, 0x00c00000))
	appendParam(0x05, appendQUICVarint(nil, 0x00100000))
	appendParam(0x06, appendQUICVarint(nil, 0x00100000))
	appendParam(0x07, appendQUICVarint(nil, 0x00100000))
	appendParam(0x08, appendQUICVarint(nil, 100))
	appendParam(0x09, appendQUICVarint(nil, 100))
	appendParam(0x0e, appendQUICVarint(nil, 8))
	appendParam(0x03, appendQUICVarint(nil, 1472))
	return p
}
