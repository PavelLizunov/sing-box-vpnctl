//go:build with_turnrelay

package include

import (
	"context"
	"strings"
	"testing"

	"github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/turnrelay"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/stretchr/testify/require"
)

func TestTurnRelayOutboundRegistered(t *testing.T) {
	registry := OutboundRegistry()
	options, loaded := registry.CreateOptions("turnrelay")
	require.True(t, loaded)
	require.NotNil(t, options)
}

func TestTurnRelayBoxStarts(t *testing.T) {
	outboundOptions := &turnrelay.Options{
		ServerOptions:     option.ServerOptions{Server: "127.0.0.1", ServerPort: 56004},
		Provider:          "static",
		ServerType:        "exit",
		TURNServer:        "127.0.0.1:1",
		TURNUsername:      "turn-user",
		TURNPassword:      "turn-password",
		Password:          "exit-password",
		ServerFingerprint: strings.Repeat("ab", 32),
		Connections:       1,
		Mode:              "srtp",
	}
	instance, err := box.New(box.Options{
		Context: Context(context.Background()),
		Options: option.Options{
			Log:       &option.LogOptions{Level: "warn"},
			Outbounds: []option.Outbound{{Type: "turnrelay", Tag: "relay", Options: outboundOptions}},
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = instance.Close() })
	require.NoError(t, instance.Start())
	outbound, loaded := instance.Outbound().Outbound("relay")
	require.True(t, loaded)
	conn, err := outbound.DialContext(context.Background(), "tcp", M.ParseSocksaddr("127.0.0.1:9"))
	require.NoError(t, err)
	require.NotNil(t, conn)
	_ = conn.Close()
}
