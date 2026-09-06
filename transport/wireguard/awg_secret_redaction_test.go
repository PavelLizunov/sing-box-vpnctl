//go:build with_awg && with_gvisor

package wireguard

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json"
	M "github.com/sagernet/sing/common/metadata"
)

func TestAWGEndpointStartSecretRedaction(t *testing.T) {
	// Deliberately synthetic, distinct markers; never use deployed credentials.
	privateKey := bytes.Repeat([]byte{0xa5}, 32)
	peerPSK := bytes.Repeat([]byte{0xb6}, 32)
	headerKey := bytes.Repeat([]byte{0xc7}, 32)
	peerPublicKey := bytes.Repeat([]byte{0xd8}, 32)
	for _, tc := range []struct {
		name    string
		s1      uint32
		padding string
	}{
		{
			name: "oversized_prefix",
			s1:   65535,
		},
		{
			name:    "malformed_padding_echo",
			s1:      12,
			padding: hex.EncodeToString(headerKey),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A short S prefix with HP would fail awgIpcLines, not Start.
			// Oversized S1 and malformed padding instead pass JSON and endpoint
			// construction, then reach the real device's runtime UAPI checks.
			configJSON := fmt.Sprintf(`{
				"address": ["192.0.2.1/32"],
				"private_key": %q,
				"peers": [{"public_key": %q, "pre_shared_key": %q,
					"allowed_ips": ["192.0.2.2/32"]}],
				"s1": %d, "s2": 12, "s3": 12, "s4": 12,
				"header_protection_key": %q,
				"content_padding_addition": %q
			}`, base64.StdEncoding.EncodeToString(privateKey),
				base64.StdEncoding.EncodeToString(peerPublicKey),
				base64.StdEncoding.EncodeToString(peerPSK), tc.s1,
				hex.EncodeToString(headerKey), tc.padding)
			var config option.WireGuardEndpointOptions
			if json.Unmarshal([]byte(configJSON), &config) != nil {
				t.Fatal("synthetic configuration must pass JSON decoding")
			}
			capture := &awgSecretCaptureLogger{ContextLogger: log.NewNOPFactory().Logger()}
			options := EndpointOptions{
				Context:    context.Background(),
				Logger:     capture,
				Dialer:     awgSecretNoNetworkDialer{},
				Address:    config.Address,
				PrivateKey: config.PrivateKey,
				AmneziaWG:  config.AmneziaWGOptions,
				Workers:    1,
			}
			for _, peer := range config.Peers {
				options.Peers = append(options.Peers, PeerOptions{
					PublicKey:    peer.PublicKey,
					PreSharedKey: peer.PreSharedKey,
					AllowedIPs:   peer.AllowedIPs,
				})
			}
			endpoint, err := NewEndpoint(options)
			if err != nil {
				t.Fatal("synthetic configuration must reach Start, not fail construction")
			}
			t.Cleanup(func() { _ = endpoint.Close() })
			fullIPC := endpoint.ipcConf
			for _, peer := range endpoint.peers {
				fullIPC += peer.GenerateIpcLines()
			}
			for _, field := range []string{
				"private_key=" + hex.EncodeToString(privateKey),
				"preshared_key=" + hex.EncodeToString(peerPSK),
				"header_protection_key=" + hex.EncodeToString(headerKey),
			} {
				if !strings.Contains(fullIPC, field) {
					t.Fatal("fixture must include all three synthetic secrets in IPC")
				}
			}

			err = endpoint.Start(false)
			if err == nil {
				t.Fatal("Start must reject the runtime UAPI configuration")
			}
			// Start closes the rejected device before returning. Snapshot under
			// the mutex because device workers also log during shutdown.
			capture.mu.Lock()
			debugLog := strings.Join(capture.debug, "\n")
			errorLog := strings.Join(capture.errors, "\n")
			capture.mu.Unlock()
			if err.Error() != "setup wireguard: rejected device configuration" {
				t.Error("Start must return the exact static configuration-rejection error")
			}
			if !strings.Contains(debugLog, "uapi: updating private key") {
				t.Error("debug capture must prove device UAPI processing was reached")
			}
			if !strings.Contains(errorLog, "uapi configuration rejected") &&
				!strings.Contains(errorLog, "failed to merge with device:") &&
				!strings.Contains(errorLog, "failed to parse content_padding_addition:") {
				t.Error("error capture must identify the static runtime UAPI rejection")
			}
			for _, output := range []struct{ name, text string }{
				{"returned error", err.Error()},
				{"debug log", debugLog},
				{"error log", errorLog},
			} {
				if strings.Contains(output.text, fullIPC) {
					t.Errorf("%s exposed full IPC configuration (content withheld)", output.name)
				}
				for _, secret := range []struct {
					name string
					data []byte
				}{
					{"private key", privateKey},
					{"peer PSK", peerPSK},
					{"header-protection key", headerKey},
				} {
					for _, encoded := range []string{
						base64.StdEncoding.EncodeToString(secret.data),
						base64.RawStdEncoding.EncodeToString(secret.data),
						hex.EncodeToString(secret.data),
						strings.ToUpper(hex.EncodeToString(secret.data)),
					} {
						if strings.Contains(output.text, encoded) {
							t.Errorf("%s exposed synthetic %s encoding (content withheld)", output.name, secret.name)
						}
					}
				}
			}
		})
	}
}

type awgSecretCaptureLogger struct {
	log.ContextLogger
	mu     sync.Mutex
	debug  []string
	errors []string
}

func (l *awgSecretCaptureLogger) Debug(args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.debug = append(l.debug, fmt.Sprint(args...))
}

func (l *awgSecretCaptureLogger) Error(args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.errors = append(l.errors, fmt.Sprint(args...))
}

func (l *awgSecretCaptureLogger) DebugContext(_ context.Context, args ...any) {
	l.Debug(args...)
}

func (l *awgSecretCaptureLogger) ErrorContext(_ context.Context, args ...any) {
	l.Error(args...)
}

// The real userspace device may start its receive loop before IpcSet rejects
// the config. Block until bind shutdown without opening a socket or doing DNS.
type awgSecretNoNetworkDialer struct{}

func (awgSecretNoNetworkDialer) DialContext(ctx context.Context, _ string, _ M.Socksaddr) (net.Conn, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (awgSecretNoNetworkDialer) ListenPacket(ctx context.Context, _ M.Socksaddr) (net.PacketConn, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
