//go:build with_awg

package wireguard

import (
	"encoding/binary"
	"hash/crc32"
	"strings"
	"testing"

	"github.com/sagernet/sing-box/option"

	"github.com/stretchr/testify/require"
)

func obfuscateCPS(t *testing.T, spec string) []byte {
	t.Helper()
	chain, err := parseCPS(spec)
	require.NoError(t, err, "spec must parse: %q", spec)
	return chain.obfuscate()
}

func TestMasqueI1Empty(t *testing.T) {
	t.Parallel()
	s, err := masqueI1(option.AmneziaWGOptions{})
	require.NoError(t, err)
	require.Equal(t, "", s, "no id/ip/ib -> no masquerade")
}

func TestMasqueI1ConflictWithExplicitI1(t *testing.T) {
	t.Parallel()
	_, err := masqueI1(option.AmneziaWGOptions{I1: "<b 0x0844>", Id: "a.com", Ip: "quic"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "conflicts with an explicit i1")
}

func TestMasqueI1UnknownProtocol(t *testing.T) {
	t.Parallel()
	_, err := masqueI1(option.AmneziaWGOptions{Id: "a.com", Ip: "ftp"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown masquerade protocol")
}

func TestMasqueI1MissingProtocol(t *testing.T) {
	t.Parallel()
	_, err := masqueI1(option.AmneziaWGOptions{Id: "a.com"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "ip (masquerade protocol) is required")
}

func TestMasqueI1DomainRequiredForQUICOnly(t *testing.T) {
	t.Parallel()
	_, err := masqueI1(option.AmneziaWGOptions{Ip: "quic"})
	require.Error(t, err, "id should be required for ip=quic")
	require.Contains(t, err.Error(), "id (masquerade domain) is required for ip=quic")
}

func TestMasqueI1DomainOptionalForNonQUIC(t *testing.T) {
	t.Parallel()
	stun, err := masqueI1(option.AmneziaWGOptions{Ip: "stun"})
	require.NoError(t, err, "stun without id should be valid")
	require.NotEmpty(t, stun)
	pkt := obfuscateCPS(t, stun)
	require.Equal(t, uint16(0x0001), uint16(pkt[0])<<8|uint16(pkt[1]), "stun-without-id is a Binding Request")

	sip, err := masqueI1(option.AmneziaWGOptions{Ip: "sip"})
	require.NoError(t, err, "sip without id should be valid (pseudo-host)")
	require.True(t, strings.HasPrefix(string(obfuscateCPS(t, sip)), "INVITE sip:"), "sip-without-id is an INVITE")

	dns, err := masqueI1(option.AmneziaWGOptions{Ip: "dns"})
	require.NoError(t, err, "dns without id should be valid (pseudo-domain)")
	dpkt := obfuscateCPS(t, dns)
	require.Equal(t, byte(0x00), dpkt[2]&0x80, "dns-without-id is a query (QR=0)")
	name, off := parseDNSName(t, dpkt, 12)
	require.NotEmpty(t, name, "QNAME is a generated pseudo-domain")
	require.NotContains(t, name, "_", "pseudo-domain has no underscore")
	require.Contains(t, name, ".", "pseudo-domain is multi-label (not a bare IP)")
	require.Equal(t, uint16(0x0041), binary.BigEndian.Uint16(dpkt[off:off+2]), "QTYPE HTTPS")

	_, err = masqueI1(option.AmneziaWGOptions{Ip: "quic", Id: "a.com\r\nx"})
	require.Error(t, err, "invalid id must be rejected")
	require.Contains(t, err.Error(), "invalid masquerade domain")
}

func TestMasqueProtocolCaseInsensitive(t *testing.T) {
	t.Parallel()
	s, err := masqueI1(option.AmneziaWGOptions{Id: "a.com", Ip: "QUIC", Ib: "Chrome"})
	require.NoError(t, err)
	require.NotEmpty(t, s)
}

func TestValidateMasqueDomainAccepts(t *testing.T) {
	t.Parallel()
	for _, d := range []string{
		"a.com", "www.google.com", "ozon.ru", "sub-domain.example.co.uk",
		"xn--80ak6aa92e.com", "_dmarc.example.com", "a.com.",
		"localhost",
	} {
		require.NoError(t, validateMasqueDomain(d), "should accept %q", d)
	}
}

func TestValidateMasqueDomainRejectsInjection(t *testing.T) {
	t.Parallel()
	for _, d := range []string{
		"a.com\nx",
		"a.com\r\nVia: evil",
		"a.com\x00",
		"a.com\t",
		"a.com>;q=1",
		"a.com;tag=x",
		"a.com@evil",
		"a com",
		"a.com\"",
		"-leading.com",
		"trailing-.com",
		".leading-dot.com",
		"a..com",
		"",
		strings.Repeat("a", 64) + ".com",
		strings.Repeat("a.", 130) + "a",
	} {
		require.Error(t, validateMasqueDomain(d), "should reject %q", d)
	}
}

func TestMasqueBrowserValidation(t *testing.T) {
	t.Parallel()
	_, err := masqueI1(option.AmneziaWGOptions{Id: "a.com", Ip: "quic", Ib: "safari"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown masquerade browser")

	_, err = masqueI1(option.AmneziaWGOptions{Id: "a.com", Ip: "dns", Ib: "chrome"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "only meaningful with ip=quic")
}

func TestMasqueDNSQueryStructure(t *testing.T) {
	t.Parallel()
	spec, err := masqueI1(option.AmneziaWGOptions{Id: "www.google.com", Ip: "dns"})
	require.NoError(t, err)
	pkt := obfuscateCPS(t, spec)

	require.GreaterOrEqual(t, len(pkt), 12, "DNS header")

	require.Equal(t, byte(0x01), pkt[2], "QR=0, RD=1")
	require.Equal(t, byte(0x00), pkt[3], "byte 3 zero (RA=0, RCODE=0)")
	require.Equal(t, byte(0x00), pkt[2]&0x80, "QR bit clear (query)")
	require.Equal(t, uint16(1), binary.BigEndian.Uint16(pkt[4:6]), "QDCOUNT=1")
	require.Equal(t, uint16(0), binary.BigEndian.Uint16(pkt[6:8]), "ANCOUNT=0")
	require.Equal(t, uint16(0), binary.BigEndian.Uint16(pkt[8:10]), "NSCOUNT=0")
	require.Equal(t, uint16(1), binary.BigEndian.Uint16(pkt[10:12]), "ARCOUNT=1 (OPT)")

	name, off := parseDNSName(t, pkt, 12)
	require.Equal(t, "www.google.com", name, "QNAME must encode the configured domain")
	require.Equal(t, uint16(0x0041), binary.BigEndian.Uint16(pkt[off:off+2]), "QTYPE HTTPS(65)")
	require.Equal(t, uint16(1), binary.BigEndian.Uint16(pkt[off+2:off+4]), "QCLASS IN")
	off += 4

	require.Equal(t, byte(0x00), pkt[off], "OPT NAME root label")
	require.Equal(t, uint16(41), binary.BigEndian.Uint16(pkt[off+1:off+3]), "TYPE OPT(41)")
	require.Equal(t, uint16(1232), binary.BigEndian.Uint16(pkt[off+3:off+5]), "CLASS udp-size")
	require.Equal(t, uint32(0), binary.BigEndian.Uint32(pkt[off+5:off+9]), "TTL 0")
	rdlength := binary.BigEndian.Uint16(pkt[off+9 : off+11])
	rdataStart := off + 11
	require.Equal(t, len(pkt), rdataStart+int(rdlength), "RDLENGTH covers to end")

	require.Equal(t, uint16(0xFDE9), binary.BigEndian.Uint16(pkt[rdataStart:rdataStart+2]), "OPTION-CODE")
	optLen := binary.BigEndian.Uint16(pkt[rdataStart+2 : rdataStart+4])
	require.Equal(t, len(pkt), rdataStart+4+int(optLen), "OPTION-LENGTH covers to end")
}

func TestMasqueSTUNRequestStructure(t *testing.T) {
	t.Parallel()
	spec, err := masqueI1(option.AmneziaWGOptions{Ip: "stun"})
	require.NoError(t, err)
	pkt := obfuscateCPS(t, spec)

	require.GreaterOrEqual(t, len(pkt), 20, "STUN header")
	require.Equal(t, uint16(0x0001), binary.BigEndian.Uint16(pkt[0:2]), "Binding Request")
	require.Equal(t, uint32(0x2112A442), binary.BigEndian.Uint32(pkt[4:8]), "magic cookie")
	require.Equal(t, byte(0x00), pkt[0]&0xC0, "STUN leading two bits zero")

	msgLen := binary.BigEndian.Uint16(pkt[2:4])
	require.Equal(t, len(pkt), 20+int(msgLen), "message length covers all attributes incl FINGERPRINT")

	off := 20
	end := 20 + int(msgLen)
	var seen []uint16
	fpOff := -1
	for off < end {
		require.LessOrEqual(t, off+4, end, "attribute header must fit")
		atype := binary.BigEndian.Uint16(pkt[off : off+2])
		alen := int(binary.BigEndian.Uint16(pkt[off+2 : off+4]))
		require.LessOrEqual(t, off+4+alen, end, "attribute value must fit")
		seen = append(seen, atype)
		if atype == 0x8028 {
			fpOff = off
			require.Equal(t, 4, alen, "FINGERPRINT value is 4 bytes")
		}
		if atype == 0x0008 {
			require.Equal(t, 20, alen, "MESSAGE-INTEGRITY is 20 bytes (HMAC-SHA1)")
		}
		off += 4 + ((alen + 3) &^ 3)
	}
	require.Equal(t, end, off, "attributes tile the message exactly")
	require.Contains(t, seen, uint16(0x0006), "USERNAME present")
	require.Contains(t, seen, uint16(0x0008), "MESSAGE-INTEGRITY present")
	require.Equal(t, uint16(0x8028), seen[len(seen)-1], "FINGERPRINT is the last attribute")

	require.Greater(t, fpOff, 0)
	wantCRC := crc32.ChecksumIEEE(pkt[:fpOff]) ^ 0x5354554e
	require.Equal(t, wantCRC, binary.BigEndian.Uint32(pkt[fpOff+4:fpOff+8]), "FINGERPRINT CRC-32 must verify")
}

func TestMasqueSTUNRequestUniqueness(t *testing.T) {
	t.Parallel()
	a, err := masqueI1(option.AmneziaWGOptions{Ip: "stun"})
	require.NoError(t, err)
	b, err := masqueI1(option.AmneziaWGOptions{Ip: "stun"})
	require.NoError(t, err)
	require.NotEqual(t, a, b, "fresh per-call entropy → different blobs")
}

func TestMasqueSIPInviteStructure(t *testing.T) {
	t.Parallel()
	const host = "pbx.example.com"
	i1, i2, err := masqueI1I2(option.AmneziaWGOptions{Id: host, Ip: "sip"})
	require.NoError(t, err)
	require.NotEmpty(t, i1, "i1 (INVITE) present")
	require.NotEmpty(t, i2, "i2 (100 Trying) present")
	invite := string(obfuscateCPS(t, i1))
	trying := string(obfuscateCPS(t, i2))

	assertSIPInvite(t, invite, host)
	assertSIPTrying(t, trying)
	assertSameSIPDialog(t, invite, trying)

	requestURI := invite[len("INVITE sip:"):strings.Index(invite, " SIP/2.0")]
	require.NotContains(t, requestURI, "@"+host, "callee (request-URI) is NOT the configured id — a real call dials out")
	uaHost := sipField(t, invite, "Via: SIP/2.0/UDP ", ";")
	require.True(t, strings.HasPrefix(uaHost, "pc"), "UA host is a pcNN subdomain: %q", uaHost)
	require.True(t, strings.HasSuffix(uaHost, "."+host), "UA host is a subdomain of the configured id: %q", uaHost)
	require.Contains(t, invite, "Call-ID: "+sipField(t, invite, "Call-ID: ", "@")+"@"+uaHost, "Call-ID host == UA host")
	require.Contains(t, invite, "Contact: <sip:"+sipField(t, invite, "Contact: <sip:", "@")+"@"+uaHost+">", "Contact host == UA host")

	for _, beacon := range []string{"alice@", "bob@", "biloxi.com", "atlanta.com"} {
		require.NotContains(t, invite, beacon, "names must be generated, not the RFC template")
	}
}

func TestMasqueSIPInviteNoID(t *testing.T) {
	t.Parallel()
	i1, i2, err := masqueI1I2(option.AmneziaWGOptions{Ip: "sip"})
	require.NoError(t, err, "sip without id must succeed (pseudo-host generated)")
	require.NotEmpty(t, i1)
	require.NotEmpty(t, i2)
	invite := string(obfuscateCPS(t, i1))
	trying := string(obfuscateCPS(t, i2))
	assertSIPInvite(t, invite, "")
	assertSIPTrying(t, trying)
	assertSameSIPDialog(t, invite, trying)
}

func assertSIPInvite(t *testing.T, text, host string) {
	t.Helper()
	require.True(t, strings.HasPrefix(text, "INVITE sip:"), "request-line starts with INVITE method")
	rlEnd := strings.Index(text, "\r\n")
	require.Greater(t, rlEnd, 0)
	require.True(t, strings.HasSuffix(text[:rlEnd], " SIP/2.0"), "request-line ends SIP/2.0")
	if host != "" {
		fromLine := sipField(t, text, "\r\nFrom: ", "\r\n")
		require.Contains(t, fromLine, "@"+host+">", "configured id is the caller domain (From host)")
	}
	assertSIPHeaderBlock(t, text)
	for _, h := range []string{
		"\r\nMax-Forwards: 70\r\n",
		"\r\nContact: <sip:",
		"\r\nContent-Type: application/sdp\r\n",
		"\r\nContent-Length: 0\r\n",
	} {
		require.Contains(t, text, h, "missing/garbled INVITE header: %q", h)
	}
	require.True(t, strings.HasSuffix(text, "\r\n\r\n"), "INVITE has no body (ends at blank line)")
	require.Equal(t, strings.Index(text, "\r\n\r\n")+4, len(text), "nothing follows the header block")
}

func assertSIPTrying(t *testing.T, text string) {
	t.Helper()
	require.True(t, strings.HasPrefix(text, "SIP/2.0 100 Trying\r\n"), "status line is 100 Trying")
	assertSIPHeaderBlock(t, text)
	require.Contains(t, text, "\r\nContent-Length: 0\r\n", "100 Trying has Content-Length: 0")
	require.NotContains(t, text, "Max-Forwards", "100 Trying must not carry Max-Forwards")
	require.NotContains(t, text, "Contact:", "100 Trying must not carry Contact")
	require.True(t, strings.HasSuffix(text, "\r\n\r\n"), "100 Trying ends at the blank line")
}

func assertSIPHeaderBlock(t *testing.T, text string) {
	t.Helper()
	headerEnd := strings.Index(text, "\r\n\r\n")
	require.GreaterOrEqual(t, headerEnd, 0, "header block must terminate with a blank line")
	for _, line := range strings.Split(text[:headerEnd], "\r\n")[1:] {
		require.Contains(t, line, ":", "every SIP header line must contain ':' : %q", line)
	}
	for _, h := range []string{
		"\r\nVia: SIP/2.0/UDP ",
		";branch=z9hG4bK",
		"\r\nTo: ",
		"\r\nFrom: ",
		";tag=",
		"\r\nCall-ID: ",
		"\r\nCSeq: ",
		" INVITE\r\n",
	} {
		require.Contains(t, text, h, "missing/garbled shared header: %q", h)
	}
	toIdx := strings.Index(text, "\r\nTo: ")
	toLine := text[toIdx+2:]
	toLine = toLine[:strings.Index(toLine, "\r\n")]
	require.NotContains(t, toLine, ";tag=", "To must have no tag in the initial transaction")
}

func assertSameSIPDialog(t *testing.T, invite, trying string) {
	t.Helper()
	for _, field := range []struct{ name, prefix, end string }{
		{"Via branch", "branch=z9hG4bK", "\r\n"},
		{"From tag", ";tag=", "\r\n"},
		{"Call-ID", "\r\nCall-ID: ", "\r\n"},
		{"CSeq", "\r\nCSeq: ", "\r\n"},
	} {
		a := sipField(t, invite, field.prefix, field.end)
		b := sipField(t, trying, field.prefix, field.end)
		require.Equal(t, a, b, "%s must match across INVITE and 100 Trying", field.name)
	}
}

func sipField(t *testing.T, text, prefix, end string) string {
	t.Helper()
	i := strings.Index(text, prefix)
	require.GreaterOrEqual(t, i, 0, "field prefix %q present", prefix)
	rest := text[i+len(prefix):]
	j := strings.Index(rest, end)
	require.GreaterOrEqual(t, j, 0, "field terminator %q present after %q", end, prefix)
	return rest[:j]
}

func TestMasqueSIPDomainCannotInject(t *testing.T) {
	t.Parallel()
	_, err := masqueI1(option.AmneziaWGOptions{Id: "a.com\r\nEvil: 1", Ip: "sip"})
	require.Error(t, err, "CRLF domain must be rejected before reaching the generator")
}

func parseDNSName(t *testing.T, pkt []byte, off int) (string, int) {
	t.Helper()
	var labels []string
	for {
		require.Less(t, off, len(pkt), "DNS name must terminate")
		l := int(pkt[off])
		off++
		if l == 0 {
			break
		}
		require.LessOrEqual(t, off+l, len(pkt), "DNS label must fit")
		labels = append(labels, string(pkt[off:off+l]))
		off += l
	}
	return strings.Join(labels, "."), off
}
