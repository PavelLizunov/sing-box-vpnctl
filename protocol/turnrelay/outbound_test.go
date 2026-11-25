//go:build with_turnrelay

package turnrelay

import (
	"context"
	"encoding/hex"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	pion "github.com/pion/logging"
	pionturn "github.com/pion/turn/v5"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/turnrelay/internal/upstream/exit"
	"github.com/sagernet/sing-box/protocol/turnrelay/internal/upstream/obfs"
	"github.com/sagernet/sing-box/protocol/turnrelay/internal/upstream/relay"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/stretchr/testify/require"
)

const (
	testTURNUser     = "turn-user"
	testTURNPassword = "turn-password"
	testExitPassword = "exit-password"
)

func startTURNServer(t *testing.T) *net.UDPAddr {
	t.Helper()
	address, _ := listenTURN(t, "127.0.0.1:0")
	return address
}

func listenTURN(t *testing.T, address string) (*net.UDPAddr, func()) {
	t.Helper()
	packet, err := net.ListenPacket("udp4", address)
	require.NoError(t, err)
	const realm = "turnrelay.test"
	key := pionturn.GenerateAuthKey(testTURNUser, realm, testTURNPassword)
	loggerFactory := pion.NewDefaultLoggerFactory()
	loggerFactory.Writer = io.Discard
	server, err := pionturn.NewServer(pionturn.ServerConfig{
		Realm:         realm,
		LoggerFactory: loggerFactory,
		AuthHandler: func(request *pionturn.RequestAttributes) (string, []byte, bool) {
			if request.Username != testTURNUser || request.Realm != realm {
				return "", nil, false
			}
			return request.Username, key, true
		},
		PacketConnConfigs: []pionturn.PacketConnConfig{{
			PacketConn: packet,
			RelayAddressGenerator: &pionturn.RelayAddressGeneratorStatic{
				RelayAddress: net.ParseIP("127.0.0.1"),
				Address:      "127.0.0.1",
			},
		}},
	})
	require.NoError(t, err)
	stop := sync.OnceFunc(func() {
		_ = server.Close()
		_ = packet.Close()
	})
	t.Cleanup(stop)
	return packet.LocalAddr().(*net.UDPAddr), stop
}

type detouredConn struct{ net.Conn }

func (detouredConn) RemoteAddr() net.Addr { return M.ParseSocksaddr("relay.invalid:3478") }

func TestRelayAllocationThroughNamedDetour(t *testing.T) {
	address := startTURNServer(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	allocation, err := relay.Allocate(ctx, relay.Options{
		Server: "relay.invalid:3478", Username: testTURNUser, Password: testTURNPassword, UDP: true,
		DialContext: func(ctx context.Context, network, remote string) (net.Conn, error) {
			require.Equal(t, "relay.invalid:3478", remote)
			c, err := (&net.Dialer{}).DialContext(ctx, network, address.String())
			if err != nil {
				return nil, err
			}
			return detouredConn{c}, nil
		},
	})
	require.NoError(t, err)
	require.NoError(t, allocation.Close())
}

func startExitServer(t *testing.T, mode obfs.Mode) (*net.UDPAddr, []byte) {
	t.Helper()
	cert, err := obfs.GenerateServerCert()
	require.NoError(t, err)
	instance, err := exit.Listen(t.Context(), exit.ListenConfig{
		Address:  "127.0.0.1:0",
		Mode:     mode,
		Password: testExitPassword,
		Cert:     &cert,
		Server:   exit.ServerOptions{AllowPrivate: true},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = instance.Close() })
	return instance.Addr().(*net.UDPAddr), obfs.CertFingerprint(cert)
}

func testOutbound(t *testing.T, turnAddr, exitAddr *net.UDPAddr, fingerprint []byte, connections int, mode string) *Outbound {
	t.Helper()
	options := Options{
		ServerOptions:     option.ServerOptions{Server: exitAddr.IP.String(), ServerPort: uint16(exitAddr.Port)},
		Provider:          "static",
		ServerType:        "exit",
		TURNServer:        turnAddr.String(),
		TURNUsername:      testTURNUser,
		TURNPassword:      testTURNPassword,
		Password:          testExitPassword,
		ServerFingerprint: hex.EncodeToString(fingerprint),
		Connections:       connections,
		Mode:              mode,
	}
	created, err := NewOutbound(t.Context(), nil, log.NewNOPFactory().Logger(), "relay", options)
	require.NoError(t, err)
	out := created.(*Outbound)
	scope := adapter.NewScope(t.Context(), log.NewNOPFactory().Logger())
	require.NoError(t, out.Start(adapter.StartStateStart, scope))
	t.Cleanup(func() {
		_ = out.Close()
		_ = scope.Close()
	})
	return out
}

func startTCPEcho(t *testing.T) *net.TCPAddr {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
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
	return listener.Addr().(*net.TCPAddr)
}

func startUDPEcho(t *testing.T) *net.UDPAddr {
	t.Helper()
	packet, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = packet.Close() })
	go func() {
		buffer := make([]byte, 65535)
		for {
			n, from, err := packet.ReadFrom(buffer)
			if err != nil {
				return
			}
			_, _ = packet.WriteTo(buffer[:n], from)
		}
	}()
	return packet.LocalAddr().(*net.UDPAddr)
}

func TestExitSurvivesRelayRestart(t *testing.T) {
	address, stop := listenTURN(t, "127.0.0.1:0")
	exitAddr, fingerprint := startExitServer(t, obfs.ModeDTLS)
	out := testOutbound(t, address, exitAddr, fingerprint, 2, "dtls")
	readyCtx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	require.NoError(t, out.pool.WaitReady(readyCtx, 2))
	peer := M.SocksaddrFromNet(startUDPEcho(t))
	var nonce byte
	echo := func() bool {
		conn, err := out.DialContext(t.Context(), "udp", peer)
		if err != nil {
			return false
		}
		defer conn.Close()
		nonce++
		if _, err := conn.Write([]byte{nonce}); err != nil {
			return false
		}
		if err := conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
			return false
		}
		var reply [8]byte
		for {
			n, err := conn.Read(reply[:])
			if err != nil {
				return false
			}
			if n == 1 && reply[0] == nonce {
				return true
			}
		}
	}
	require.Eventually(t, echo, 10*time.Second, 200*time.Millisecond)
	stop()
	require.False(t, echo())
	listenTURN(t, address.String())
	require.Eventually(t, echo, 40*time.Second, 300*time.Millisecond)
}

func TestExitOverStaticTURN(t *testing.T) {
	turnAddr := startTURNServer(t)
	exitAddr, fingerprint := startExitServer(t, obfs.ModeSRTP)
	out := testOutbound(t, turnAddr, exitAddr, fingerprint, 2, "srtp")
	readyCtx, readyCancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer readyCancel()
	require.NoError(t, out.pool.WaitReady(readyCtx, 2))

	t.Run("tcp", func(t *testing.T) {
		echoAddr := startTCPEcho(t)
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()
		conn, err := out.DialContext(ctx, "tcp", M.SocksaddrFromNet(echoAddr))
		require.NoError(t, err)
		defer conn.Close()
		payload := []byte("turnrelay tcp")
		_, err = conn.Write(payload)
		require.NoError(t, err)
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		buffer := make([]byte, len(payload))
		_, err = io.ReadFull(conn, buffer)
		require.NoError(t, err)
		require.Equal(t, payload, buffer)
	})

	t.Run("udp dial", func(t *testing.T) {
		echoAddr := startUDPEcho(t)
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()
		conn, err := out.DialContext(ctx, "udp", M.SocksaddrFromNet(echoAddr))
		require.NoError(t, err)
		defer conn.Close()
		payload := []byte("turnrelay udp")
		_, err = conn.Write(payload)
		require.NoError(t, err)
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		buffer := make([]byte, len(payload))
		n, err := conn.Read(buffer)
		require.NoError(t, err)
		require.Equal(t, payload, buffer[:n])
	})

	t.Run("udp packet", func(t *testing.T) {
		echoAddr := startUDPEcho(t)
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()
		packet, err := out.ListenPacket(ctx, M.SocksaddrFromNet(echoAddr))
		require.NoError(t, err)
		defer packet.Close()
		payload := []byte("turnrelay packet")
		_, err = packet.WriteTo(payload, echoAddr)
		require.NoError(t, err)
		_ = packet.SetReadDeadline(time.Now().Add(10 * time.Second))
		buffer := make([]byte, len(payload))
		n, from, err := packet.ReadFrom(buffer)
		require.NoError(t, err)
		require.Equal(t, payload, buffer[:n])
		require.Equal(t, echoAddr.String(), from.String())
	})
}

func TestExitRejectsWrongPin(t *testing.T) {
	turnAddr := startTURNServer(t)
	exitAddr, _ := startExitServer(t, obfs.ModeSRTP)
	otherCert, err := obfs.GenerateServerCert()
	require.NoError(t, err)
	out := testOutbound(t, turnAddr, exitAddr, obfs.CertFingerprint(otherCert), 1, "srtp")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	err = out.pool.WaitReady(ctx, 1)
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestExitOverStaticTURNDTLS(t *testing.T) {
	turnAddr := startTURNServer(t)
	exitAddr, fingerprint := startExitServer(t, obfs.ModeDTLS)
	out := testOutbound(t, turnAddr, exitAddr, fingerprint, 1, "dtls")
	readyCtx, readyCancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer readyCancel()
	require.NoError(t, out.pool.WaitReady(readyCtx, 1))
	echoAddr := startTCPEcho(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	conn, err := out.DialContext(ctx, "tcp", M.SocksaddrFromNet(echoAddr))
	require.NoError(t, err)
	defer conn.Close()
	payload := []byte("turnrelay dtls")
	_, err = conn.Write(payload)
	require.NoError(t, err)
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	buffer := make([]byte, len(payload))
	_, err = io.ReadFull(conn, buffer)
	require.NoError(t, err)
	require.Equal(t, payload, buffer)
}
