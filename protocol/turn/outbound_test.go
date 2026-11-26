//go:build with_turn

package turn

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
	pion "github.com/pion/turn/v5"
	"github.com/sagernet/sing-box/adapter"
	boxTLS "github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/stretchr/testify/require"
)

func testTLSListener(t testing.TB) (net.Listener, Options) {
	t.Helper()
	const name = "relay.test"
	key, cert, err := boxTLS.GenerateCertificate(nil, nil, time.Now, name, time.Now().Add(time.Hour))
	require.NoError(t, err)
	identity, err := tls.X509KeyPair(cert, key)
	require.NoError(t, err)
	listener, err := tls.Listen("tcp4", "127.0.0.1:0", &tls.Config{
		MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{identity},
	})
	require.NoError(t, err)
	options := Options{
		ServerOptions: option.ServerOptions{Server: "127.0.0.1", ServerPort: M.SocksaddrFromNet(listener.Addr()).Port},
		OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{TLS: &option.OutboundTLSOptions{
			Enabled: true, ServerName: name, Certificate: []string{string(cert)},
		}},
		Username: "test-client", Password: "local-test-password",
	}
	return listener, options
}

type testRelay struct {
	options     Options
	server      *pion.Server
	allocations <-chan net.Addr
	connections <-chan net.Conn
	channels    <-chan struct{}
}

type relayListener struct {
	net.Listener
	connections chan net.Conn
}

func (l *relayListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err == nil {
		l.connections <- conn
	}
	return conn, err
}

func startTestRelay(t testing.TB, relayIP string) testRelay {
	t.Helper()
	return relayWithPermission(t, relayIP, func(_ net.Addr, peer net.IP) bool { return peer.IsLoopback() })
}

func relayWithPermission(t testing.TB, relayIP string, permission pion.PermissionHandler) testRelay {
	t.Helper()
	listener, options := testTLSListener(t)
	const realm = "local.test"
	key := pion.GenerateAuthKey(options.Username, realm, options.Password)
	allocations := make(chan net.Addr, 8)
	connections := make(chan net.Conn, 8)
	channels := make(chan struct{}, 8)
	server, err := pion.NewServer(pion.ServerConfig{
		Realm: realm,
		AuthHandler: func(request *pion.RequestAttributes) (string, []byte, bool) {
			return options.Username, key, request.Username == options.Username
		},
		EventHandler: pion.EventHandler{
			OnAllocationCreated: func(_, _ net.Addr, _, _, _ string, addr net.Addr, _ int) {
				allocations <- addr
			},
			OnChannelCreated: func(_, _ net.Addr, _, _, _ string, _, _ net.Addr, _ uint16) {
				channels <- struct{}{}
			},
		},
		ListenerConfigs: []pion.ListenerConfig{{
			Listener:          &relayListener{Listener: listener, connections: connections},
			PermissionHandler: permission,
			RelayAddressGenerator: &pion.RelayAddressGeneratorStatic{
				RelayAddress: net.ParseIP(relayIP), Address: relayIP,
			},
		}},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		server.Close()
		for {
			select {
			case conn := <-connections:
				conn.Close()
			default:
				return
			}
		}
	})
	return testRelay{options: options, server: server, allocations: allocations, connections: connections, channels: channels}
}

func testOutbound(t testing.TB, options Options) adapter.Outbound {
	t.Helper()
	out, err := NewOutbound(t.Context(), nil, log.NewNOPFactory().Logger(), "relay", options)
	require.NoError(t, err)
	t.Cleanup(func() { out.(io.Closer).Close() })
	return out
}

func TestRelayedDNSAndReconnect(t *testing.T) {
	relay := startTestRelay(t, "127.0.0.1")
	out := testOutbound(t, relay.options)
	require.Equal(t, "turn", out.Type())
	require.Equal(t, []string{"udp"}, out.Network())

	peer, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer peer.Close()
	destination := M.SocksaddrFromNet(peer.LocalAddr())
	query := new(dns.Msg).SetQuestion("name.test.", dns.TypeA)
	wireQuery, err := query.Pack()
	require.NoError(t, err)
	for i := range 3 {
		conn, err := out.DialContext(t.Context(), "udp", destination)
		require.NoError(t, err)
		require.NoError(t, conn.SetDeadline(time.Now().Add(3*time.Second)))
		require.NoError(t, peer.SetDeadline(time.Now().Add(3*time.Second)))
		_, err = conn.Write(wireQuery)
		require.NoError(t, err)
		var data [1500]byte
		n, source, err := peer.ReadFrom(data[:])
		require.NoError(t, err)
		require.Equal(t, wireQuery, data[:n])
		select {
		case relayAddr := <-relay.allocations:
			require.Equal(t, relayAddr.String(), source.String())
		case <-time.After(3 * time.Second):
			t.Fatal("server did not report an allocation")
		}
		reply := new(dns.Msg).SetReply(query)
		reply.Answer = []dns.RR{&dns.A{
			Hdr: dns.RR_Header{Name: "name.test.", Rrtype: dns.TypeA, Class: dns.ClassINET}, A: net.IPv4(192, 0, 2, 1),
		}}
		wireReply, err := reply.Pack()
		require.NoError(t, err)
		_, err = peer.WriteTo(wireReply, source)
		require.NoError(t, err)
		n, err = conn.Read(data[:])
		require.NoError(t, err)
		require.Equal(t, wireReply, data[:n])
		wire := <-relay.connections
		switch i {
		case 0:
			out.(adapter.InterfaceUpdateListener).InterfaceUpdated(t.Context())
		case 1:
			require.NoError(t, wire.Close())
		}
		if i < 2 {
			_, err = conn.Read(data[:])
			require.Error(t, err)
			var timeout net.Error
			if errors.As(err, &timeout) {
				require.False(t, timeout.Timeout(), "reset must wake the reader")
			}
		}
		conn.Close()
		require.Eventually(t, func() bool { return relay.server.AllocationCount() == 0 }, 3*time.Second, 10*time.Millisecond)
	}
	require.NoError(t, out.(io.Closer).Close())
	out.(adapter.InterfaceUpdateListener).InterfaceUpdated(t.Context())
	_, err = out.DialContext(t.Context(), "udp", destination)
	require.Error(t, err)
	require.Zero(t, relay.server.AllocationCount())
}

func TestPacketDestinations(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "::1"} {
		t.Run(ip, func(t *testing.T) {
			peer, err := net.ListenPacket("udp", net.JoinHostPort(ip, "0"))
			if err != nil && ip == "::1" {
				t.Skipf("IPv6 loopback is unavailable: %v", err)
			}
			require.NoError(t, err)
			defer peer.Close()
			relay := startTestRelay(t, ip)
			out := testOutbound(t, relay.options)
			destination := M.SocksaddrFromNet(peer.LocalAddr())
			_, err = out.DialContext(t.Context(), "tcp", destination)
			require.Error(t, err)
			packet, err := out.ListenPacket(t.Context(), destination)
			require.NoError(t, err)
			defer packet.Close()
			require.NoError(t, packet.SetDeadline(time.Now().Add(3*time.Second)))
			require.NoError(t, peer.SetDeadline(time.Now().Add(3*time.Second)))
			payload := []byte("one datagram")
			oversize := 65508
			if destination.IsIPv6() {
				oversize = 65497
			}
			_, err = packet.WriteTo(make([]byte, oversize), destination)
			require.ErrorContains(t, err, "too large")
			_, err = packet.WriteTo(payload, destination)
			require.NoError(t, err)
			var data [1500]byte
			n, source, err := peer.ReadFrom(data[:])
			require.NoError(t, err)
			require.Equal(t, payload, data[:n])
			select {
			case <-relay.channels:
			case <-time.After(3 * time.Second):
				t.Fatal("TURN channel was not established")
			}
			for _, size := range []int{0, 1, 4, 5, len(payload)} {
				_, err = peer.WriteTo(payload[:size], source)
				require.NoError(t, err)
				n, from, err := packet.ReadFrom(data[:])
				require.NoError(t, err, "payload size %d", size)
				require.Equal(t, destination.String(), from.String())
				require.Equal(t, payload[:size], data[:n])
			}
			otherFamily := net.IPv6loopback
			if destination.IsIPv6() {
				otherFamily = net.IPv4(127, 0, 0, 1)
			}
			_, err = packet.WriteTo(payload, &net.UDPAddr{IP: otherFamily, Port: 53})
			require.Error(t, err)
			var lookups atomic.Int32
			savedResolver := net.DefaultResolver
			net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				lookups.Add(1)
				return nil, errors.New("unexpected DNS query")
			}}
			defer func() { net.DefaultResolver = savedResolver }()
			domain := M.ParseSocksaddr("must-not-resolve.invalid:53")
			_, err = packet.WriteTo(payload, domain)
			require.Error(t, err)
			_, err = out.DialContext(t.Context(), "udp", domain)
			require.Error(t, err)
			require.Zero(t, lookups.Load())
			past := time.Now().Add(-time.Second)
			require.NoError(t, packet.SetDeadline(past))
			_, _, err = packet.ReadFrom(data[:])
			var timeout net.Error
			require.ErrorAs(t, err, &timeout)
			require.True(t, timeout.Timeout())
			repeated := make(chan error, 1)
			go func() {
				_, _, err := packet.ReadFrom(make([]byte, 32))
				repeated <- err
			}()
			select {
			case err = <-repeated:
				require.ErrorAs(t, err, &timeout)
				require.True(t, timeout.Timeout())
			case <-time.After(300 * time.Millisecond):
				t.Fatal("second read blocked past the expired deadline")
			}
			require.NoError(t, packet.SetReadDeadline(time.Now().Add(3*time.Second)))
			_, err = peer.WriteTo(payload, source)
			require.NoError(t, err)
			n, _, err = packet.ReadFrom(data[:])
			require.NoError(t, err)
			require.Equal(t, payload, data[:n])
			_, err = packet.WriteTo(payload, destination)
			require.ErrorAs(t, err, &timeout)
			require.True(t, timeout.Timeout())
		})
	}
}

func TestDeadlineDuringPermissionRequest(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	relay := relayWithPermission(t, "127.0.0.1", func(_ net.Addr, peer net.IP) bool {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		return peer.IsLoopback()
	})
	out := testOutbound(t, relay.options)
	packet, err := out.ListenPacket(t.Context(), M.ParseSocksaddr("127.0.0.1:53"))
	require.NoError(t, err)
	defer packet.Close()
	defer close(release)
	done := make(chan error, 1)
	go func() {
		_, err := packet.WriteTo([]byte("deadline"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 53})
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("TURN server did not receive CreatePermission")
	}
	require.NoError(t, packet.SetWriteDeadline(time.Now().Add(10*time.Millisecond)))
	select {
	case err = <-done:
		var timeout net.Error
		require.ErrorAs(t, err, &timeout)
		require.True(t, timeout.Timeout())
	case <-time.After(300 * time.Millisecond):
		t.Fatal("write stayed blocked waiting for a permission response")
	}
}

func TestRejectInvalidRelayCredentials(t *testing.T) {
	relay := startTestRelay(t, "127.0.0.1")
	options := relay.options
	for _, name := range []string{"untrusted CA", "wrong server name", "wrong password"} {
		t.Run(name, func(t *testing.T) {
			invalid := options
			tlsOptions := *options.TLS
			invalid.TLS = &tlsOptions
			switch name {
			case "untrusted CA":
				invalid.TLS.Certificate = nil
			case "wrong server name":
				invalid.TLS.ServerName = "wrong.test"
			case "wrong password":
				invalid.Password = "incorrect"
			}
			out := testOutbound(t, invalid)
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			_, err := out.DialContext(ctx, "udp", M.ParseSocksaddr("127.0.0.1:53"))
			require.Error(t, err)
			require.NoError(t, ctx.Err(), "credential failure must be reported before the deadline")
			require.Zero(t, relay.server.AllocationCount())
		})
	}
	for _, name := range []string{"cleartext", "insecure", "missing username", "missing password"} {
		t.Run(name, func(t *testing.T) {
			invalid := options
			tlsOptions := *options.TLS
			invalid.TLS = &tlsOptions
			switch name {
			case "cleartext":
				invalid.TLS.Enabled = false
			case "insecure":
				invalid.TLS.Insecure = true
			case "missing username":
				invalid.Username = ""
			case "missing password":
				invalid.Password = ""
			}
			_, err := NewOutbound(t.Context(), nil, log.NewNOPFactory().Logger(), "relay", invalid)
			require.Error(t, err)
		})
	}
}

func TestCancelPendingAllocation(t *testing.T) {
	listener, options := testTLSListener(t)
	defer listener.Close()
	out := testOutbound(t, options)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := out.DialContext(ctx, "udp", M.ParseSocksaddr("127.0.0.1:53"))
		done <- err
	}()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, _ := listener.Accept()
		accepted <- conn
	}()
	var conn net.Conn
	select {
	case conn = <-accepted:
		require.NotNil(t, conn)
	case err := <-done:
		t.Fatalf("dial ended before requesting an allocation: %v", err)
	case <-ctx.Done():
		t.Fatal("timed out waiting for the TLS connection")
	}
	defer conn.Close()
	require.NoError(t, conn.SetDeadline(time.Now().Add(3*time.Second)))
	var buffer [2048]byte
	_, err := conn.Read(buffer[:])
	require.NoError(t, err)
	cancel()
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not unblock the allocation")
	}
	_, err = io.Copy(io.Discard, conn)
	var timeout net.Error
	if errors.As(err, &timeout) {
		require.False(t, timeout.Timeout(), "client should release its underlying TLS connection")
	}
}

func BenchmarkRelayedUDP(b *testing.B) {
	peer, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(b, err)
	defer peer.Close()
	go func() {
		var data [1500]byte
		for {
			n, source, err := peer.ReadFrom(data[:])
			if err != nil {
				return
			}
			if _, err := peer.WriteTo(data[:n], source); err != nil {
				return
			}
		}
	}()
	b.Run("direct", func(b *testing.B) {
		var dialer net.Dialer
		conn, err := dialer.DialContext(b.Context(), "udp4", peer.LocalAddr().String())
		require.NoError(b, err)
		defer conn.Close()
		benchmarkEcho(b, conn)
	})
	b.Run("turn", func(b *testing.B) {
		out := testOutbound(b, startTestRelay(b, "127.0.0.1").options)
		conn, err := out.DialContext(b.Context(), "udp", M.SocksaddrFromNet(peer.LocalAddr()))
		require.NoError(b, err)
		defer conn.Close()
		benchmarkEcho(b, conn)
	})
}

func benchmarkEcho(b *testing.B, conn net.Conn) {
	b.Helper()
	payload := bytes.Repeat([]byte{0xa5}, 1200)
	response := make([]byte, len(payload))
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	for b.Loop() {
		if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
			b.Fatal(err)
		}
		if _, err := conn.Write(payload); err != nil {
			b.Fatal(err)
		}
		n, err := conn.Read(response)
		if err != nil || !bytes.Equal(response[:n], payload) {
			b.Fatalf("echo received %d bytes, err=%v", n, err)
		}
	}
}
