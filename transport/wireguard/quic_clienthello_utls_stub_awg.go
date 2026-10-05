//go:build with_awg && !with_utls

package wireguard

import (
	"crypto/ecdh"
	"crypto/rand"
)

func buildBrowserClientHello(sni, browser string) ([]byte, error) {
	_ = browser
	var tlsRandom [32]byte
	if _, err := rand.Read(tlsRandom[:]); err != nil {
		return nil, err
	}
	ecKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return buildGenericClientHello(sni, tlsRandom, ecKey.PublicKey().Bytes())
}
