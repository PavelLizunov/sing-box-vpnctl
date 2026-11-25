//go:build with_turnrelay

package turnrelay

import (
	"strings"
	"testing"

	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/stretchr/testify/require"
)

func validOptions() Options {
	return Options{
		ServerOptions:     option.ServerOptions{Server: "127.0.0.1", ServerPort: 56004},
		TURNServer:        "127.0.0.1:3478",
		TURNUsername:      "turn-user",
		TURNPassword:      "turn-pass",
		Password:          "exit-pass",
		ServerFingerprint: strings.Repeat("ab", 32),
	}
}

func TestOptionsBuild(t *testing.T) {
	options := validOptions()
	cfg, err := options.build()
	require.NoError(t, err)
	require.Equal(t, 2, cfg.connections)
	require.True(t, cfg.udp)
	require.Equal(t, "127.0.0.1:3478", cfg.turnServer)
	require.Len(t, cfg.fingerprint, 32)

	options.Connections = 3
	options.Mode = "dtls"
	options.Provider = "static"
	options.ServerType = "exit"
	udp := false
	options.UDP = &udp
	cfg, err = options.build()
	require.NoError(t, err)
	require.Equal(t, 3, cfg.connections)
	require.False(t, cfg.udp)
	require.Equal(t, "dtls", cfg.mode)
}

func TestOptionsRejectUnsupported(t *testing.T) {
	cases := []struct {
		name    string
		change  func(*Options)
		wantErr string
	}{
		{"vk provider", func(o *Options) { o.Provider = "vk" }, "provider"},
		{"wireguard server", func(o *Options) { o.ServerType = "wireguard" }, "server_type"},
		{"wrap mode", func(o *Options) { o.Mode = "wrap" }, "mode"},
		{"missing turn server", func(o *Options) { o.TURNServer = "" }, "turn_server is required"},
		{"turn server without port", func(o *Options) { o.TURNServer = "turn.test" }, "host:port"},
		{"turn server zero port", func(o *Options) { o.TURNServer = "turn.test:0" }, "1..65535"},
		{"missing turn username", func(o *Options) { o.TURNUsername = "" }, "turn_username"},
		{"missing turn password", func(o *Options) { o.TURNPassword = "" }, "turn_username"},
		{"missing exit password", func(o *Options) { o.Password = "" }, "password is required"},
		{"missing server", func(o *Options) { o.Server = "" }, "literal IP"},
		{"domain server", func(o *Options) { o.Server = "exit.test" }, "literal IP"},
		{"unspecified server", func(o *Options) { o.Server = "0.0.0.0" }, "specific IP"},
		{"missing server port", func(o *Options) { o.ServerPort = 0 }, "server_port is required"},
		{"missing fingerprint", func(o *Options) { o.ServerFingerprint = "" }, "server_fingerprint"},
		{"short fingerprint", func(o *Options) { o.ServerFingerprint = strings.Repeat("ab", 31) }, "server_fingerprint"},
		{"non hex fingerprint", func(o *Options) { o.ServerFingerprint = strings.Repeat("zz", 32) }, "server_fingerprint"},
		{"too many connections", func(o *Options) { o.Connections = 61 }, "connections"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			options := validOptions()
			testCase.change(&options)
			_, err := options.build()
			require.Error(t, err)
			require.Contains(t, err.Error(), testCase.wantErr)
		})
	}
}

func TestNewOutboundRequiresStart(t *testing.T) {
	created, err := NewOutbound(t.Context(), nil, log.NewNOPFactory().Logger(), "relay", validOptions())
	require.NoError(t, err)
	out := created.(*Outbound)
	t.Cleanup(func() { _ = out.Close() })
	conn, err := out.DialContext(t.Context(), "tcp", M.ParseSocksaddr("127.0.0.1:1"))
	require.Error(t, err)
	require.Nil(t, conn)
	require.Contains(t, err.Error(), "not started")
}
