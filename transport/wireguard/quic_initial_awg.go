//go:build with_awg

package wireguard

import (
	"crypto"
	"crypto/aes"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/binary"
	"math/big"

	E "github.com/sagernet/sing/common/exceptions"

	"golang.org/x/crypto/hkdf"
)

func appendQUICVarint(dst []byte, v uint64) []byte {
	switch {
	case v < 1<<6:
		return append(dst, byte(v))
	case v < 1<<14:
		return append(dst, byte(v>>8)|0x40, byte(v))
	case v < 1<<30:
		return append(dst,
			byte(v>>24)|0x80, byte(v>>16), byte(v>>8), byte(v))
	default:
		return append(dst,
			byte(v>>56)|0xc0, byte(v>>48), byte(v>>40), byte(v>>32),
			byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
	}
}

const (
	quicInitialTotalLen = 1250
	quicInitialLenField = 1232
	quicPacketNumberLen = 1
	quicAEADTagLen      = 16
	quicDCIDLen         = 8
)

// Protocol: DPI evasion requires the first CRYPTO frame to have a nonzero offset and the offset-zero frame to arrive later.
type frameKind int

const (
	frameCrypto frameKind = iota
	framePing
	framePadding
)

type planEntry struct {
	kind      frameKind
	cryptoIdx int
	padLen    int
	padFlex   bool
}

type quicGenParams struct {
	fragments   int
	pings       int
	totalLenMin int
	totalLenMax int
}

func defaultQUICGenParams() quicGenParams {
	return quicGenParams{
		fragments:   6,
		pings:       2,
		totalLenMin: quicInitialTotalLen,
		totalLenMax: quicInitialTotalLen,
	}
}

type cryptoFragment struct {
	offset uint64
	data   []byte
}

func planFragmentsN(ch []byte, n int) ([]cryptoFragment, error) {
	if n < 2 {
		return nil, E.New("amneziawg: need ≥2 CRYPTO fragments")
	}
	if len(ch) < n {
		return nil, E.New("amneziawg: ClientHello too short for the requested fragment count")
	}
	cutSet := make(map[int]struct{}, n-1)
	interior := len(ch) - 1
	for len(cutSet) < n-1 {
		p, err := randInt(interior)
		if err != nil {
			return nil, err
		}
		cutSet[p+1] = struct{}{}
	}
	cuts := make([]int, 0, n+1)
	cuts = append(cuts, 0)
	for p := range cutSet {
		cuts = append(cuts, p)
	}
	cuts = append(cuts, len(ch))
	sortInts(cuts)

	frags := make([]cryptoFragment, n)
	for i := 0; i < n; i++ {
		frags[i] = cryptoFragment{offset: uint64(cuts[i]), data: ch[cuts[i]:cuts[i+1]]}
	}
	return frags, nil
}

func randomizedWirePlan(frags []cryptoFragment, pings int) ([]planEntry, error) {
	n := len(frags)
	if n < 2 {
		return nil, E.New("amneziawg: need ≥2 CRYPTO fragments for a wire plan")
	}
	if pings < 1 {
		pings = 1
	}

	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	for i := n - 1; i > 0; i-- {
		j, err := randInt(i + 1)
		if err != nil {
			return nil, err
		}
		order[i], order[j] = order[j], order[i]
	}
	if order[0] == 0 {
		order[0], order[1] = order[1], order[0]
	}

	cryptoEntries := make([]planEntry, n)
	for i, idx := range order {
		cryptoEntries[i] = planEntry{kind: frameCrypto, cryptoIdx: idx}
	}

	padRuns := pings + 2
	flexPick, err := randInt(padRuns)
	if err != nil {
		return nil, err
	}
	fillers := make([]planEntry, 0, pings+padRuns)
	for i := 0; i < pings; i++ {
		fillers = append(fillers, planEntry{kind: framePing})
	}
	for i := 0; i < padRuns; i++ {
		e := planEntry{kind: framePadding}
		if i == flexPick {
			e.padFlex = true
		} else {
			sz, err := randInt(48)
			if err != nil {
				return nil, err
			}
			e.padLen = 8 + sz
		}
		fillers = append(fillers, e)
	}

	plan := make([]planEntry, 0, n+len(fillers))
	gaps := make([][]planEntry, n)
	for _, f := range fillers {
		slot, err := randInt(n)
		if err != nil {
			return nil, err
		}
		gaps[slot] = append(gaps[slot], f)
	}
	for i, c := range cryptoEntries {
		plan = append(plan, c)
		plan = append(plan, gaps[i]...)
	}
	return plan, nil
}

func randInt(n int) (int, error) {
	if n <= 1 {
		return 0, nil
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0, err
	}
	return int(v.Int64()), nil
}

func sortInts(a []int) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j-1] > a[j]; j-- {
			a[j-1], a[j] = a[j], a[j-1]
		}
	}
}

func appendCryptoFrame(dst []byte, f cryptoFragment) []byte {
	dst = append(dst, 0x06)
	dst = appendQUICVarint(dst, f.offset)
	dst = appendQUICVarint(dst, uint64(len(f.data)))
	return append(dst, f.data...)
}

func planFixedLen(frags []cryptoFragment, plan []planEntry) (int, bool, error) {
	total := 0
	flex := false
	for _, e := range plan {
		switch e.kind {
		case frameCrypto:
			if e.cryptoIdx < 0 || e.cryptoIdx >= len(frags) {
				return 0, false, E.New("amneziawg: bad crypto fragment index in wire plan")
			}
			f := frags[e.cryptoIdx]
			total += 1 + varintLen(f.offset) + varintLen(uint64(len(f.data))) + len(f.data)
		case framePing:
			total++
		case framePadding:
			if e.padFlex {
				flex = true
				continue
			}
			total += e.padLen
		}
	}
	return total, flex, nil
}

func buildInitialPayload(frags []cryptoFragment, plan []planEntry, targetLen int) ([]byte, error) {
	fixed, hasFlex, err := planFixedLen(frags, plan)
	if err != nil {
		return nil, err
	}
	if !hasFlex {
		return nil, E.New("amneziawg: wire plan has no flex PADDING run to absorb slack")
	}
	flexLen := targetLen - fixed
	if flexLen < 0 {
		return nil, E.New("amneziawg: QUIC Initial frames overflow the length field (ClientHello too large)")
	}

	out := make([]byte, 0, targetLen)
	for _, e := range plan {
		switch e.kind {
		case frameCrypto:
			out = appendCryptoFrame(out, frags[e.cryptoIdx])
		case framePing:
			out = append(out, 0x01)
		case framePadding:
			n := e.padLen
			if e.padFlex {
				n = flexLen
			}
			for i := 0; i < n; i++ {
				out = append(out, 0x00)
			}
		}
	}
	if len(out) != targetLen {
		return nil, E.New("amneziawg: QUIC Initial payload did not land on the length field")
	}
	return out, nil
}

func varintLen(v uint64) int {
	switch {
	case v < 1<<6:
		return 1
	case v < 1<<14:
		return 2
	case v < 1<<30:
		return 4
	default:
		return 8
	}
}

func deriveInitialKeys(dcid []byte) (key, iv, hp []byte) {
	initialSecret := hkdf.Extract(crypto.SHA256.New, dcid, quicSaltV1)
	clientSecret := quicHKDFExpandLabel(crypto.SHA256, initialSecret, []byte{}, "client in", crypto.SHA256.Size())
	key = quicHKDFExpandLabel(crypto.SHA256, clientSecret, []byte{}, "quic key", 16)
	iv = quicHKDFExpandLabel(crypto.SHA256, clientSecret, []byte{}, "quic iv", 12)
	hp = quicHKDFExpandLabel(crypto.SHA256, clientSecret, []byte{}, "quic hp", 16)
	return
}

func encryptInitial(header, payload, key, iv, hp []byte, pnOffset int, pn uint64) ([]byte, error) {
	cipher := quicAEADAESGCMTLS13(key, iv)
	// Quirk: The AEAD accepts an 8-byte packet number and XORs it into the IV internally.
	nonce := make([]byte, cipher.NonceSize())
	binary.BigEndian.PutUint64(nonce[cipher.NonceSize()-8:], pn)
	ciphertext := cipher.Seal(nil, nonce, payload, header)

	packet := make([]byte, 0, len(header)+len(ciphertext))
	packet = append(packet, header...)
	packet = append(packet, ciphertext...)

	sampleOffset := pnOffset + 4
	if sampleOffset+aes.BlockSize > len(packet) {
		return nil, E.New("amneziawg: QUIC Initial too short to sample for header protection")
	}
	block, err := aes.NewCipher(hp)
	if err != nil {
		return nil, err
	}
	mask := make([]byte, aes.BlockSize)
	block.Encrypt(mask, packet[sampleOffset:sampleOffset+aes.BlockSize])
	packet[0] ^= mask[0] & 0x0f
	for i := 0; i < quicPacketNumberLen; i++ {
		packet[pnOffset+i] ^= mask[1+i]
	}
	return packet, nil
}

func buildInitialPacket(sni, browser string, p quicGenParams) ([]byte, error) {
	dcid := make([]byte, quicDCIDLen)
	if _, err := rand.Read(dcid); err != nil {
		return nil, err
	}
	var tlsRandom [32]byte
	if _, err := rand.Read(tlsRandom[:]); err != nil {
		return nil, err
	}
	ecKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	clientHello, err := buildClientHello(sni, tlsRandom, ecKey.PublicKey().Bytes(), browser)
	if err != nil {
		return nil, err
	}

	frags, err := planFragmentsN(clientHello, p.fragments)
	if err != nil {
		return nil, err
	}
	plan, err := randomizedWirePlan(frags, p.pings)
	if err != nil {
		return nil, err
	}

	totalLen, err := pickTotalLen(p)
	if err != nil {
		return nil, err
	}

	const headerFixed = 1 + 4 + 1 + 1 + 1
	const lenFieldVarintWidth = 2
	headerLen := headerFixed + quicDCIDLen + lenFieldVarintWidth + quicPacketNumberLen
	payloadLen := totalLen - headerLen - quicAEADTagLen
	lengthField := quicPacketNumberLen + payloadLen + quicAEADTagLen
	if lengthField < 1<<6 || lengthField >= 1<<14 {
		return nil, E.New("amneziawg: QUIC Initial length field outside 2-byte varint range")
	}

	payload, err := buildInitialPayload(frags, plan, payloadLen)
	if err != nil {
		return nil, err
	}

	header := make([]byte, 0, headerLen)
	header = append(header, 0xC0|byte(quicPacketNumberLen-1))
	header = binary.BigEndian.AppendUint32(header, quicVersion1)
	header = append(header, byte(quicDCIDLen))
	header = append(header, dcid...)
	header = append(header, 0x00)
	header = append(header, 0x00)
	header = appendQUICVarint(header, uint64(lengthField))
	pnOffset := len(header)
	for i := 0; i < quicPacketNumberLen; i++ {
		header = append(header, 0x00)
	}

	key, iv, hp := deriveInitialKeys(dcid)
	packet, err := encryptInitial(header, payload, key, iv, hp, pnOffset, 0)
	if err != nil {
		return nil, err
	}
	if len(packet) != totalLen {
		return nil, E.New("amneziawg: QUIC Initial assembled to unexpected size")
	}
	return packet, nil
}

func pickTotalLen(p quicGenParams) (int, error) {
	lo, hi := p.totalLenMin, p.totalLenMax
	if lo < 1200 {
		lo = 1200
	}
	if hi < lo {
		hi = lo
	}
	if hi == lo {
		return lo, nil
	}
	d, err := randInt(hi - lo + 1)
	if err != nil {
		return 0, err
	}
	return lo + d, nil
}

// Invariant: The DCID stays fixed after encryption because it determines the Initial keys.
func masqueQUICInitialCPS(domain, browser string) (string, error) {
	packet, err := buildInitialPacket(domain, browser, defaultQUICGenParams())
	if err != nil {
		return "", err
	}
	var b cpsBuilder
	b.addBytes(packet)
	return b.String(), nil
}
