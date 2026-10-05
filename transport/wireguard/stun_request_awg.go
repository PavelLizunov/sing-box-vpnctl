//go:build with_awg

package wireguard

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"hash/crc32"
)

const (
	stunTypeBindingRequest = 0x0001
	stunAttrUsername       = 0x0006
	stunAttrMsgIntegrity   = 0x0008
	stunAttrPriority       = 0x0024
	stunAttrSoftware       = 0x8022
	stunAttrIceControlling = 0x802a
	stunAttrFingerprint    = 0x8028
	stunFingerprintXOR     = 0x5354554e
)

const stunSoftwareProduct = "libwebrtc"

func appendSTUNAttr(dst []byte, typ uint16, val []byte) []byte {
	var h [4]byte
	binary.BigEndian.PutUint16(h[0:], typ)
	binary.BigEndian.PutUint16(h[2:], uint16(len(val)))
	dst = append(dst, h[:]...)
	dst = append(dst, val...)
	for len(dst)%4 != 0 {
		dst = append(dst, 0x00)
	}
	return dst
}

func buildSTUNBindingRequest() ([]byte, error) {
	txn := make([]byte, 12)
	if _, err := rand.Read(txn); err != nil {
		return nil, err
	}
	ufrag := make([]byte, 8)
	if _, err := rand.Read(ufrag); err != nil {
		return nil, err
	}
	tie := make([]byte, 8)
	if _, err := rand.Read(tie); err != nil {
		return nil, err
	}
	prio := make([]byte, 4)
	if _, err := rand.Read(prio); err != nil {
		return nil, err
	}
	integrityKey := make([]byte, 16)
	if _, err := rand.Read(integrityKey); err != nil {
		return nil, err
	}

	username := stunICEUsername(ufrag)

	var attrs []byte
	attrs = appendSTUNAttr(attrs, stunAttrUsername, username)
	attrs = appendSTUNAttr(attrs, stunAttrIceControlling, tie)
	attrs = appendSTUNAttr(attrs, stunAttrPriority, prio)
	attrs = appendSTUNAttr(attrs, stunAttrSoftware, []byte(stunSoftwareProduct))

	const miAttrTotal = 24
	header := make([]byte, 20)
	binary.BigEndian.PutUint16(header[0:], stunTypeBindingRequest)
	binary.BigEndian.PutUint32(header[4:], stunMagicCookie)
	copy(header[8:], txn)

	binary.BigEndian.PutUint16(header[2:], uint16(len(attrs)+miAttrTotal))
	preMI := append(append([]byte{}, header...), attrs...)
	mac := hmac.New(sha1.New, integrityKey)
	mac.Write(preMI)
	mi := mac.Sum(nil)
	withMI := appendSTUNAttr(preMI, stunAttrMsgIntegrity, mi)

	const fpAttrTotal = 8
	binary.BigEndian.PutUint16(withMI[2:], uint16(len(attrs)+miAttrTotal+fpAttrTotal))
	crc := crc32.ChecksumIEEE(withMI) ^ stunFingerprintXOR
	var fp [4]byte
	binary.BigEndian.PutUint32(fp[:], crc)
	pkt := appendSTUNAttr(withMI, stunAttrFingerprint, fp[:])

	return pkt, nil
}

func stunICEUsername(seed []byte) []byte {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, 0, 9)
	for i := 0; i < 4; i++ {
		out = append(out, hexdigits[seed[i]%16])
	}
	out = append(out, ':')
	for i := 4; i < 8; i++ {
		out = append(out, hexdigits[seed[i]%16])
	}
	return out
}

func masqueSTUNRequestCPS() (string, error) {
	pkt, err := buildSTUNBindingRequest()
	if err != nil {
		return "", err
	}
	var b cpsBuilder
	b.addBytes(pkt)
	return b.String(), nil
}
