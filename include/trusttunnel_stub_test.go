//go:build !with_trusttunnel

package include

import (
	"testing"

	"github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json"

	"github.com/stretchr/testify/require"
)

func TestTrustTunnelDisabledConfig(t *testing.T) {
	for name, config := range map[string]string{
		"inbound":  `{"inbounds":[{"type":"trusttunnel","tag":"tt-in","users":[{"username":"user","password":"secret"}],"quic":true}]}`,
		"outbound": `{"outbounds":[{"type":"trusttunnel","tag":"tt-out","server":"127.0.0.1","server_port":443,"username":"user","password":"secret","tls":{"enabled":true}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			ctx := Context(t.Context())
			options, err := json.UnmarshalExtendedContext[option.Options](ctx, []byte(config))
			require.NoError(t, err)
			_, err = box.New(box.Options{Context: ctx, Options: options})
			require.ErrorContains(t, err, errTrustTunnelNotIncluded.Error())
		})
	}
}
