//go:build with_awg

package wireguard

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
)

const (
	masqueProtoQUIC = "quic"
	masqueProtoDNS  = "dns"
	masqueProtoSTUN = "stun"
	masqueProtoSIP  = "sip"
)

func masqueI1(o option.AmneziaWGOptions) (string, error) {
	if o.Id == "" && o.Ip == "" && o.Ib == "" {
		return "", nil
	}

	if o.I1 != "" {
		return "", E.New("amneziawg: id/ip/ib masquerade conflicts with an explicit i1; use one or the other")
	}

	proto := strings.ToLower(strings.TrimSpace(o.Ip))
	if proto == "" {
		return "", E.New("amneziawg: ip (masquerade protocol) is required when id/ib is set; one of quic|dns|stun|sip")
	}

	domain := strings.TrimSpace(o.Id)
	if domain != "" {
		if err := validateMasqueDomain(domain); err != nil {
			return "", err
		}
	}

	browser, err := normalizeMasqueBrowser(o.Ib, proto)
	if err != nil {
		return "", err
	}

	switch proto {
	case masqueProtoQUIC:
		if domain == "" {
			return "", E.New("amneziawg: id (masquerade domain) is required for ip=quic (it becomes the ClientHello SNI)")
		}
		return masqueQUICInitialCPS(domain, browser)
	case masqueProtoSTUN:
		return masqueSTUNRequestCPS()
	case masqueProtoDNS:
		if domain == "" {
			domain = pgDomainHost()
		}
		return masqueDNSQueryCPS(domain)
	case masqueProtoSIP:
		return masqueSIPInviteCPS(newSIPDialog(domain))
	default:
		return "", E.New("amneziawg: unknown masquerade protocol ", strconv.Quote(proto), "; one of quic|dns|stun|sip")
	}
}

func masqueI1I2(o option.AmneziaWGOptions) (i1, i2 string, err error) {
	i1, err = masqueI1(o)
	if err != nil || i1 == "" {
		return i1, "", err
	}
	proto := strings.ToLower(strings.TrimSpace(o.Ip))
	domain := strings.TrimSpace(o.Id)
	switch proto {
	case masqueProtoSIP:
		d := newSIPDialog(domain)
		invite, err := masqueSIPInviteCPS(d)
		if err != nil {
			return "", "", err
		}
		trying, err := masqueSIPTryingCPS(d)
		if err != nil {
			return "", "", err
		}
		i1, i2 = invite, trying
	}
	return i1, i2, nil
}

func validateMasqueDomain(domain string) error {
	if domain == "" {
		return E.New("amneziawg: id (masquerade domain) is required when ip/ib is set")
	}
	name := strings.TrimSuffix(domain, ".")
	if name == "" || len(name) > 253 {
		return E.New("amneziawg: invalid masquerade domain ", strconv.Quote(domain), ": empty or longer than 253 bytes")
	}
	if strings.HasPrefix(name, ".") {
		return E.New("amneziawg: invalid masquerade domain ", strconv.Quote(domain), ": leading dot")
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" {
			return E.New("amneziawg: invalid masquerade domain ", strconv.Quote(domain), ": empty label")
		}
		if len(label) > 63 {
			return E.New("amneziawg: invalid masquerade domain ", strconv.Quote(domain), ": label longer than 63 bytes")
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return E.New("amneziawg: invalid masquerade domain ", strconv.Quote(domain), ": label with leading/trailing hyphen")
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
				(c >= '0' && c <= '9') || c == '-' || c == '_'
			if !ok {
				return E.New("amneziawg: invalid masquerade domain ", strconv.Quote(domain), ": illegal character (only a-z A-Z 0-9 - _ allowed)")
			}
		}
	}
	return nil
}

const (
	masqueBrowserChrome  = "chrome"
	masqueBrowserFirefox = "firefox"
	masqueBrowserCurl    = "curl"
)

func normalizeMasqueBrowser(ib, proto string) (string, error) {
	browser := strings.ToLower(strings.TrimSpace(ib))
	if browser == "" {
		return "", nil
	}
	switch browser {
	case masqueBrowserChrome, masqueBrowserFirefox, masqueBrowserCurl:
	default:
		return "", E.New("amneziawg: unknown masquerade browser ", strconv.Quote(ib), "; one of chrome|firefox|curl")
	}
	if proto != masqueProtoQUIC {
		return "", E.New("amneziawg: ib (browser) is only meaningful with ip=quic, got ip=", strconv.Quote(proto))
	}
	return browser, nil
}

type cpsBuilder struct {
	parts []string
}

func (c *cpsBuilder) addBytes(b []byte) {
	if len(b) == 0 {
		return
	}
	c.parts = append(c.parts, "<b 0x"+hex.EncodeToString(b)+">")
}

func (c *cpsBuilder) addRand(n int) {
	if n <= 0 {
		return
	}
	c.parts = append(c.parts, fmt.Sprintf("<r %d>", n))
}

func (c *cpsBuilder) addRandChars(n int) {
	if n <= 0 {
		return
	}
	c.parts = append(c.parts, fmt.Sprintf("<rc %d>", n))
}

func (c *cpsBuilder) addRandDigits(n int) {
	if n <= 0 {
		return
	}
	c.parts = append(c.parts, fmt.Sprintf("<rd %d>", n))
}

func (c *cpsBuilder) String() string {
	return strings.Join(c.parts, "")
}

const (
	dnsOptUDPSize   uint16 = 1232
	dnsOptCoverCode uint16 = 0xFDE9
	dnsCoverLen            = 40
	dnsQTypeHTTPS   uint16 = 0x0041
)

func masqueDNSQueryCPS(domain string) (string, error) {
	qname, err := encodeDNSName(domain)
	if err != nil {
		return "", err
	}

	qtHi, qtLo := be16(dnsQTypeHTTPS)
	question := make([]byte, 0, len(qname)+4)
	question = append(question, qname...)
	question = append(question, qtHi, qtLo, 0x00, 0x01)

	const optOptionHdrLen = 4

	optLen := uint16(dnsCoverLen)
	rdLength := uint16(optOptionHdrLen + dnsCoverLen)

	var hdr cpsBuilder

	hdr.addRand(2)
	hdr.addBytes([]byte{
		0x01, 0x00,
		0x00, 0x01,
		0x00, 0x00,
		0x00, 0x00,
		0x00, 0x01,
	})

	hdr.addBytes(question)

	udpHi, udpLo := be16(dnsOptUDPSize)
	rdHi, rdLo := be16(rdLength)
	ocHi, ocLo := be16(dnsOptCoverCode)
	olHi, olLo := be16(optLen)
	opt := []byte{
		0x00,
		0x00, 0x29,
		udpHi, udpLo,
		0x00, 0x00, 0x00, 0x00,
		rdHi, rdLo,
		ocHi, ocLo,
		olHi, olLo,
	}
	hdr.addBytes(opt)

	hdr.addRand(dnsCoverLen)

	return hdr.String(), nil
}

func encodeDNSName(domain string) ([]byte, error) {
	name := strings.TrimSuffix(domain, ".")
	labels := strings.Split(name, ".")
	out := make([]byte, 0, len(name)+2)
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 {
			return nil, E.New("amneziawg: cannot encode DNS label ", strconv.Quote(label))
		}
		out = append(out, byte(len(label)))
		out = append(out, label...)
	}
	out = append(out, 0x00)
	return out, nil
}

const stunMagicCookie uint32 = 0x2112A442

func be16(v uint16) (hi, lo byte) {
	return byte(v >> 8), byte(v & 0xFF)
}
