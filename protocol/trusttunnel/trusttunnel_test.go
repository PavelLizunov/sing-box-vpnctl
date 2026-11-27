//go:build with_trusttunnel

package trusttunnel

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/tls"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/auth"
	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	"github.com/stretchr/testify/require"
)

type echoRouter struct {
	adapter.Router
	tcpMetadata chan adapter.InboundContext
	udpMetadata chan adapter.InboundContext
}

func (r *echoRouter) RouteConnectionEx(_ context.Context, conn net.Conn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	r.tcpMetadata <- metadata
	go func() {
		_, err := io.Copy(conn, conn)
		_ = conn.Close()
		onClose(err)
	}()
}

func (r *echoRouter) RoutePacketConnectionEx(_ context.Context, conn N.PacketConn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	r.udpMetadata <- metadata
	go func() {
		for {
			packet := buf.NewPacket()
			destination, err := conn.ReadPacket(packet)
			if err != nil {
				packet.Release()
				_ = conn.Close()
				onClose(err)
				return
			}
			if err := conn.WritePacket(packet, destination); err != nil {
				_ = conn.Close()
				onClose(err)
				return
			}
		}
	}()
}

func TestTrustTunnelLoopback(t *testing.T) {
	t.Run("HTTP2", func(t *testing.T) { testTrustTunnelLoopback(t, false) })
	t.Run("HTTP3", func(t *testing.T) {
		if !C.WithQUIC {
			t.Skip("requires with_quic")
		}
		testTrustTunnelLoopback(t, true)
	})
}

func testTrustTunnelLoopback(t *testing.T, quic bool) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	logger := log.NewNOPFactory().Logger()
	const serverName = "trusttunnel.test"
	key, cert, err := tls.GenerateCertificate(nil, nil, time.Now, serverName, time.Now().Add(time.Hour))
	require.NoError(t, err)

	router := &echoRouter{
		tcpMetadata: make(chan adapter.InboundContext, 8),
		udpMetadata: make(chan adapter.InboundContext, 8),
	}
	user := auth.User{Username: "test-user", Password: "test-password"}
	in, err := NewInbound(ctx, router, logger, "tunnel-in", option.TrustTunnelInboundOptions{
		Users: []auth.User{user},
		InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{TLS: &option.InboundTLSOptions{
			Enabled: true, Certificate: []string{string(cert)}, Key: []string{string(key)},
		}},
		TrustTunnelQUICOptions: option.TrustTunnelQUICOptions{QUIC: quic},
	})
	require.NoError(t, err)
	inboundScope := adapter.NewScope(ctx, logger)
	t.Cleanup(func() { require.NoError(t, inboundScope.Close()) })
	require.NoError(t, in.Start(adapter.StartStateStart, inboundScope))
	server := in.(*Inbound).tcpAddress
	options := option.TrustTunnelOutboundOptions{
		ServerOptions: option.ServerOptions{Server: server.Addr.String(), ServerPort: server.Port},
		OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{TLS: &option.OutboundTLSOptions{
			Enabled: true, ServerName: serverName, Certificate: []string{string(cert)},
		}},
		Username: user.Username, Password: user.Password, HealthCheck: true,
		TrustTunnelQUICOptions: option.TrustTunnelQUICOptions{QUIC: quic},
	}
	tcpTarget := M.ParseSocksaddr("target.test:443")
	udpTarget := M.ParseSocksaddr("127.0.0.1:53")
	out, err := NewOutbound(ctx, nil, logger, "tunnel-out", options)
	require.NoError(t, err)
	client := out.(*Outbound)
	outboundScope := adapter.NewScope(ctx, logger)
	t.Cleanup(func() { require.NoError(t, outboundScope.Close()) })
	require.NoError(t, client.Start(adapter.StartStateStart, outboundScope))

	tcp, err := client.DialContext(ctx, N.NetworkTCP, tcpTarget)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tcp.Close() })
	tcpEcho(t, tcp)
	checkMetadata(t, ctx, router.tcpMetadata, user.Username, tcpTarget)

	udp, err := client.ListenPacket(ctx, udpTarget)
	require.NoError(t, err)
	t.Cleanup(func() { _ = udp.Close() })
	udpEcho(t, udp, udpTarget)
	checkMetadata(t, ctx, router.udpMetadata, user.Username, udpTarget)

	client.InterfaceUpdated(ctx)
	requireStreamClosed(t, ctx, tcp)
	_, _, err = udp.ReadFrom(make([]byte, 64))
	require.Error(t, err)

	reconnected, err := client.DialContext(ctx, N.NetworkTCP, tcpTarget)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reconnected.Close() })
	tcpEcho(t, reconnected)

	connectedUDP, err := client.DialContext(ctx, N.NetworkUDP, udpTarget)
	require.NoError(t, err)
	t.Cleanup(func() { _ = connectedUDP.Close() })
	tcpEcho(t, connectedUDP)

	require.NoError(t, client.Close())
	require.NoError(t, client.Close())
	requireStreamClosed(t, ctx, reconnected)
	_, err = client.DialContext(ctx, N.NetworkTCP, tcpTarget)
	require.ErrorIs(t, err, net.ErrClosed)

	options.HealthCheck = false
	for _, credentials := range []auth.User{
		{Username: "wrong-user", Password: user.Password},
		{Username: user.Username, Password: "wrong-password"},
	} {
		options.Username, options.Password = credentials.Username, credentials.Password
		rejected, err := NewOutbound(ctx, nil, logger, "rejected", options)
		require.NoError(t, err)
		t.Cleanup(func() { _ = rejected.(*Outbound).Close() })
		_, err = rejected.DialContext(ctx, N.NetworkTCP, tcpTarget)
		require.ErrorContains(t, err, "HTTP 403")
		_, err = rejected.ListenPacket(ctx, udpTarget)
		require.ErrorContains(t, err, "HTTP 403")
	}
	options.Username, options.Password = user.Username, user.Password
	lastClient, err := NewOutbound(ctx, nil, logger, "last", options)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lastClient.(*Outbound).Close() })
	active, err := lastClient.DialContext(ctx, N.NetworkTCP, tcpTarget)
	require.NoError(t, err)
	t.Cleanup(func() { _ = active.Close() })
	tcpEcho(t, active)
	require.NoError(t, inboundScope.Close())
	requireStreamClosed(t, ctx, active)
}

func tcpEcho(t *testing.T, conn net.Conn) {
	t.Helper()
	const message = "trusttunnel echo"
	_, err := conn.Write([]byte(message))
	require.NoError(t, err)
	response := make([]byte, len(message))
	_, err = io.ReadFull(conn, response)
	require.NoError(t, err)
	require.Equal(t, message, string(response))
}

func udpEcho(t *testing.T, conn net.PacketConn, target M.Socksaddr) {
	t.Helper()
	const message = "trusttunnel datagram"
	_, err := conn.WriteTo([]byte(message), target)
	require.NoError(t, err)
	response := make([]byte, 128)
	n, source, err := conn.ReadFrom(response)
	require.NoError(t, err)
	require.Equal(t, target, M.SocksaddrFromNet(source))
	require.Equal(t, message, string(response[:n]))
}

func checkMetadata(t *testing.T, ctx context.Context, metadata <-chan adapter.InboundContext, user string, target M.Socksaddr) {
	t.Helper()
	select {
	case got := <-metadata:
		require.Equal(t, C.TypeTrustTunnel, got.InboundType)
		require.Equal(t, "tunnel-in", got.Inbound)
		require.Equal(t, user, got.User)
		require.Equal(t, target, got.Destination)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func requireStreamClosed(t *testing.T, ctx context.Context, conn net.Conn) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		_, err := conn.Read(make([]byte, 1))
		done <- err
	}()
	select {
	case err := <-done:
		require.Error(t, err)
	case <-ctx.Done():
		t.Fatal("stream did not close")
	}
}

func TestTrustTunnelOutboundRejectsMissingTLS(t *testing.T) {
	_, err := NewOutbound(t.Context(), nil, log.NewNOPFactory().Logger(), "test", option.TrustTunnelOutboundOptions{
		Username: "test", Password: "test",
	})
	require.ErrorContains(t, err, "TLS is required")
}
