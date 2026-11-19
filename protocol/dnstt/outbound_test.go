package dnstt

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/net2share/vaydns/noise"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	sshout "github.com/sagernet/sing-box/protocol/ssh"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/service"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func TestOutboundValidation(t *testing.T) {
	options := option.DNSTTOutboundOptions{
		ServerOptions: option.ServerOptions{Server: "127.0.0.1"},
		Domain:        "tunnel.example.net.",
		PublicKey:     strings.Repeat("01", 32),
	}
	logger := log.NewNOPFactory().Logger()
	valid, err := NewOutbound(t.Context(), nil, logger, "dnstt", options)
	require.NoError(t, err)
	o := valid.(*Outbound)
	t.Cleanup(func() { o.Close() })
	require.Equal(t, uint16(53), o.resolver.Port)
	require.Equal(t, []string{"tcp"}, o.Network())
	_, err = o.ListenPacket(t.Context(), M.Socksaddr{})
	require.ErrorContains(t, err, "only supports TCP")
	_, err = o.DialContext(t.Context(), "udp", M.Socksaddr{})
	require.ErrorContains(t, err, "only supports TCP")

	for _, field := range []string{"server", "domain", "public_key"} {
		t.Run(field, func(t *testing.T) {
			invalid := options
			switch field {
			case "server":
				invalid.Server = ""
			case "domain":
				invalid.Domain = "."
			case "public_key":
				invalid.PublicKey = "invalid"
			}
			_, err := NewOutbound(t.Context(), nil, logger, "dnstt", invalid)
			require.ErrorContains(t, err, field)
		})
	}
}

func TestOriginalServerSSHDetour(t *testing.T) {
	serverBinary := os.Getenv("DNSTT_SERVER")
	if serverBinary == "" {
		t.Skip("set DNSTT_SERVER to the original dnstt-server binary")
	}
	payload := make([]byte, 64*1024)
	_, err := rand.Read(payload)
	require.NoError(t, err)
	destination := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(payload)
	}))
	t.Cleanup(destination.Close)
	sshAddr, hostKey := startSSHServer(t, destination.Listener.Addr().String())
	resolver, publicKey := startOriginalServer(t, serverBinary, sshAddr)

	registry := outbound.NewRegistry()
	RegisterOutbound(registry)
	sshout.RegisterOutbound(registry)
	manager := outbound.NewManager(registry, nil, "ssh")
	ctx := service.ContextWith[adapter.OutboundManager](service.ContextWithDefaultRegistry(t.Context()), manager)
	logger := log.NewNOPFactory().Logger()
	require.NoError(t, manager.Create(ctx, nil, logger, "tunnel", Type, &option.DNSTTOutboundOptions{
		ServerOptions: option.ServerOptions{Server: "127.0.0.1", ServerPort: resolver},
		Domain:        "tunnel.test",
		PublicKey:     publicKey,
	}))
	tunnel, found := manager.Outbound("tunnel")
	require.True(t, found)
	t.Cleanup(func() { tunnel.(*Outbound).Close() })
	require.NoError(t, manager.Create(ctx, nil, logger, "ssh", C.TypeSSH, &option.SSHOutboundOptions{
		DialerOptions: option.DialerOptions{Detour: "tunnel"},
		ServerOptions: option.ServerOptions{Server: "192.0.2.1", ServerPort: 22},
		User:          "dnstt-test",
		HostKey:       []string{hostKey},
	}))
	sshOutbound := manager.Default()
	client := destination.Client()
	transport := client.Transport.(*http.Transport)
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return sshOutbound.DialContext(ctx, network, M.ParseSocksaddr(addr))
	}
	client.Timeout = 20 * time.Second
	t.Cleanup(client.CloseIdleConnections)

	for _, reconnect := range []bool{false, true} {
		if reconnect {
			client.CloseIdleConnections()
			sshOutbound.(adapter.IdleConnectionKeeper).CloseIdleConnections()
			tunnel.(adapter.InterfaceUpdateListener).InterfaceUpdated(ctx)
		}
		response, err := client.Get(destination.URL)
		require.NoError(t, err)
		data, err := io.ReadAll(response.Body)
		response.Body.Close()
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, response.StatusCode)
		require.True(t, bytes.Equal(payload, data), "payload changed in transit")
	}
}

func startOriginalServer(t *testing.T, binary, backend string) (uint16, string) {
	t.Helper()
	reserved, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	address := reserved.LocalAddr().(*net.UDPAddr)
	require.NoError(t, reserved.Close())
	key, err := noise.GeneratePrivkey()
	require.NoError(t, err)
	keyPath := filepath.Join(t.TempDir(), "server.key")
	require.NoError(t, os.WriteFile(keyPath, []byte(noise.EncodeKey(key)), 0o600))
	var serverLog bytes.Buffer
	server := exec.CommandContext(t.Context(), binary, "-udp", address.String(), "-privkey-file", keyPath, "tunnel.test", backend)
	server.Stderr = &serverLog
	require.NoError(t, server.Start())
	t.Cleanup(func() {
		server.Process.Kill()
		server.Wait()
		if t.Failed() {
			t.Log(serverLog.String())
		}
	})
	query := new(dns.Msg).SetQuestion(".", dns.TypeTXT)
	probe := dns.Client{Timeout: 100 * time.Millisecond}
	require.Eventually(t, func() bool {
		_, _, err := probe.Exchange(query, address.String())
		return err == nil
	}, 3*time.Second, 50*time.Millisecond, "dnstt-server did not answer a DNS probe")
	return uint16(address.Port), noise.EncodeKey(noise.PubkeyFromPrivkey(key))
}

func startSSHServer(t *testing.T, destination string) (string, string) {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(privateKey)
	require.NoError(t, err)
	config := &ssh.ServerConfig{NoClientAuth: true}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go acceptSSH(conn, config, destination)
		}
	}()
	return listener.Addr().String(), strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
}

func acceptSSH(conn net.Conn, config *ssh.ServerConfig, destination string) {
	defer conn.Close()
	_, channels, requests, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(requests)
	for request := range channels {
		var target struct {
			Host       string
			Port       uint32
			OriginHost string
			OriginPort uint32
		}
		if request.ChannelType() != "direct-tcpip" || ssh.Unmarshal(request.ExtraData(), &target) != nil || net.JoinHostPort(target.Host, fmt.Sprint(target.Port)) != destination {
			request.Reject(ssh.Prohibited, "unexpected destination")
			continue
		}
		go func() {
			upstream, err := net.DialTimeout("tcp", destination, 3*time.Second)
			if err != nil {
				request.Reject(ssh.ConnectionFailed, err.Error())
				return
			}
			defer upstream.Close()
			channel, requests, err := request.Accept()
			if err != nil {
				return
			}
			defer channel.Close()
			go ssh.DiscardRequests(requests)
			go io.Copy(upstream, channel)
			io.Copy(channel, upstream)
		}()
	}
}
