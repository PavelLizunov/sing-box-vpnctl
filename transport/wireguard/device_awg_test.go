//go:build with_awg

package wireguard

import (
	"strings"
	"testing"

	"github.com/sagernet/sing-box/option"

	"github.com/stretchr/testify/require"
)

func TestAwgIpcLines(t *testing.T) {
	t.Parallel()
	lines, err := awgIpcLines(option.AmneziaWGOptions{
		Jc:   5,
		Jmin: 10,
		Jmax: 50,
		S1:   28,
		S2:   121,
		S3:   25,
		S4:   9,
		H1:   "43613244-384550127",
		H2:   "826869626-2105069164",
		H3:   "2124774725-2141151992",
		H4:   "2144594503-2146278491",
		I1:   "<b 0x0844>",
	})
	require.NoError(t, err)
	require.Equal(t,
		"\njc=5\njmin=10\njmax=50\ns1=28\ns2=121\ns3=25\ns4=9"+
			"\nh1=43613244-384550127\nh2=826869626-2105069164"+
			"\nh3=2124774725-2141151992\nh4=2144594503-2146278491"+
			"\ni1=<b 0x0844>",
		lines)
}

func TestAwgIpcLinesSingleHeaders(t *testing.T) {
	t.Parallel()
	lines, err := awgIpcLines(option.AmneziaWGOptions{
		H1: "1",
		H2: "2",
		H3: "3",
		H4: "4",
	})
	require.NoError(t, err)
	require.Equal(t, "\nh1=1\nh2=2\nh3=3\nh4=4", lines)
}

func TestAwgIpcLinesUnsetHeadersOmitted(t *testing.T) {
	t.Parallel()
	lines, err := awgIpcLines(option.AmneziaWGOptions{
		Jc: 4,
		H2: "10-20",
	})
	require.NoError(t, err)
	require.Equal(t, "\njc=4\nh2=10-20", lines)
}

func TestAwgIpcLinesPlainWireGuard(t *testing.T) {
	t.Parallel()
	lines, err := awgIpcLines(option.AmneziaWGOptions{})
	require.NoError(t, err)
	require.Equal(t, "", lines)
}

func TestAwgIpcLinesInvalidHeader(t *testing.T) {
	t.Parallel()
	_, err := awgIpcLines(option.AmneziaWGOptions{H3: "100-50"})
	require.Error(t, err)
	require.ErrorContains(t, err, "h3")
}

func TestAwgIpcLinesJminGreaterThanJmax(t *testing.T) {
	t.Parallel()
	require.NotPanics(t, func() {
		_, err := awgIpcLines(option.AmneziaWGOptions{Jc: 5, Jmin: 70, Jmax: 40})
		require.Error(t, err)
		require.ErrorContains(t, err, "jmin")
		require.ErrorContains(t, err, "jmax")
	})
}

func TestAwgIpcLinesValidJunkRange(t *testing.T) {
	t.Parallel()
	_, err := awgIpcLines(option.AmneziaWGOptions{Jc: 4, Jmin: 40, Jmax: 70})
	require.NoError(t, err)
	_, err = awgIpcLines(option.AmneziaWGOptions{H1: "1"})
	require.NoError(t, err)
}

func TestAwgIpcLinesQUICSingleInitial(t *testing.T) {
	t.Parallel()
	const sni = "www.google.com"
	lines, err := awgIpcLines(option.AmneziaWGOptions{Id: sni, Ip: "quic", Ib: "chrome"})
	require.NoError(t, err)

	i1 := ipcValue(t, lines, "i1")
	require.NotEmpty(t, i1, "i1 present")
	require.Empty(t, ipcValue(t, lines, "i2"), "quic is single-packet: i2 empty")

	pkt := obfuscateCPS(t, i1)
	require.Equal(t, 1250, len(pkt), "i1 is a 1250B Initial")
	d := decryptInitial(t, pkt)
	require.Equal(t, sni, extractSNI(t, d.clientHello), "i1 carries the SNI")
	require.NotEqual(t, uint64(0), d.cryptoFrames[0].offset, "first CRYPTO offset != 0 (I1)")
}

func TestAwgIpcLinesNonSIPNoI2(t *testing.T) {
	t.Parallel()
	for _, o := range []option.AmneziaWGOptions{
		{Id: "a.com", Ip: "dns"},
		{Ip: "stun"},
		{Id: "a.com", Ip: "quic"},
	} {
		lines, err := awgIpcLines(o)
		require.NoError(t, err)
		require.NotEmpty(t, ipcValue(t, lines, "i1"), "i1 present for ip=%s", o.Ip)
		require.Empty(t, ipcValue(t, lines, "i2"), "i2 empty for ip=%s", o.Ip)
	}
}

func TestAwgIpcLinesSIPFillsI1AndI2(t *testing.T) {
	t.Parallel()
	const host = "pbx.example.com"
	lines, err := awgIpcLines(option.AmneziaWGOptions{Id: host, Ip: "sip"})
	require.NoError(t, err)

	i1 := ipcValue(t, lines, "i1")
	i2 := ipcValue(t, lines, "i2")
	require.NotEmpty(t, i1, "i1 (INVITE) present")
	require.NotEmpty(t, i2, "i2 (100 Trying) present")

	invite := string(obfuscateCPS(t, i1))
	trying := string(obfuscateCPS(t, i2))
	assertSIPInvite(t, invite, host)
	assertSIPTrying(t, trying)
	assertSameSIPDialog(t, invite, trying)
}

func TestAwgIpcLinesSIPExplicitI2Conflict(t *testing.T) {
	t.Parallel()
	_, err := awgIpcLines(option.AmneziaWGOptions{Id: "a.com", Ip: "sip", I2: "<b 0x0844>"})
	require.Error(t, err, "ip=sip + explicit i2 must conflict")
	require.Contains(t, err.Error(), "explicit i2 conflicts")
}

func TestAwg31IpcLines(t *testing.T) {
	t.Parallel()
	bTrue := true
	lines, err := awgIpcLines(option.AmneziaWGOptions{
		S1:                     20,
		S2:                     20,
		S3:                     20,
		S4:                     20,
		RandomTrailers:         &bTrue,
		DisableCookie:          &bTrue,
		HeaderProtectionKey:    "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		ContentPaddingAddition: "10-50",
		RekeyAfterTime:         "60-120",
	})
	require.NoError(t, err)
	require.Equal(t, "true", ipcValue(t, lines, "random_trailers"))
	require.Equal(t, "true", ipcValue(t, lines, "disable_cookies"))
	require.Equal(t, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", ipcValue(t, lines, "header_protection_key"))
	require.Equal(t, "10-50", ipcValue(t, lines, "content_padding_addition"))
	require.Equal(t, "60-120", ipcValue(t, lines, "rekey_after_time"))
}

func TestAwg31HeaderProtectionValidation(t *testing.T) {
	t.Parallel()
	_, err := awgIpcLines(option.AmneziaWGOptions{
		HeaderProtectionKey: "abcd",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "header_protection_key must be a valid 64-character hex string")

	_, err = awgIpcLines(option.AmneziaWGOptions{
		S1:                  10,
		HeaderProtectionKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "s1..s4 must each be >= 12 bytes")
}

func ipcValue(t *testing.T, lines, key string) string {
	t.Helper()
	for _, line := range strings.Split(lines, "\n") {
		if v, ok := strings.CutPrefix(line, key+"="); ok {
			return v
		}
	}
	return ""
}
