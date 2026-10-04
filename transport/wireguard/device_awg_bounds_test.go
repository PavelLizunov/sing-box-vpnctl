//go:build with_awg

package wireguard

import (
	"strings"
	"testing"

	"github.com/sagernet/sing-box/option"
)

func TestAwgJunkBounds(t *testing.T) {
	for _, options := range []option.AmneziaWGOptions{
		{Jc: 129, Jmin: 40, Jmax: 70},
		{Jc: 4, Jmin: 40, Jmax: 65508},
		{Jc: 1, Jmin: 4000000000, Jmax: 4000000000},
	} {
		if lines, err := awgIpcLines(options); err == nil || lines != "" {
			t.Fatalf("accepted jc=%d jmin=%d jmax=%d", options.Jc, options.Jmin, options.Jmax)
		}
	}
	lines, err := awgIpcLines(option.AmneziaWGOptions{Jc: 128, Jmin: 1, Jmax: 65507})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"\njc=128", "\njmin=1", "\njmax=65507"} {
		if !strings.Contains(lines, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestAwgRekeyAfterTimeRange(t *testing.T) {
	for _, value := range []string{"0-4294967295", "120-60", "abc", "1-2-3", "-5", "4294967296"} {
		if lines, err := awgIpcLines(option.AmneziaWGOptions{RekeyAfterTime: value}); err == nil || lines != "" {
			t.Fatalf("accepted rekey_after_time %q", value)
		}
	}
	for _, value := range []string{"0", "45", "60-120", "1-4294967295"} {
		lines, err := awgIpcLines(option.AmneziaWGOptions{RekeyAfterTime: value})
		if err != nil {
			t.Fatalf("rejected rekey_after_time %q: %v", value, err)
		}
		if !strings.Contains(lines, "\nrekey_after_time="+value) {
			t.Fatalf("missing rekey_after_time=%s", value)
		}
	}
}
