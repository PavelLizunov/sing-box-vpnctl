//go:build with_awg && with_utls

package wireguard

import (
	"context"

	utls "github.com/metacubex/utls"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
)

func browserHelloID(browser string) (utls.ClientHelloID, bool) {
	switch browser {
	case masqueBrowserChrome:
		return utls.HelloChrome_120, true
	case masqueBrowserFirefox:
		return utls.HelloFirefox_120, true
	default:
		return utls.ClientHelloID{}, false
	}
}

func buildBrowserClientHello(sni, browser string) ([]byte, error) {
	id, ok := browserHelloID(browser)
	if !ok {
		return nil, E.New("amneziawg: no uTLS fingerprint for browser ", browser)
	}

	spec, err := utls.UTLSIdToSpec(id)
	if err != nil {
		return nil, E.Cause(err, "amneziawg: uTLS spec for ", browser)
	}

	// Quirk: QUIC mode rejects browser presets that advertise TLS versions below 1.3.
	spec.TLSVersMin = utls.VersionTLS13
	spec.TLSVersMax = utls.VersionTLS13
	for _, ext := range spec.Extensions {
		switch e := ext.(type) {
		case *utls.ALPNExtension:
			e.AlpnProtocols = []string{"h3"}
		case *utls.SupportedVersionsExtension:
			e.Versions = []uint16{utls.VersionTLS13}
		case *utls.SupportedCurvesExtension:
			e.Curves = common.Filter(e.Curves, notPQCurve)
		case *utls.KeyShareExtension:
			e.KeyShares = common.Filter(e.KeyShares, func(k utls.KeyShare) bool { return notPQCurve(k.Group) })
		}
	}

	cfg := &utls.Config{ServerName: sni, MinVersion: utls.VersionTLS13}
	q := utls.UQUICClient(&utls.QUICConfig{TLSConfig: cfg}, utls.HelloCustom)
	defer q.Close()
	if err := q.ApplyPreset(&spec); err != nil {
		return nil, E.Cause(err, "amneziawg: uTLS ApplyPreset")
	}
	// Protocol: QUIC requires transport parameters in the ClientHello even when no handshake completes.
	q.SetTransportParameters(quicDecoyTransportParams())
	if err := q.Start(context.Background()); err != nil {
		return nil, E.Cause(err, "amneziawg: uTLS QUIC start")
	}

	var hello []byte
	for {
		ev := q.NextEvent()
		if ev.Kind == utls.QUICNoEvent {
			break
		}
		if ev.Kind == utls.QUICWriteData && ev.Level == utls.QUICEncryptionLevelInitial {
			hello = append(hello, ev.Data...)
		}
	}
	if len(hello) == 0 || hello[0] != 0x01 {
		return nil, E.New("amneziawg: uTLS produced no ClientHello")
	}
	return hello, nil
}

// Invariant: Post-quantum hybrid key shares must be excluded to keep the ClientHello within one QUIC Initial.
func notPQCurve(c utls.CurveID) bool {
	return c != utls.X25519MLKEM768 && c != utls.X25519Kyber768Draft00
}

func quicDecoyTransportParams() []byte {
	return buildQUICTransportParams()
}
