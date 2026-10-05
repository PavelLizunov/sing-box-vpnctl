//go:build with_awg

package wireguard

import (
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"

	"golang.org/x/crypto/hkdf"
)

const quicVersion1 uint32 = 0x1

var quicSaltV1 = []byte{
	0x38, 0x76, 0x2c, 0xf7, 0xf5, 0x59, 0x34, 0xb3, 0x4d, 0x17,
	0x9a, 0xe6, 0xa4, 0xc8, 0x0c, 0xad, 0xcc, 0xbb, 0x7f, 0x0a,
}

var (
	hkdfLabelClientIn = []byte{0x00, 0x20, 0x0f, 't', 'l', 's', '1', '3', ' ', 'c', 'l', 'i', 'e', 'n', 't', ' ', 'i', 'n', 0x00}
	hkdfLabelQuicKey  = []byte{0x00, 0x10, 0x0e, 't', 'l', 's', '1', '3', ' ', 'q', 'u', 'i', 'c', ' ', 'k', 'e', 'y', 0x00}
	hkdfLabelQuicIV   = []byte{0x00, 0x0c, 0x0d, 't', 'l', 's', '1', '3', ' ', 'q', 'u', 'i', 'c', ' ', 'i', 'v', 0x00}
	hkdfLabelQuicHP   = []byte{0x00, 0x10, 0x0d, 't', 'l', 's', '1', '3', ' ', 'q', 'u', 'i', 'c', ' ', 'h', 'p', 0x00}
)

func quicHKDFExpandLabel(hash crypto.Hash, secret, context []byte, label string, length int) []byte {
	var b []byte
	if len(context) == 0 {
		switch label {
		case "client in":
			if length == 32 {
				b = hkdfLabelClientIn
			}
		case "quic key":
			if length == 16 {
				b = hkdfLabelQuicKey
			}
		case "quic iv":
			if length == 12 {
				b = hkdfLabelQuicIV
			}
		case "quic hp":
			if length == 16 {
				b = hkdfLabelQuicHP
			}
		}
	}
	if b == nil {
		b = make([]byte, 3, 3+6+len(label)+1+len(context))
		binary.BigEndian.PutUint16(b, uint16(length))
		b[2] = uint8(6 + len(label))
		b = append(b, []byte("tls13 ")...)
		b = append(b, []byte(label)...)
		b = b[:3+6+len(label)+1]
		b[3+6+len(label)] = uint8(len(context))
		b = append(b, context...)
	}
	out := make([]byte, length)
	n, err := hkdf.Expand(hash.New, secret, b).Read(out)
	if err != nil || n != length {
		panic("quic: HKDF-Expand-Label invocation failed unexpectedly")
	}
	return out
}

func quicAEADAESGCMTLS13(key, nonceMask []byte) cipher.AEAD {
	if len(nonceMask) != 12 {
		panic("tls: internal error: wrong nonce length")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	ret := &quicXorNonceAEAD{aead: aead}
	copy(ret.nonceMask[:], nonceMask)
	return ret
}

type quicXorNonceAEAD struct {
	nonceMask [12]byte
	aead      cipher.AEAD
}

func (f *quicXorNonceAEAD) NonceSize() int { return 8 }
func (f *quicXorNonceAEAD) Overhead() int  { return f.aead.Overhead() }

func (f *quicXorNonceAEAD) Seal(out, nonce, plaintext, additionalData []byte) []byte {
	for i, b := range nonce {
		f.nonceMask[4+i] ^= b
	}
	result := f.aead.Seal(out, f.nonceMask[:], plaintext, additionalData)
	for i, b := range nonce {
		f.nonceMask[4+i] ^= b
	}
	return result
}

func (f *quicXorNonceAEAD) Open(out, nonce, ciphertext, additionalData []byte) ([]byte, error) {
	for i, b := range nonce {
		f.nonceMask[4+i] ^= b
	}
	result, err := f.aead.Open(out, f.nonceMask[:], ciphertext, additionalData)
	for i, b := range nonce {
		f.nonceMask[4+i] ^= b
	}
	return result, err
}
