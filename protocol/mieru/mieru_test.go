//go:build with_mieru

package mieru

import (
	"context"
	"io"
	"net"
	"net/netip"
	"runtime"
	"sync"
	"testing"
	"time"

	mierupb "github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/json/badoption"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/stretchr/testify/require"
)

const (
	testUserName = "mieru-user"
	testPassword = "mieru-loopback-password"
)

type testRouter struct {
	adapter.Router
}

func (r *testRouter) RouteConnectionEx(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	go func() {
		defer func() {
			if onClose != nil {
				onClose(nil)
			}
		}()
		remote, err := net.DialTimeout(N.NetworkTCP, metadata.Destination.String(), 3*time.Second)
		if err != nil {
			_ = conn.Close()
			return
		}
		defer remote.Close()
		var waitGroup sync.WaitGroup
		waitGroup.Add(2)
		go func() {
			defer waitGroup.Done()
			_, _ = io.Copy(remote, conn)
			_ = remote.Close()
		}()
		go func() {
			defer waitGroup.Done()
			_, _ = io.Copy(conn, remote)
			_ = conn.Close()
		}()
		waitGroup.Wait()
		_ = conn.Close()
	}()
}

func (r *testRouter) RoutePacketConnectionEx(_ context.Context, conn N.PacketConn, _ adapter.InboundContext, onClose N.CloseHandlerFunc) {
	go func() {
		defer func() {
			_ = conn.Close()
			if onClose != nil {
				onClose(nil)
			}
		}()
		packet := buf.New()
		defer packet.Release()
		for {
			packet.Reset()
			destination, err := conn.ReadPacket(packet)
			if err != nil {
				return
			}
			remote, err := net.DialTimeout(N.NetworkUDP, destination.String(), 3*time.Second)
			if err != nil {
				return
			}
			_ = remote.SetDeadline(time.Now().Add(3 * time.Second))
			if _, err = remote.Write(packet.Bytes()); err != nil {
				_ = remote.Close()
				return
			}
			response := buf.NewSize(64 * 1024)
			n, err := remote.Read(response.FreeBytes())
			_ = remote.Close()
			if err != nil {
				response.Release()
				return
			}
			response.Truncate(n)
			err = conn.WritePacket(response, destination)
			response.Release()
			if err != nil {
				return
			}
		}
	}()
}

func freePort(t *testing.T) uint16 {
	t.Helper()
	for attempt := 0; attempt < 10; attempt++ {
		tcpListener, err := net.Listen("tcp4", "127.0.0.1:0")
		require.NoError(t, err)
		port := tcpListener.Addr().(*net.TCPAddr).Port
		udpConn, udpErr := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
		if udpErr == nil {
			require.NoError(t, udpConn.Close())
			require.NoError(t, tcpListener.Close())
			return uint16(port)
		}
		require.NoError(t, tcpListener.Close())
	}
	t.Fatal("no free TCP and UDP port pair")
	return 0
}

func startTCPEcho(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = listener.Close()
	})
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()
	return listener
}

func startUDPEcho(t *testing.T) *net.UDPConn {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = conn.Close()
	})
	go func() {
		buffer := make([]byte, 64*1024)
		for {
			n, from, err := conn.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			_, _ = conn.WriteToUDP(buffer[:n], from)
		}
	}()
	return conn
}

func startTestInbound(t *testing.T, users []option.MieruUser, bindings []option.MieruPortBinding) *Inbound {
	t.Helper()
	listenAddress := badoption.Addr(netip.MustParseAddr("127.0.0.1"))
	logger := log.NewNOPFactory().Logger()
	created, err := NewInbound(t.Context(), &testRouter{}, logger, "mieru-in", option.MieruInboundOptions{
		ListenOptions: option.ListenOptions{Listen: &listenAddress},
		PortBindings:  bindings,
		Users:         users,
	})
	require.NoError(t, err)
	instance := created.(*Inbound)
	scope := adapter.NewScope(t.Context(), logger)
	require.NoError(t, instance.Start(adapter.StartStateInitialize, scope))
	require.NoError(t, instance.Start(adapter.StartStateStart, scope))
	t.Cleanup(func() {
		require.NoError(t, instance.Close())
	})
	return instance
}

func startTestOutbound(t *testing.T, port uint16, transport, handshakeMode, userName, password string) *Outbound {
	t.Helper()
	logger := log.NewNOPFactory().Logger()
	created, err := NewOutbound(t.Context(), nil, logger, "mieru-out", option.MieruOutboundOptions{
		ServerOptions: option.ServerOptions{Server: "127.0.0.1", ServerPort: port},
		Transport:     transport,
		UserName:      userName,
		Password:      password,
		Multiplexing:  "off",
		HandshakeMode: handshakeMode,
	})
	require.NoError(t, err)
	instance := created.(*Outbound)
	scope := adapter.NewScope(t.Context(), logger)
	require.NoError(t, instance.Start(adapter.StartStateInitialize, scope))
	require.NoError(t, instance.Start(adapter.StartStateStart, scope))
	t.Cleanup(func() {
		require.NoError(t, instance.Close())
	})
	return instance
}

func TestTCPInterop(t *testing.T) {
	echo := startTCPEcho(t)
	port := freePort(t)
	startTestInbound(t, []option.MieruUser{{UserName: testUserName, Password: testPassword}}, []option.MieruPortBinding{{Port: port, Transport: "tcp"}})
	outbound := startTestOutbound(t, port, "tcp", "HANDSHAKE_STANDARD", testUserName, testPassword)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, err := outbound.DialContext(ctx, N.NetworkTCP, M.SocksaddrFromNet(echo.Addr()))
	require.NoError(t, err)
	defer conn.Close()

	payload := []byte("mieru tcp loopback interop")
	_, err = conn.Write(payload)
	require.NoError(t, err)
	response := make([]byte, len(payload))
	_, err = io.ReadFull(conn, response)
	require.NoError(t, err)
	require.Equal(t, payload, response)
}

func TestUDPInterop(t *testing.T) {
	echo := startUDPEcho(t)
	port := freePort(t)
	startTestInbound(t, []option.MieruUser{{UserName: testUserName, Password: testPassword}}, []option.MieruPortBinding{{Port: port, Transport: "udp"}})
	outbound := startTestOutbound(t, port, "udp", "HANDSHAKE_STANDARD", testUserName, testPassword)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	packetConn, err := outbound.ListenPacket(ctx, M.SocksaddrFromNet(echo.LocalAddr()))
	require.NoError(t, err)
	defer packetConn.Close()
	require.NoError(t, packetConn.SetDeadline(time.Now().Add(5*time.Second)))

	payload := []byte("mieru udp loopback interop")
	_, err = packetConn.WriteTo(payload, echo.LocalAddr())
	require.NoError(t, err)
	response := make([]byte, 64*1024)
	n, _, err := packetConn.ReadFrom(response)
	require.NoError(t, err)
	require.Equal(t, payload, response[:n])
}

func TestRejectsInvalidCredentials(t *testing.T) {
	echo := startTCPEcho(t)
	port := freePort(t)
	startTestInbound(t, []option.MieruUser{{UserName: testUserName, Password: testPassword}}, []option.MieruPortBinding{{Port: port, Transport: "tcp"}})

	testCases := []struct {
		name     string
		userName string
		password string
	}{
		{name: "wrong password", userName: testUserName, password: "wrong-password"},
		{name: "wrong username", userName: "wrong-user", password: testPassword},
		{name: "unknown user", userName: "nobody", password: "nothing"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			outbound := startTestOutbound(t, port, "tcp", "HANDSHAKE_NO_WAIT", testCase.userName, testCase.password)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			conn, err := outbound.DialContext(ctx, N.NetworkTCP, M.SocksaddrFromNet(echo.Addr()))
			if err == nil {
				_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
				_, _ = conn.Write([]byte("must not echo"))
				response := make([]byte, 13)
				_, err = io.ReadFull(conn, response)
				_ = conn.Close()
			}
			require.Error(t, err, "mieru accepted invalid credentials")
		})
	}
}

func TestOutboundConfigValidation(t *testing.T) {
	validOptions := func() option.MieruOutboundOptions {
		return option.MieruOutboundOptions{
			ServerOptions: option.ServerOptions{Server: "127.0.0.1", ServerPort: 8388},
			UserName:      testUserName,
			Password:      testPassword,
			Transport:     "tcp",
		}
	}
	testCases := []struct {
		name   string
		mutate func(*option.MieruOutboundOptions)
		want   string
	}{
		{name: "missing server", mutate: func(options *option.MieruOutboundOptions) { options.Server = "" }, want: "server"},
		{name: "missing username", mutate: func(options *option.MieruOutboundOptions) { options.UserName = "" }, want: "username"},
		{name: "missing password", mutate: func(options *option.MieruOutboundOptions) { options.Password = "" }, want: "password"},
		{name: "missing port", mutate: func(options *option.MieruOutboundOptions) { options.ServerPort = 0 }, want: "port"},
		{name: "invalid transport", mutate: func(options *option.MieruOutboundOptions) { options.Transport = "sctp" }, want: "transport"},
		{name: "invalid handshake", mutate: func(options *option.MieruOutboundOptions) { options.HandshakeMode = "fast" }, want: "handshake_mode"},
		{name: "invalid multiplexing", mutate: func(options *option.MieruOutboundOptions) { options.Multiplexing = "turbo" }, want: "multiplexing"},
		{name: "port and range", mutate: func(options *option.MieruOutboundOptions) { options.PortRange = "8000-9000" }, want: "either"},
		{name: "invalid range", mutate: func(options *option.MieruOutboundOptions) {
			options.ServerPort = 0
			options.PortRange = "9000-8000"
		}, want: "range"},
		{name: "binding without port", mutate: func(options *option.MieruOutboundOptions) {
			options.ServerPort = 0
			options.PortBindings = []option.MieruPortBinding{{Transport: "tcp"}}
		}, want: "neither"},
		{name: "binding with both", mutate: func(options *option.MieruOutboundOptions) {
			options.ServerPort = 0
			options.PortBindings = []option.MieruPortBinding{{Port: 8388, PortRange: "8000-9000", Transport: "tcp"}}
		}, want: "both"},
		{name: "binding and shorthand", mutate: func(options *option.MieruOutboundOptions) {
			options.PortBindings = []option.MieruPortBinding{{Port: 8388, Transport: "tcp"}}
		}, want: "either"},
	}
	logger := log.NewNOPFactory().Logger()
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			options := validOptions()
			testCase.mutate(&options)
			_, err := NewOutbound(t.Context(), nil, logger, "test", options)
			require.Error(t, err)
			require.Contains(t, err.Error(), testCase.want)
		})
	}
}

func TestInboundConfigValidation(t *testing.T) {
	validUsers := func() []option.MieruUser {
		return []option.MieruUser{{UserName: testUserName, Password: testPassword}}
	}
	testCases := []struct {
		name   string
		mutate func(*option.MieruInboundOptions)
		want   string
	}{
		{name: "missing users", mutate: func(options *option.MieruInboundOptions) { options.Users = nil }, want: "user"},
		{name: "empty username", mutate: func(options *option.MieruInboundOptions) { options.Users[0].UserName = "" }, want: "username"},
		{name: "empty password", mutate: func(options *option.MieruInboundOptions) { options.Users[0].Password = "" }, want: "password"},
		{name: "duplicate user", mutate: func(options *option.MieruInboundOptions) {
			options.Users = append(options.Users, options.Users[0])
		}, want: "duplicated"},
		{name: "missing port", mutate: func(options *option.MieruInboundOptions) { options.ListenPort = 0 }, want: "port"},
		{name: "invalid transport", mutate: func(options *option.MieruInboundOptions) { options.Transport = "icmp" }, want: "transport"},
		{name: "invalid range", mutate: func(options *option.MieruInboundOptions) {
			options.ListenPort = 0
			options.PortRange = "1-65536"
		}, want: "range"},
	}
	logger := log.NewNOPFactory().Logger()
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			options := option.MieruInboundOptions{
				ListenOptions: option.ListenOptions{ListenPort: 8388},
				Transport:     "tcp",
				Users:         validUsers(),
			}
			testCase.mutate(&options)
			_, err := NewInbound(t.Context(), &testRouter{}, logger, "test", options)
			require.Error(t, err)
			require.Contains(t, err.Error(), testCase.want)
		})
	}
}

func TestCloseReleasesGoroutines(t *testing.T) {
	runCycle := func(t *testing.T) {
		tcpEcho := startTCPEcho(t)
		udpEcho := startUDPEcho(t)
		port := freePort(t)
		logger := log.NewNOPFactory().Logger()
		listenAddress := badoption.Addr(netip.MustParseAddr("127.0.0.1"))
		createdInbound, err := NewInbound(t.Context(), &testRouter{}, logger, "mieru-in", option.MieruInboundOptions{
			ListenOptions: option.ListenOptions{Listen: &listenAddress},
			PortBindings:  []option.MieruPortBinding{{Port: port, Transport: "tcp"}, {Port: port, Transport: "udp"}},
			Users:         []option.MieruUser{{UserName: testUserName, Password: testPassword}},
		})
		require.NoError(t, err)
		inbound := createdInbound.(*Inbound)
		inboundScope := adapter.NewScope(t.Context(), logger)
		require.NoError(t, inbound.Start(adapter.StartStateInitialize, inboundScope))
		require.NoError(t, inbound.Start(adapter.StartStateStart, inboundScope))

		createdOutbound, err := NewOutbound(t.Context(), nil, logger, "mieru-out", option.MieruOutboundOptions{
			ServerOptions: option.ServerOptions{Server: "127.0.0.1"},
			PortBindings:  []option.MieruPortBinding{{Port: port, Transport: "tcp"}, {Port: port, Transport: "udp"}},
			UserName:      testUserName,
			Password:      testPassword,
			Multiplexing:  "off",
		})
		require.NoError(t, err)
		outbound := createdOutbound.(*Outbound)
		outboundScope := adapter.NewScope(t.Context(), logger)
		require.NoError(t, outbound.Start(adapter.StartStateInitialize, outboundScope))
		require.NoError(t, outbound.Start(adapter.StartStateStart, outboundScope))

		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		conn, err := outbound.DialContext(ctx, N.NetworkTCP, M.SocksaddrFromNet(tcpEcho.Addr()))
		cancel()
		require.NoError(t, err)
		_, err = conn.Write([]byte("close cycle"))
		require.NoError(t, err)
		response := make([]byte, 11)
		_, err = io.ReadFull(conn, response)
		require.NoError(t, err)
		require.NoError(t, conn.Close())

		ctx, cancel = context.WithTimeout(t.Context(), 5*time.Second)
		packetConn, err := outbound.ListenPacket(ctx, M.SocksaddrFromNet(udpEcho.LocalAddr()))
		cancel()
		require.NoError(t, err)
		require.NoError(t, packetConn.SetDeadline(time.Now().Add(5*time.Second)))
		_, err = packetConn.WriteTo([]byte("close cycle udp"), udpEcho.LocalAddr())
		require.NoError(t, err)
		udpResponse := make([]byte, 64)
		n, _, err := packetConn.ReadFrom(udpResponse)
		require.NoError(t, err)
		require.Equal(t, []byte("close cycle udp"), udpResponse[:n])
		require.NoError(t, packetConn.Close())

		require.NoError(t, outbound.Close())
		require.NoError(t, inbound.Close())
		require.NoError(t, tcpEcho.Close())
		require.NoError(t, udpEcho.Close())
	}

	runCycle(t)
	time.Sleep(500 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	runCycle(t)
	require.Eventually(t, func() bool {
		return runtime.NumGoroutine() <= baseline+4
	}, 5*time.Second, 50*time.Millisecond, "goroutines after close: want <= %d, got %d", baseline+4, runtime.NumGoroutine())
}

func TestBuildPortBindings(t *testing.T) {
	bindings, err := buildPortBindings([]option.MieruPortBinding{{Port: 8388}}, 0, "", "udp")
	require.NoError(t, err)
	require.Len(t, bindings, 1)
	require.Equal(t, mierupb.TransportProtocol_UDP, bindings[0].GetProtocol())
	require.EqualValues(t, 8388, bindings[0].GetPort())

	bindings, err = buildPortBindings(nil, 0, "8000-8002", "tcp")
	require.NoError(t, err)
	require.Len(t, bindings, 1)
	require.Equal(t, "8000-8002", bindings[0].GetPortRange())

	_, err = buildPortBindings(nil, 8388, "8000-8002", "tcp")
	require.ErrorContains(t, err, "either")
}

func TestCloseBeforeStart(t *testing.T) {
	logger := log.NewNOPFactory().Logger()
	createdOutbound, err := NewOutbound(t.Context(), nil, logger, "test", option.MieruOutboundOptions{
		ServerOptions: option.ServerOptions{Server: "127.0.0.1", ServerPort: 8388},
		Transport:     "tcp",
		UserName:      testUserName,
		Password:      testPassword,
	})
	require.NoError(t, err)
	require.NoError(t, createdOutbound.(*Outbound).Close())

	createdInbound, err := NewInbound(t.Context(), &testRouter{}, logger, "test", option.MieruInboundOptions{
		ListenOptions: option.ListenOptions{ListenPort: 8388},
		Transport:     "tcp",
		Users:         []option.MieruUser{{UserName: testUserName, Password: testPassword}},
	})
	require.NoError(t, err)
	require.NoError(t, createdInbound.(*Inbound).Close())
}
