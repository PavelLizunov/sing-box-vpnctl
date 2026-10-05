//go:build !with_awg

package wireguard

import (
	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
)

func awgIpcLines(o option.AmneziaWGOptions) (string, error) {
	if o.IsSet() {
		return "", E.New("AmneziaWG (awg) support is not included in this build, rebuild with -tags with_awg")
	}
	return "", nil
}
