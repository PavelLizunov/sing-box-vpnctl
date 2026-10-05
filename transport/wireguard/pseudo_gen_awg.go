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

// Protocol: SIP dialog tokens stay identical in the INVITE and its 100 Trying response, so they are generated once.
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

func pgDomainHost() string {
	if pgIntn(100) < 60 {
		return pgWord() + "." + pgWord() + "." + pgPick(pgTLD)
	}
	return pgWord() + "." + pgPick(pgTLD)
}
