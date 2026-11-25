//go:build with_turnrelay

package turnrelay

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/dialer"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/turnrelay/internal/upstream/credpool"
	"github.com/sagernet/sing-box/protocol/turnrelay/internal/upstream/exit"
	"github.com/sagernet/sing-box/protocol/turnrelay/internal/upstream/mux"
	"github.com/sagernet/sing-box/protocol/turnrelay/internal/upstream/obfs"
	"github.com/sagernet/sing-box/protocol/turnrelay/internal/upstream/provider/static"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"
)

var (
	_ adapter.Lifecycle               = (*Outbound)(nil)
	_ adapter.Outbound                = (*Outbound)(nil)
	_ adapter.InterfaceUpdateListener = (*Outbound)(nil)
)

func RegisterOutbound(registry *outbound.Registry) {
	outbound.Register[Options](registry, Type, NewOutbound)
}

type Outbound struct {
	outbound.Adapter
	ctx     context.Context
	pool    *mux.Pool
	options mux.Options
	peer    *net.UDPAddr
	logf    func(string, ...any)
	mu      sync.Mutex
	client  *exit.Client
	started bool
	closed  bool
	cancel  context.CancelFunc
	watcher sync.WaitGroup
}

func NewOutbound(ctx context.Context, _ adapter.Router, logger log.ContextLogger, tag string, options Options) (adapter.Outbound, error) {
	cfg, err := options.build()
	if err != nil {
		return nil, err
	}
	hook, err := dialHook(ctx, options.DialerOptions)
	if err != nil {
		return nil, err
	}
	wrapper, err := obfs.New(obfs.Mode(cfg.mode), obfs.Options{ServerFingerprint: cfg.fingerprint})
	if err != nil {
		return nil, err
	}
	logf := func(string, ...any) {}
	if logger != nil {
		logf = func(format string, args ...any) {
			logger.Debug(fmt.Sprintf(format, args...))
		}
	}
	creds := credpool.New(static.New(cfg.turnServer, cfg.turnUsername, cfg.turnPassword), credpool.Options{
		Links:  []string{"static"},
		TTL:    8 * time.Hour,
		Margin: 30 * time.Minute,
		Logf:   logf,
	})
	peer := net.UDPAddrFromAddrPort(cfg.server)
	poolOptions := mux.Options{
		ProbeInterval: 3 * time.Second,
		ZombieAfter:   12 * time.Second,
		Workers:       cfg.connections,
		Peer:          peer,
		Wrapper:       wrapper,
		Creds:         creds,
		TURNUDP:       cfg.udp,
		Logf:          logf,
		DialContext:   hook,
		Password:      cfg.password,
	}
	return &Outbound{
		Adapter: outbound.NewAdapterWithDialerOptions(Type, tag, []string{N.NetworkTCP, N.NetworkUDP}, options.DialerOptions),
		ctx:     ctx,
		options: poolOptions,
		peer:    peer,
		logf:    logf,
	}, nil
}

func dialHook(ctx context.Context, options option.DialerOptions) (func(context.Context, string, string) (net.Conn, error), error) {
	if service.FromContext[adapter.NetworkManager](ctx) == nil {
		if options.Detour != "" {
			return nil, errors.New("turnrelay: detour requires a running sing-box instance")
		}
		return nil, nil
	}
	sbDialer, err := dialer.New(ctx, options, true)
	if err != nil {
		return nil, err
	}
	return func(dialCtx context.Context, network string, address string) (net.Conn, error) {
		return sbDialer.DialContext(dialCtx, network, M.ParseSocksaddr(address))
	}, nil
}

func (o *Outbound) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	if stage != adapter.StartStateStart {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed || o.started {
		return nil
	}
	lifetime := o.ctx
	if scope != nil {
		lifetime = scope.Context()
		scope.Add(o.Close)
	}
	lifetime, o.cancel = context.WithCancel(lifetime)
	o.startSession(lifetime)
	o.started = true
	o.watcher.Go(func() { o.watchSession(lifetime) })
	return nil
}

func (o *Outbound) startSession(ctx context.Context) {
	o.pool = mux.New(o.options)
	o.pool.Start(ctx)
	o.client = exit.NewClient(newRelayPipe(o.pool, o.peer), o.peer, exit.ClientOptions{Logf: o.logf})
}

func (o *Outbound) watchSession(ctx context.Context) {
	ticker := time.NewTicker(o.options.ProbeInterval)
	defer ticker.Stop()
	var lastReply time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			o.mu.Lock()
			if o.closed {
				o.mu.Unlock()
				return
			}
			if reply := o.pool.LastProbeReply(); reply.After(lastReply) {
				lastReply = reply
			} else if lastReply.IsZero() && o.pool.Stats().Active > 0 {
				lastReply = now
			}
			if lastReply.IsZero() || now.Sub(lastReply) < o.options.ZombieAfter {
				o.mu.Unlock()
				continue
			}
			// Protocol: new workers sharing the old session ID can compete with stale workers on the exit for replies.
			oldPool, oldClient := o.pool, o.client
			o.logf("turnrelay: no exit probe replies; rebuilding the session")
			o.startSession(ctx)
			o.mu.Unlock()
			oldPool.Close()
			_ = oldClient.Close()
			lastReply = time.Time{}
		}
	}
}

func (o *Outbound) InterfaceUpdated(context.Context) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.started && !o.closed {
		o.pool.Handover("interface updated")
	}
}

func (o *Outbound) Close() error {
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return nil
	}
	o.closed = true
	pool, client := o.pool, o.client
	o.client = nil
	if o.cancel != nil {
		o.cancel()
	}
	o.mu.Unlock()
	if client != nil {
		pool.Close()
		_ = client.Close()
	}
	o.watcher.Wait()
	return nil
}

func (o *Outbound) exitClient() (*exit.Client, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return nil, net.ErrClosed
	}
	if o.client == nil {
		return nil, errors.New("turnrelay: outbound is not started")
	}
	return o.client, nil
}

func (o *Outbound) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	client, err := o.exitClient()
	if err != nil {
		return nil, err
	}
	switch N.NetworkName(network) {
	case N.NetworkTCP:
		return client.DialContext(ctx, network, destination)
	case N.NetworkUDP:
		if !destination.IsIP() || destination.Port == 0 || destination.Addr.IsUnspecified() {
			return nil, errors.New("turnrelay: udp dial requires a numeric peer IP and port")
		}
		packet, err := client.ListenPacket(ctx, destination)
		if err != nil {
			return nil, err
		}
		return &connectedPacket{PacketConn: packet, peer: destination}, nil
	default:
		return nil, errors.New("turnrelay: unsupported network " + network)
	}
}

func (o *Outbound) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	client, err := o.exitClient()
	if err != nil {
		return nil, err
	}
	return client.ListenPacket(ctx, destination)
}

type connectedPacket struct {
	net.PacketConn
	peer M.Socksaddr
}

func (c *connectedPacket) Read(b []byte) (int, error) {
	for {
		n, from, err := c.ReadFrom(b)
		if err != nil || M.SocksaddrFromNet(from).Unwrap() == c.peer.Unwrap() {
			return n, err
		}
	}
}

func (c *connectedPacket) Write(b []byte) (int, error) { return c.WriteTo(b, c.peer) }
func (c *connectedPacket) RemoteAddr() net.Addr        { return c.peer }
