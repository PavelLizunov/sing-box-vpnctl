//go:build with_awg

package wireguard

import (
	"strconv"
	"strings"

	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
)

func awgIpcLines(o option.AmneziaWGOptions) (string, error) {
	if !o.IsSet() {
		return "", nil
	}
	// Invariant: Validate raw values before normalization or generation can hide line breaks that inject configuration keys.
	for _, value := range []string{string(o.H1), string(o.H2), string(o.H3), string(o.H4), o.I1, o.I2, o.I3, o.I4, o.I5, o.Id, o.Ip, o.Ib, o.HeaderProtectionKey, o.ContentPaddingAddition, o.RekeyAfterTime} {
		if strings.ContainsAny(value, "\r\n") {
			return "", E.New("amneziawg: string options must not contain CR or LF")
		}
	}
	if err := validateJunk(o); err != nil {
		return "", err
	}
	if err := validateRekeyAfterTime(o.RekeyAfterTime); err != nil {
		return "", err
	}
	var b strings.Builder
	b.Grow(256)
	writeUint := func(key string, value uint32) {
		if value != 0 {
			b.WriteString("\n")
			b.WriteString(key)
			b.WriteString("=")
			b.WriteString(strconv.FormatUint(uint64(value), 10))
		}
	}
	var stringErr error
	writeStr := func(key, value string) {
		if strings.ContainsAny(value, "\r\n") {
			stringErr = E.New("amneziawg: ", key, " must not contain CR or LF")
			return
		}
		if value != "" {
			b.WriteString("\n")
			b.WriteString(key)
			b.WriteString("=")
			b.WriteString(value)
		}
	}
	writeMagic := func(key string, value option.MagicHeader) error {
		spec, err := value.Spec()
		if err != nil {
			return E.Cause(err, key)
		}
		writeStr(key, spec)
		return nil
	}
	i1 := o.I1
	i2 := o.I2
	masque, masque2, err := masqueI1I2(o)
	if err != nil {
		return "", err
	}
	if masque != "" {
		i1 = masque
		if masque2 != "" {
			if o.I2 != "" {
				return "", E.New("amneziawg: id/ip/ib masquerade (ip=sip) fills i2; an explicit i2 conflicts with it")
			}
			i2 = masque2
		}
	}

	writeUint("jc", o.Jc)
	writeUint("jmin", o.Jmin)
	writeUint("jmax", o.Jmax)
	writeUint("s1", o.S1)
	writeUint("s2", o.S2)
	writeUint("s3", o.S3)
	writeUint("s4", o.S4)
	if err := writeMagic("h1", o.H1); err != nil {
		return "", err
	}
	if err := writeMagic("h2", o.H2); err != nil {
		return "", err
	}
	if err := writeMagic("h3", o.H3); err != nil {
		return "", err
	}
	if err := writeMagic("h4", o.H4); err != nil {
		return "", err
	}
	writeStr("i1", i1)
	writeStr("i2", i2)
	writeStr("i3", o.I3)
	writeStr("i4", o.I4)
	writeStr("i5", o.I5)

	writeBool := func(key string, value *bool) {
		if value != nil {
			b.WriteString("\n")
			b.WriteString(key)
			b.WriteString("=")
			b.WriteString(strconv.FormatBool(*value))
		}
	}
	if o.HeaderProtectionKey != "" {
		if len(o.HeaderProtectionKey) != 64 || !isHex(o.HeaderProtectionKey) {
			return "", E.New("amneziawg: header_protection_key must be a valid 64-character hex string (32 bytes)")
		}
		if o.S1 < 12 || o.S2 < 12 || o.S3 < 12 || o.S4 < 12 {
			return "", E.New("amneziawg: s1..s4 must each be >= 12 bytes when header_protection_key is used")
		}
		writeStr("header_protection_key", o.HeaderProtectionKey)
	}
	writeStr("content_padding_addition", o.ContentPaddingAddition)
	writeStr("rekey_after_time", o.RekeyAfterTime)
	writeBool("random_trailers", o.RandomTrailers)
	writeBool("disable_cookies", o.DisableCookie)

	if stringErr != nil {
		return "", stringErr
	}
	return b.String(), nil
}

func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

const (
	awgMaxJunkCount = 128
	awgMaxJunkSize  = 65507
)

// Quirk: The full uint32 range wraps the device sampler's width to zero and causes immediate rekeying.
func validateRekeyAfterTime(value string) error {
	if value == "" {
		return nil
	}
	low, high, isRange := strings.Cut(value, "-")
	if !isRange {
		high = low
	}
	lo, err := strconv.ParseUint(low, 10, 32)
	if err != nil {
		return E.New("amneziawg: rekey_after_time must be seconds or a seconds range")
	}
	hi, err := strconv.ParseUint(high, 10, 32)
	if err != nil || hi < lo {
		return E.New("amneziawg: rekey_after_time must be seconds or a seconds range")
	}
	if lo == 0 && hi == 1<<32-1 {
		return E.New("amneziawg: rekey_after_time range is too wide")
	}
	return nil
}

// Quirk: An inverted junk-size range makes the device's random sampler panic in the retransmit-timer goroutine.
func validateJunk(o option.AmneziaWGOptions) error {
	if o.Jc > awgMaxJunkCount {
		return E.New("amneziawg: jc must be <= ", strconv.Itoa(awgMaxJunkCount))
	}
	if o.Jmax > awgMaxJunkSize {
		return E.New("amneziawg: jmax must be <= ", strconv.Itoa(awgMaxJunkSize))
	}
	if o.Jmin > o.Jmax {
		return E.New("amneziawg: jmin (", strconv.FormatUint(uint64(o.Jmin), 10), ") must be <= jmax (", strconv.FormatUint(uint64(o.Jmax), 10), ")")
	}
	return nil
}
