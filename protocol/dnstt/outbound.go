package dnstt

import (
	"context"
	"net"
	"os"
	"sync"
	"time"

	"github.com/net2share/vaydns/client"
	"github.com/net2share/vaydns/dns"
	"github.com/net2share/vaydns/noise"
	"github.com/net2share/vaydns/turbotunnel"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/dialer"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/xtaci/smux"
)

var _ adapter.InterfaceUpdateListener = (*Outbound)(nil)

const Type = "dnstt"

var wireConfig = turbotunnel.WireConfig{ClientIDSize: 8, Compat: true}

func RegisterOutbound(registry *outbound.Registry) {
	outbound.Register[option.DNSTTOutboundOptions](registry, Type, NewOutbound)
}

type Outbound struct {
	outbound.Adapter
	ctx       context.Context
	dialer    N.Dialer
	resolver  M.Socksaddr
	domain    dns.Name
	publicKey []byte
	mtu       int

	access  sync.Mutex
	closed  bool
	session *smux.Session
	pending *sessionAttempt
}

type sessionAttempt struct {
	done    chan struct{}
	cancel  context.CancelFunc
	session *smux.Session
	err     error
}

func NewOutbound(ctx context.Context, _ adapter.Router, _ log.ContextLogger, tag string, options option.DNSTTOutboundOptions) (adapter.Outbound, error) {
	resolver := options.ServerOptions.Build()
	if !resolver.IsValid() {
		return nil, E.New("missing DNSTT resolver server")
	}
	if resolver.Port == 0 {
		resolver.Port = 53
	}
	domain, err := dns.ParseName(options.Domain)
	if err != nil {
		return nil, E.Cause(err, "invalid DNSTT domain")
	}
	if len(domain) == 0 {
		return nil, E.New("missing DNSTT domain")
	}
	mtu := client.DNSNameCapacity(domain, 253, 0) - wireConfig.DataOverhead()
	if mtu < 50 {
		return nil, E.New("DNSTT domain is too long to carry KCP packets")
	}
	publicKey, err := noise.DecodeKey(options.PublicKey)
	if err != nil {
		return nil, E.Cause(err, "invalid DNSTT public_key")
	}
	resolverDialer, err := dialer.New(ctx, options.DialerOptions, resolver.IsDomain())
	if err != nil {
		return nil, err
	}
	return &Outbound{
		Adapter:   outbound.NewAdapterWithDialerOptions(Type, tag, []string{N.NetworkTCP}, options.DialerOptions),
		ctx:       ctx,
		dialer:    resolverDialer,
		resolver:  resolver,
		domain:    domain,
		publicKey: publicKey,
		mtu:       mtu,
	}, nil
}

func (o *Outbound) DialContext(ctx context.Context, network string, _ M.Socksaddr) (net.Conn, error) {
	if N.NetworkName(network) != N.NetworkTCP {
		return nil, E.New("DNSTT only supports TCP")
	}
	session, err := o.getSession(ctx)
	if err != nil {
		return nil, err
	}
	type result struct {
		stream *smux.Stream
		err    error
	}
	opened := make(chan result)
	go func() {
		stream, err := session.OpenStream()
		if err != nil {
			session.Close()
		}
		select {
		case opened <- result{stream, err}:
		case <-ctx.Done():
			if stream != nil {
				stream.Close()
			}
		}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-opened:
		if result.err != nil {
			return nil, E.Cause(result.err, "open DNSTT stream")
		}
		return result.stream, nil
	}
}

func (o *Outbound) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	return nil, E.New("DNSTT only supports TCP")
}

func (o *Outbound) getSession(ctx context.Context) (*smux.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	o.access.Lock()
	if o.closed {
		o.access.Unlock()
		return nil, os.ErrClosed
	}
	if session := o.session; session != nil && !session.IsClosed() {
		o.access.Unlock()
		return session, nil
	}
	attempt := o.pending
	if attempt == nil {
		setupCtx, cancel := context.WithTimeout(o.ctx, 30*time.Second)
		attempt = &sessionAttempt{done: make(chan struct{}), cancel: cancel}
		o.pending = attempt
		go o.startSession(setupCtx, attempt)
	}
	o.access.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-attempt.done:
		return attempt.session, attempt.err
	}
}

func (o *Outbound) startSession(ctx context.Context, attempt *sessionAttempt) {
	defer attempt.cancel()
	session, err := o.newSession(ctx)
	o.access.Lock()
	if o.pending != attempt {
		o.access.Unlock()
		if session != nil {
			session.Close()
		}
		attempt.err = context.Canceled
	} else {
		o.pending = nil
		o.session = session
		o.access.Unlock()
		attempt.session, attempt.err = session, err
	}
	close(attempt.done)
}

func (o *Outbound) InterfaceUpdated(context.Context) {
	o.reset(false)
}

func (o *Outbound) Close() error {
	o.reset(true)
	return nil
}

func (o *Outbound) reset(closeOutbound bool) {
	o.access.Lock()
	if closeOutbound {
		o.closed = true
	}
	session, attempt := o.session, o.pending
	o.session, o.pending = nil, nil
	o.access.Unlock()
	if attempt != nil {
		attempt.cancel()
	}
	if session != nil {
		session.Close()
	}
}
