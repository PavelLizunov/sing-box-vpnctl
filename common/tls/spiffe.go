package tls

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"time"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
)

type spiffeClientConn struct {
	*tls.Conn
	provided      atomic.Bool
	authenticated atomic.Bool
}

func newSPIFFEClientConn(conn net.Conn, config *tls.Config) *spiffeClientConn {
	config = config.Clone()
	config.ClientSessionCache = nil
	config.SessionTicketsDisabled = true
	client := new(spiffeClientConn)
	getCertificate := config.GetClientCertificate
	config.GetClientCertificate = func(request *tls.CertificateRequestInfo) (*tls.Certificate, error) {
		certificate, err := getCertificate(request)
		if err == nil && certificate != nil && len(certificate.Certificate) > 0 {
			client.provided.Store(true)
		}
		return certificate, err
	}
	client.Conn = tls.Client(conn, config)
	return client
}

func (c *spiffeClientConn) HandshakeContext(ctx context.Context) error {
	if c.authenticated.Load() {
		return nil
	}
	if err := c.Conn.HandshakeContext(ctx); err != nil {
		return err
	}
	if !c.provided.Load() || c.Conn.ConnectionState().DidResume {
		c.NetConn().Close()
		return errors.New("the TLS peer did not request a fresh SPIFFE client certificate")
	}
	c.authenticated.Store(true)
	return nil
}

func (c *spiffeClientConn) Handshake() error {
	return c.HandshakeContext(context.Background())
}

func (c *spiffeClientConn) Read(buffer []byte) (int, error) {
	if err := c.HandshakeContext(context.Background()); err != nil {
		return 0, err
	}
	return c.Conn.Read(buffer)
}

func (c *spiffeClientConn) Write(buffer []byte) (int, error) {
	if err := c.HandshakeContext(context.Background()); err != nil {
		return 0, err
	}
	return c.Conn.Write(buffer)
}

func validateClientSPIFFEOptions(options option.OutboundTLSOptions) error {
	if options.ClientSPIFFE == nil {
		return nil
	}
	if !options.Enabled || options.Insecure || options.Engine != "" && options.Engine != C.TLSEngineGo ||
		options.Reality != nil && options.Reality.Enabled || options.UTLS != nil && options.UTLS.Enabled {
		return errors.New("client_spiffe requires verified TLS with the Go engine")
	}
	if len(options.ClientCertificate) > 0 || options.ClientCertificatePath != "" ||
		len(options.ClientKey) > 0 || options.ClientKeyPath != "" {
		return errors.New("client_spiffe cannot be combined with a static client certificate or key")
	}
	return nil
}

func validateServerSPIFFEOptions(options option.InboundTLSOptions) error {
	if len(options.ClientSPIFFEID) == 0 {
		return nil
	}
	if !options.Enabled || options.Reality != nil && options.Reality.Enabled {
		return errors.New("client_spiffe_id requires TLS without Reality")
	}
	return nil
}

func spiffeClientCertificate(options option.TLSClientSPIFFEOptions) (func(*tls.CertificateRequestInfo) (*tls.Certificate, error), error) {
	if !strings.HasPrefix(options.Endpoint, "unix:") && !strings.HasPrefix(options.Endpoint, "npipe:") {
		return nil, errors.New("client_spiffe.endpoint must be a local Unix socket or named pipe")
	}
	if err := workloadapi.ValidateAddress(options.Endpoint); err != nil {
		return nil, fmt.Errorf("invalid client_spiffe.endpoint: %w", err)
	}
	expected, err := spiffeid.FromString(options.ID)
	if err != nil {
		return nil, fmt.Errorf("invalid client_spiffe.id: %w", err)
	}
	return func(request *tls.CertificateRequestInfo) (*tls.Certificate, error) {
		identities, err := workloadapi.FetchX509SVIDs(request.Context(), workloadapi.WithAddr(options.Endpoint))
		if err != nil {
			return nil, fmt.Errorf("fetch SPIFFE workload identity: %w", err)
		}
		now := time.Now()
		var selected *tls.Certificate
		for _, identity := range identities {
			if identity.ID != expected || len(identity.Certificates) == 0 {
				continue
			}
			leaf := identity.Certificates[0]
			if now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) {
				continue
			}
			certificate := &tls.Certificate{
				PrivateKey: identity.PrivateKey,
				Leaf:       leaf,
			}
			for _, cert := range identity.Certificates {
				certificate.Certificate = append(certificate.Certificate, cert.Raw)
			}
			if err := request.SupportsCertificate(certificate); err != nil {
				continue
			}
			if selected == nil || leaf.NotAfter.After(selected.Leaf.NotAfter) {
				selected = certificate
			}
		}
		if selected == nil {
			return nil, errors.New("no valid TLS certificate for the required SPIFFE workload identity")
		}
		return selected, nil
	}, nil
}

func spiffeServerVerifier(ids []string) (func(tls.ConnectionState) error, error) {
	allowed := make(map[spiffeid.ID]struct{}, len(ids))
	for _, value := range ids {
		id, err := spiffeid.FromString(value)
		if err != nil {
			return nil, fmt.Errorf("invalid client_spiffe_id: %w", err)
		}
		allowed[id] = struct{}{}
	}
	return func(connection tls.ConnectionState) error {
		if connection.DidResume {
			return errors.New("client SPIFFE identity requires a fresh TLS handshake")
		}
		if len(connection.VerifiedChains) == 0 || len(connection.PeerCertificates) == 0 {
			return errors.New("verified client SPIFFE certificate is required")
		}
		id, err := x509svid.IDFromCert(connection.PeerCertificates[0])
		if err != nil {
			return fmt.Errorf("invalid client SPIFFE certificate: %w", err)
		}
		if _, found := allowed[id]; !found {
			return errors.New("client SPIFFE identity is not allowed")
		}
		return nil
	}, nil
}
