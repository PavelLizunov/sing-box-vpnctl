//go:build with_turn && with_wireguard && with_gvisor

package include

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
	pion "github.com/pion/turn/v5"
	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	boxTLS "github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/service"
	"github.com/stretchr/testify/require"
)

type turnTestListener struct {
	net.Listener
	accepted chan net.Conn
}

func (l *turnTestListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err == nil {
		select {
		case l.accepted <- conn:
		default:
		}
	}
	return conn, err
}

func startWGTurnBox(t *testing.T, config string) (*box.Box, adapter.DNSRouter) {
	t.Helper()
	ctx := Context(t.Context())
	options, err := json.UnmarshalExtendedContext[option.Options](ctx, []byte(config))
	require.NoError(t, err)
	instance, err := box.New(box.Options{Context: ctx, Options: options})
	require.NoError(t, err)
	require.NoError(t, instance.Start())
	t.Cleanup(func() { instance.Close() })
	return instance, service.FromContext[adapter.DNSRouter](ctx)
}

func TestWireGuardDetourOverTURN(t *testing.T) {
	const (
		name     = "relay.test"
		serverIP = "10.197.31.1"
		clientIP = "10.197.31.2"
	)
	key, cert, err := boxTLS.GenerateCertificate(nil, nil, time.Now, name, time.Now().Add(time.Hour))
	require.NoError(t, err)
	identity, err := tls.X509KeyPair(cert, key)
	require.NoError(t, err)
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{identity}}
	var allocations atomic.Uint32
	startRelay := func(address string) (*pion.Server, *turnTestListener) {
		t.Helper()
		listener, err := tls.Listen("tcp4", address, tlsConfig)
		require.NoError(t, err)
		tracked := &turnTestListener{Listener: listener, accepted: make(chan net.Conn, 1)}
		relay, err := pion.NewServer(pion.ServerConfig{
			Realm: name,
			AuthHandler: func(req *pion.RequestAttributes) (string, []byte, bool) {
				return "test", pion.GenerateAuthKey("test", name, "test-password"), req.Username == "test"
			},
			EventHandler: pion.EventHandler{OnAllocationCreated: func(_, _ net.Addr, _, _, _ string, _ net.Addr, _ int) {
				allocations.Add(1)
			}},
			ListenerConfigs: []pion.ListenerConfig{{
				Listener:              tracked,
				PermissionHandler:     func(_ net.Addr, ip net.IP) bool { return ip.IsLoopback() },
				RelayAddressGenerator: &pion.RelayAddressGeneratorStatic{RelayAddress: net.IPv4(127, 0, 0, 1), Address: "127.0.0.1"},
			}},
		})
		require.NoError(t, err)
		t.Cleanup(func() { relay.Close() })
		return relay, tracked
	}
	relay, wire := startRelay("127.0.0.1:0")

	serverKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	require.NoError(t, err)
	clientKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	require.NoError(t, err)
	freePort, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	wgPort := M.SocksaddrFromNet(freePort.LocalAddr()).Port
	require.NoError(t, freePort.Close())
	b64 := base64.StdEncoding.EncodeToString
	startWGTurnBox(t, fmt.Sprintf(`{
		"log":{"level":"error"},
		"dns":{"servers":[{"type":"hosts","tag":"offline"}]},
		"outbounds":[{"type":"direct","tag":"local"}],
		"route":{"final":"local"},
		"endpoints":[{"type":"wireguard","tag":"wg-server","system":false,"workers":1,
			"address":[%q],"private_key":%q,"listen_port":%d,
			"peers":[{"public_key":%q,"allowed_ips":[%q]}]}]
	}`, serverIP+"/32", b64(serverKey.Bytes()), wgPort, b64(clientKey.PublicKey().Bytes()), clientIP+"/32"))

	dnsSocket, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	ready := make(chan struct{})
	var dnsRequests atomic.Uint32
	dnsServer := &dns.Server{PacketConn: dnsSocket, NotifyStartedFunc: func() { close(ready) }, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, m *dns.Msg) {
		dnsRequests.Add(1)
		reply := new(dns.Msg).SetReply(m)
		reply.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: m.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET}, A: net.IPv4(192, 0, 2, 21)}}
		_ = w.WriteMsg(reply)
	})}
	go dnsServer.ActivateAndServe()
	<-ready
	t.Cleanup(func() { dnsServer.Shutdown() })
	client, resolver := startWGTurnBox(t, fmt.Sprintf(`{
		"log":{"level":"error"},
		"dns":{"servers":[{"type":"udp","tag":"inside","server":%q,"server_port":%d,"detour":"wg"}],"disable_cache":true},
		"outbounds":[{"type":"turn","tag":"relay","server":"127.0.0.1","server_port":%d,
			"username":"test","password":"test-password","tls":{"enabled":true,"server_name":%q,"certificate":%q}}],
		"endpoints":[{"type":"wireguard","tag":"wg","system":false,"workers":1,
			"address":[%q],"private_key":%q,"detour":"relay",
			"peers":[{"address":"127.0.0.1","port":%d,"public_key":%q,"allowed_ips":[%q]}]}],
		"route":{"final":"wg"}
	}`, serverIP, M.SocksaddrFromNet(dnsSocket.LocalAddr()).Port, M.SocksaddrFromNet(wire.Addr()).Port, name, cert,
		clientIP+"/32", b64(clientKey.Bytes()), wgPort, b64(serverKey.PublicKey().Bytes()), serverIP+"/32"))
	path := client.Outbound().Default()
	require.Equal(t, "wg", path.Tag())

	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "through wireguard") }))
	t.Cleanup(httpServer.Close)
	httpTransport := &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return path.DialContext(ctx, network, M.ParseSocksaddr(address))
		},
	}
	httpClient := &http.Client{Transport: httpTransport, Timeout: 8 * time.Second}
	url := strings.Replace(httpServer.URL, "127.0.0.1", serverIP, 1)

	udpSocket, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { udpSocket.Close() })
	var received atomic.Uint32
	go func() {
		var buf [256]byte
		for {
			n, src, err := udpSocket.ReadFrom(buf[:])
			if err != nil {
				return
			}
			received.Add(1)
			udpSocket.WriteTo(buf[:n], src)
		}
	}()
	udp := func(data string, timeout time.Duration) error {
		ctx, cancel := context.WithTimeout(t.Context(), timeout)
		defer cancel()
		conn, err := path.DialContext(ctx, "udp", M.ParseSocksaddrHostPort(serverIP, M.SocksaddrFromNet(udpSocket.LocalAddr()).Port))
		if err != nil {
			return err
		}
		defer conn.Close()
		deadline, _ := ctx.Deadline()
		if err := conn.SetDeadline(deadline); err != nil {
			return err
		}
		if _, err := io.WriteString(conn, data); err != nil {
			return err
		}
		var reply [256]byte
		n, err := conn.Read(reply[:])
		if err == nil && string(reply[:n]) != data {
			return fmt.Errorf("unexpected UDP reply %q", reply[:n])
		}
		return err
	}
	checkPath := func() {
		t.Helper()
		resp, err := httpClient.Get(url)
		require.NoError(t, err)
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		require.NoError(t, err)
		require.Equal(t, "through wireguard", string(body))
		require.NoError(t, udp("echo", 3*time.Second))
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		defer cancel()
		answer, err := resolver.Exchange(ctx, new(dns.Msg).SetQuestion("wg-only.test.", dns.TypeA), adapter.DNSQueryOptions{})
		require.NoError(t, err)
		require.Len(t, answer.Answer, 1)
		require.Equal(t, "192.0.2.21", answer.Answer[0].(*dns.A).A.String())
	}
	checkPath()
	require.Positive(t, allocations.Load())
	original := allocations.Load()
	require.NoError(t, relay.Close())
	select {
	case conn := <-wire.accepted:
		_ = conn.Close()
	case <-time.After(time.Second):
		t.Fatal("missing TURN control connection")
	}
	before := received.Load()
	require.Error(t, udp("relay-down", 1500*time.Millisecond))
	require.Equal(t, before, received.Load(), "payload reached the receiver while TURN was down")
	beforeDNS := dnsRequests.Load()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	answer, err := resolver.Exchange(ctx, new(dns.Msg).SetQuestion("relay-down.test.", dns.TypeA), adapter.DNSQueryOptions{})
	cancel()
	require.True(t, err != nil || (answer != nil && answer.Rcode != dns.RcodeSuccess), "unexpected DNS resolution while TURN was down")
	require.Equal(t, beforeDNS, dnsRequests.Load(), "query reached the resolver while TURN was down")
	startRelay(wire.Addr().String())
	require.Eventually(t, func() bool { return allocations.Load() > original }, 8*time.Second, 50*time.Millisecond)
	checkPath()
	beforeReset := allocations.Load()
	client.Network().ResetNetwork(t.Context())
	require.Eventually(t, func() bool { return allocations.Load() > beforeReset }, 8*time.Second, 50*time.Millisecond)
	checkPath()
}
