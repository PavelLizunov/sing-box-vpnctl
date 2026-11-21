package tls

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/url"
	"testing"
	"time"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
)

func TestSPIFFEClientRequiresVerifiedGoTLS(t *testing.T) {
	valid := option.OutboundTLSOptions{
		Enabled:      true,
		ClientSPIFFE: &option.TLSClientSPIFFEOptions{ID: "spiffe://example.org/current"},
	}
	if err := validateClientSPIFFEOptions(valid); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(*option.OutboundTLSOptions)
	}{
		{"disabled", func(options *option.OutboundTLSOptions) { options.Enabled = false }},
		{"insecure", func(options *option.OutboundTLSOptions) { options.Insecure = true }},
		{"other engine", func(options *option.OutboundTLSOptions) { options.Engine = C.TLSEngineWindows }},
		{"static key", func(options *option.OutboundTLSOptions) { options.ClientKeyPath = "key.pem" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			options := valid
			test.change(&options)
			if err := validateClientSPIFFEOptions(options); err == nil {
				t.Fatal("configuration should be rejected")
			}
		})
	}
}

func TestSPIFFEClientRejectsRemoteWorkloadAPI(t *testing.T) {
	_, err := spiffeClientCertificate(option.TLSClientSPIFFEOptions{
		Endpoint: "tcp://127.0.0.1:8081",
		ID:       "spiffe://example.org/current",
	})
	if err == nil {
		t.Fatal("the Workload API must use a local Unix socket or named pipe")
	}
}

func TestSPIFFEServerRequiresMatchingVerifiedIdentity(t *testing.T) {
	verify, err := spiffeServerVerifier([]string{"spiffe://example.org/current"})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		path     string
		verified bool
		resumed  bool
		allowed  bool
	}{
		{"/current", true, false, true},
		{"/previous", true, false, false},
		{"/current", false, false, false},
		{"/current", true, true, false},
	} {
		leaf := &x509.Certificate{URIs: []*url.URL{{Scheme: "spiffe", Host: "example.org", Path: test.path}}}
		state := tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf}, DidResume: test.resumed}
		if test.verified {
			state.VerifiedChains = [][]*x509.Certificate{{leaf}}
		}
		if allowed := verify(state) == nil; allowed != test.allowed {
			t.Fatalf("path %q verified=%t: allowed=%t, want %t", test.path, test.verified, allowed, test.allowed)
		}
	}
}

func TestSPIFFEClientFailsWhenPeerDoesNotRequestCertificate(t *testing.T) {
	cert, err := GenerateKeyPair(nil, nil, time.Now, "localhost")
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	trust := x509.NewCertPool()
	trust.AddCert(leaf)

	for _, test := range []struct {
		name string
		auth tls.ClientAuthType
	}{
		{"ordinary TLS", tls.NoClientCert},
		{"client certificate requested", tls.RequireAnyClientCert},
	} {
		t.Run(test.name, func(t *testing.T) {
			clientPipe, serverPipe := net.Pipe()
			defer clientPipe.Close()
			defer serverPipe.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			server := tls.Server(serverPipe, &tls.Config{
				Certificates:           []tls.Certificate{*cert},
				ClientAuth:             test.auth,
				SessionTicketsDisabled: true,
			})
			serverResult := make(chan error, 1)
			go func() {
				serverResult <- server.HandshakeContext(ctx)
			}()
			config := &STDClientConfig{config: &tls.Config{
				ServerName: "localhost",
				RootCAs:    trust,
				GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
					return cert, nil
				},
			}}
			client, err := config.Client(clientPipe)
			if err != nil {
				t.Fatal(err)
			}
			err = client.HandshakeContext(ctx)
			if test.auth == tls.NoClientCert {
				if err == nil {
					t.Fatal("ordinary TLS completed without a client certificate")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if serverErr := <-serverResult; serverErr != nil {
				t.Fatal(serverErr)
			}
		})
	}
}
