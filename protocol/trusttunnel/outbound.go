//go:build with_trusttunnel

package trusttunnel

import (
	"context"
	stdTLS "crypto/tls"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/dialer"
	"github.com/sagernet/sing-box/common/tls"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/bufio"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	aTLS "github.com/sagernet/sing/common/tls"
	"github.com/sagernet/sing/service"

	TT "github.com/xchacha20-poly1305/sing-trusttunnel"
	"golang.org/x/net/http2"
)

func RegisterOutbound(registry *outbound.Registry) {
	outbound.Register(registry, C.TypeTrustTunnel, NewOutbound)
}

func validateQUIC(options option.TrustTunnelQUICOptions) error {
	if options.QUIC && !C.WithQUIC {
		return errors.New("TrustTunnel QUIC requires a build with with_quic")
	}
	switch options.QUICCongestionControl {
	case "":
	case "bbr", "cubic", "reno":
		if !options.QUIC {
			return errors.New("TrustTunnel quic_congestion_control requires quic")
		}
	default:
		return errors.New("unknown TrustTunnel QUIC congestion control")
	}
	return nil
}

func validateCredentials(username, password string) error {
	if username == "" || password == "" || strings.Contains(username, ":") {
		return errors.New("TrustTunnel requires a non-empty username without a colon and a non-empty password")
	}
	return nil
}

type Outbound struct {
	outbound.Adapter
	ctx        context.Context
	dialer     N.Dialer
	dnsRouter  adapter.DNSRouter
	server     M.Socksaddr
	tlsConfig  tls.Config
	authHeader string
	udp        bool
	quic       bool
	congestion string
	health     bool

	mu      sync.Mutex
	closed  bool
	session *clientSession
}

func NewOutbound(ctx context.Context, _ adapter.Router, logger log.ContextLogger, tag string, options option.TrustTunnelOutboundOptions) (adapter.Outbound, error) {
	if options.TLS == nil || !options.TLS.Enabled {
		return nil, errors.New("TLS is required for TrustTunnel outbound")
	}
	if err := validateCredentials(options.Username, options.Password); err != nil {
		return nil, err
	}
	if err := validateQUIC(options.TrustTunnelQUICOptions); err != nil {
		return nil, err
	}
	server := options.ServerOptions.Build()
	if !server.IsValid() || server.Port == 0 {
		return nil, errors.New("TrustTunnel outbound requires a server and server_port")
	}
	tlsConfig, err := tls.NewClient(ctx, logger, options.Server, *options.TLS)
	if err != nil {
		return nil, err
	}
	alpn := http2.NextProtoTLS
	if options.QUIC {
		alpn = "h3"
		if _, err = tlsConfig.STDConfig(); err != nil {
			return nil, err
		}
	}
	if protocols := tlsConfig.NextProtos(); len(protocols) > 0 && !slices.Contains(protocols, alpn) {
		return nil, E.New("TrustTunnel outbound requires ALPN ", alpn)
	}
	tlsConfig.SetNextProtos([]string{alpn})
	rawDialer, err := dialer.New(ctx, options.DialerOptions, options.ServerIsDomain())
	if err != nil {
		return nil, err
	}
	udp := options.UDP == nil || *options.UDP
	networks := []string{N.NetworkTCP}
	if udp {
		networks = append(networks, N.NetworkUDP)
	}
	return &Outbound{
		Adapter:    outbound.NewAdapterWithDialerOptions(C.TypeTrustTunnel, tag, networks, options.DialerOptions),
		ctx:        ctx,
		dialer:     rawDialer,
		dnsRouter:  service.FromContext[adapter.DNSRouter](ctx),
		server:     server,
		tlsConfig:  tlsConfig,
		authHeader: "Basic " + base64.StdEncoding.EncodeToString([]byte(options.Username+":"+options.Password)),
		udp:        udp, quic: options.QUIC, congestion: options.QUICCongestionControl, health: options.HealthCheck,
	}, nil
}

func (o *Outbound) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	if stage == adapter.StartStateStart {
		scope.Add(o.Close)
	}
	return nil
}

func (o *Outbound) InterfaceUpdated(context.Context) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.closeSession()
}

func (o *Outbound) Close() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.closed = true
	return o.closeSession()
}

func (o *Outbound) closeSession() error {
	if o.session == nil {
		return nil
	}
	session := o.session
	o.session = nil
	session.cancel()
	session.transport.CloseIdleConnections()
	err := session.closeTransport()
	if session.healthDone != nil {
		<-session.healthDone
	}
	return err
}

type clientSession struct {
	ctx            context.Context
	cancel         context.CancelFunc
	transport      TT.RoundTripper
	closeTransport func() error
	healthDone     chan struct{}
	activity       chan struct{}
}

func (o *Outbound) getSession() (*clientSession, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return nil, net.ErrClosed
	}
	if o.session != nil {
		return o.session, nil
	}
	ctx, cancel := context.WithCancel(o.ctx)
	session := &clientSession{ctx: ctx, cancel: cancel, closeTransport: func() error { return nil }}
	if o.quic {
		session.transport, session.closeTransport = o.newQUICTransport(session.ctx)
	} else {
		session.transport = &http2.Transport{
			DisableCompression: true,
			IdleConnTimeout:    TT.DefaultSessionTimeout,
			DialTLSContext: func(ctx context.Context, _, _ string, _ *stdTLS.Config) (net.Conn, error) {
				rawConn, err := o.dialSession(ctx, session.ctx, N.NetworkTCP)
				if err != nil {
					return nil, err
				}
				conn, err := aTLS.ClientHandshake(ctx, rawConn, o.tlsConfig)
				if err != nil {
					_ = rawConn.Close()
					return nil, err
				}
				if conn.ConnectionState().NegotiatedProtocol != http2.NextProtoTLS {
					_ = rawConn.Close()
					return nil, errors.New("TrustTunnel server did not negotiate h2")
				}
				return conn, nil
			},
		}
	}
	if o.health {
		session.healthDone = make(chan struct{})
		session.activity = make(chan struct{}, 1)
		go o.healthLoop(session)
	}
	o.session = session
	return session, nil
}

func (o *Outbound) healthLoop(session *clientSession) {
	defer close(session.healthDone)
	timer := time.NewTimer(TT.DefaultHealthCheckTimeout)
	defer timer.Stop()
	for {
		select {
		case <-session.ctx.Done():
			return
		case <-session.activity:
		case <-timer.C:
			ctx, cancel := context.WithTimeout(session.ctx, TT.DefaultHealthCheckTimeout)
			response, err := session.transport.RoundTrip(o.request(ctx, TT.HealthCheckMagicAddress, nil))
			if err == nil {
				_ = response.Body.Close()
			}
			cancel()
		}
		timer.Reset(TT.DefaultHealthCheckTimeout)
	}
}

func (o *Outbound) dialSession(ctx, session context.Context, network string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, TT.DefaultConnectionTimeout)
	stop := context.AfterFunc(session, cancel)
	defer stop()
	defer cancel()
	conn, err := o.dialer.DialContext(ctx, network, o.server)
	if err != nil {
		return nil, err
	}
	tracked := &sessionConn{Conn: conn}
	tracked.stop = context.AfterFunc(session, func() { _ = conn.Close() })
	if err := session.Err(); err != nil {
		_ = tracked.Close()
		return nil, err
	}
	return tracked, nil
}

type sessionConn struct {
	net.Conn
	stop func() bool
}

func (c *sessionConn) Close() error {
	c.stop()
	return c.Conn.Close()
}

func (c *sessionConn) Upstream() any { return c.Conn }

func (o *Outbound) request(ctx context.Context, host string, body io.ReadCloser) *http.Request {
	header := make(http.Header, 2)
	header.Set("Proxy-Authorization", o.authHeader)
	header.Set("User-Agent", "")
	return (&http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Scheme: "https", Host: o.server.String()},
		Header: header,
		Host:   host,
		Body:   body,
	}).WithContext(ctx)
}

func (o *Outbound) open(ctx context.Context, host string, destination M.Socksaddr) (*streamConn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	session, err := o.getSession()
	if err != nil {
		return nil, err
	}
	ctx, metadata := adapter.ExtendContext(ctx)
	metadata.Outbound = o.Tag()
	metadata.Destination = destination
	requestCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	stopSession := context.AfterFunc(session.ctx, cancel)
	stopCaller := context.AfterFunc(ctx, cancel)
	timeout := time.AfterFunc(TT.DefaultSessionTimeout, cancel)
	reader, writer := io.Pipe()
	response, err := session.transport.RoundTrip(o.request(requestCtx, host, reader))
	if !stopCaller() {
		cancel()
	}
	if !timeout.Stop() {
		cancel()
	}
	if err == nil && response.StatusCode != http.StatusOK {
		err = E.New("TrustTunnel CONNECT returned HTTP ", response.StatusCode)
	}
	if err == nil {
		err = requestCtx.Err()
	}
	if err != nil {
		stopSession()
		cancel()
		_ = writer.Close()
		_ = reader.Close()
		if response != nil {
			_ = response.Body.Close()
		}
		return nil, err
	}
	select {
	case session.activity <- struct{}{}:
	default:
	}
	return &streamConn{
		ctx: requestCtx, reader: response.Body, writer: writer, remote: destination,
		close: sync.OnceValue(func() error {
			stopSession()
			cancel()
			_ = writer.Close()
			return response.Body.Close()
		}),
	}, nil
}

func (o *Outbound) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	switch N.NetworkName(network) {
	case N.NetworkTCP:
		conn, err := o.open(ctx, destination.String(), destination)
		if err != nil {
			return nil, err
		}
		return conn, nil
	case N.NetworkUDP:
		conn, err := o.ListenPacket(ctx, destination)
		if err != nil {
			return nil, err
		}
		return bufio.NewBindPacketConn(conn, destination), nil
	default:
		return nil, E.Extend(N.ErrUnknownNetwork, network)
	}
}

func (o *Outbound) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	if !o.udp {
		return nil, errors.New("TrustTunnel outbound UDP is disabled")
	}
	stream, err := o.open(ctx, TT.UDPMagicAddress, destination)
	if err != nil {
		return nil, err
	}
	return &packetConn{stream: stream, dnsRouter: o.dnsRouter}, nil
}

type streamConn struct {
	ctx    context.Context
	reader io.Reader
	writer io.Writer
	remote M.Socksaddr
	close  func() error
}

func (c *streamConn) Read(p []byte) (int, error)       { return c.reader.Read(p) }
func (c *streamConn) Write(p []byte) (int, error)      { return c.writer.Write(p) }
func (c *streamConn) Close() error                     { return c.close() }
func (c *streamConn) LocalAddr() net.Addr              { return M.Socksaddr{} }
func (c *streamConn) RemoteAddr() net.Addr             { return c.remote }
func (c *streamConn) SetDeadline(time.Time) error      { return os.ErrInvalid }
func (c *streamConn) SetReadDeadline(time.Time) error  { return os.ErrInvalid }
func (c *streamConn) SetWriteDeadline(time.Time) error { return os.ErrInvalid }
