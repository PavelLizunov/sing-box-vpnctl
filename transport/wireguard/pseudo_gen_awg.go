//go:build with_awg

package wireguard

import (
	"crypto/rand"
	"math/big"
	"strings"
)

var pgCons = []string{
	"b", "c", "d", "f", "g", "h", "j", "k", "l", "m",
	"n", "p", "r", "s", "t", "v", "w", "z",
}

var pgVow = []string{"a", "e", "i", "o", "u"}

var pgOnset = []string{
	"br", "tr", "cr", "dr", "fr", "gr", "pr", "st", "sp", "sk",
	"sl", "sm", "sn", "bl", "cl", "fl", "gl", "pl", "tw", "sh",
}

var pgCoda = []string{
	"lk", "sk", "st", "nk", "sh", "nt", "rk", "ng",
}

var pgTLD = []string{"com", "net", "org", "io", "co"}

func pgIntn(n int) int {
	if n <= 1 {
		return 0
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		panic("amneziawg: crypto/rand failed in PseudoGen")
	}
	return int(v.Int64())
}

func pgPick(seq []string) string { return seq[pgIntn(len(seq))] }

func pgChance(pct int) bool { return pgIntn(100) < pct }

func pgWord() string {
	var b strings.Builder
	b.WriteString(pgPick(pgCons))
	b.WriteString(pgPick(pgVow))
	blocks := 2 + pgIntn(2)
	for i := 0; i < blocks; i++ {
		if pgChance(60) {
			b.WriteString(pgPick(pgCons))
		} else {
			b.WriteString(pgPick(pgOnset))
		}
		b.WriteString(pgPick(pgVow))
	}
	if pgChance(50) {
		if pgChance(30) {
			b.WriteString(pgPick(pgCoda))
		} else {
			b.WriteString(pgPick(pgCons))
		}
	}
	return b.String()
}

func pgUser() string {
	u := pgWord()
	if pgChance(30) {
		return u + "_" + pgWord()
	}
	return u
}

func pgHex(n int) string {
	const hexDigits = "0123456789abcdef"
	var b strings.Builder
	b.Grow(n)
	for i := 0; i < n; i++ {
		b.WriteByte(hexDigits[pgIntn(16)])
	}
	return b.String()
}

func pgDigits(n int) string {
	if n <= 0 {
		return ""
	}
	var b strings.Builder
	b.Grow(n)
	b.WriteByte(byte('1' + pgIntn(9)))
	for i := 1; i < n; i++ {
		b.WriteByte(byte('0' + pgIntn(10)))
	}
	return b.String()
}

func pgIsReservedIP(a, b int) bool {
	switch {
	case a == 0 || a == 10 || a == 127:
		return true
	case a == 172 && b >= 16 && b <= 31:
		return true
	case a == 192 && b == 168:
		return true
	case a == 169 && b == 254:
		return true
	case a == 100 && b >= 64 && b <= 127:
		return true
	case a >= 224:
		return true
	}
	return false
}

func pgPublicIP() string {
	for {
		a := 1 + pgIntn(223)
		b := pgIntn(256)
		if pgIsReservedIP(a, b) {
			continue
		}
		c := pgIntn(256)
		d := 1 + pgIntn(254)
		return itoa(a) + "." + itoa(b) + "." + itoa(c) + "." + itoa(d)
	}
}

func pgHost() string {
	switch r := pgIntn(100); {
	case r < 30:
		return pgWord() + "." + pgWord() + "." + pgPick(pgTLD)
	case r < 60:
		return pgPublicIP()
	case r < 90:
		return pgWord() + "." + pgPick(pgTLD)
	default:
		return "sip." + pgWord() + "." + pgPick(pgTLD)
	}
}

func pgDomainHost() string {
	if pgIntn(100) < 60 {
		return pgWord() + "." + pgWord() + "." + pgPick(pgTLD)
	}
	return pgWord() + "." + pgPick(pgTLD)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [3]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
