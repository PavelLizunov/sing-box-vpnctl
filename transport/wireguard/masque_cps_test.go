//go:build with_awg

package wireguard

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	testChars52  = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	testDigits10 = "0123456789"
)

type cpsTag struct {
	kind string
	data []byte
	n    int
}

type testObfChain struct {
	tags []cpsTag
}

func parseCPS(spec string) (*testObfChain, error) {
	var (
		tags []cpsTag
		errs []error
	)
	remaining := spec
	for {
		start := strings.IndexByte(remaining, '<')
		if start == -1 {
			break
		}
		end := strings.IndexByte(remaining[start:], '>')
		if end == -1 {
			return nil, errors.New("missing enclosing >")
		}
		end += start
		tag := remaining[start+1 : end]
		remaining = remaining[end+1:]

		parts := strings.Fields(tag)
		if len(parts) == 0 {
			errs = append(errs, errors.New("empty tag"))
			continue
		}
		key := parts[0]
		val := ""
		if len(parts) > 1 {
			val = parts[1]
		}
		switch key {
		case "b":
			v := strings.TrimPrefix(val, "0x")
			if len(v) == 0 {
				errs = append(errs, errors.New("empty <b> argument"))
				continue
			}
			if len(v)%2 != 0 {
				errs = append(errs, errors.New("odd <b> hex length"))
				continue
			}
			data, err := hex.DecodeString(v)
			if err != nil {
				errs = append(errs, fmt.Errorf("bad <b> hex: %w", err))
				continue
			}
			tags = append(tags, cpsTag{kind: "b", data: data})
		case "r", "rc", "rd":
			n, err := strconv.Atoi(val)
			if err != nil {
				errs = append(errs, fmt.Errorf("bad <%s> count: %w", key, err))
				continue
			}
			tags = append(tags, cpsTag{kind: key, n: n})
		default:
			errs = append(errs, fmt.Errorf("unknown tag <%s>", key))
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return &testObfChain{tags: tags}, nil
}

func (c *testObfChain) obfuscate() []byte {
	var out []byte
	for _, tag := range c.tags {
		switch tag.kind {
		case "b":
			out = append(out, tag.data...)
		case "r":
			buf := make([]byte, tag.n)
			rand.Read(buf)
			out = append(out, buf...)
		case "rc":
			buf := make([]byte, tag.n)
			rand.Read(buf)
			for i := range buf {
				buf[i] = testChars52[buf[i]%52]
			}
			out = append(out, buf...)
		case "rd":
			buf := make([]byte, tag.n)
			rand.Read(buf)
			for i := range buf {
				buf[i] = testDigits10[buf[i]%10]
			}
			out = append(out, buf...)
		}
	}
	return out
}
