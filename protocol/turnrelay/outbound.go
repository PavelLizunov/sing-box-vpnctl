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
	options mux.Options
	peer    *net.UDPAddr
	logf    func(string, ...any)
	logger  log.ContextLogger
	mu      sync.Mutex
	current *sessionGeneration
	ready   chan struct{}
	updates chan struct{}
	started bool
	closed  bool
	cancel  context.CancelFunc
	watcher sync.WaitGroup
}

type sessionGeneration struct {
	pool    *mux.Pool
	client  *exit.Client
	created time.Time
	retired chan struct{}
}

func (g *sessionGeneration) close() {
	_ = g.client.Close()
	g.pool.Close()
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
		logger:  logger,
		updates: make(chan struct{}, 1),
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
	current := o.startSession(lifetime)
	o.current = current
	o.started = true
	o.watcher.Go(func() { o.watchSession(lifetime, current) })
	return nil
}

func (o *Outbound) startSession(ctx context.Context) *sessionGeneration {
	options := o.options
	options.OnWake = func() { o.InterfaceUpdated(ctx) }
	pool := mux.New(options)
	g := &sessionGeneration{
		pool: pool, created: time.Now(), retired: make(chan struct{}),
		client: exit.NewClient(newRelayPipe(pool, o.peer), o.peer, exit.ClientOptions{Logf: o.logf}),
	}
	pool.Start(ctx)
	return g
}

func (o *Outbound) watchSession(ctx context.Context, current *sessionGeneration) {
	ticker := time.NewTicker(o.options.ProbeInterval)
	defer ticker.Stop()
	defer func() {
		o.mu.Lock()
		o.closed = true
		if o.current != nil {
			close(o.current.retired)
			o.current = nil
		}
		if o.ready != nil {
			close(o.ready)
			o.ready = nil
		}
		o.mu.Unlock()
		if current != nil {
			current.close()
		}
	}()

	lastPacket := current.created
	var received uint64
	for {
		reason := "no incoming exit traffic"
		select {
		case <-ctx.Done():
			return
		case <-o.updates:
			reason = "interface updated"
			timer := time.NewTimer(150 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			select {
			case <-o.updates:
			default:
			}
		case <-ticker.C:
			now := time.Now()
			if count := current.pool.Received(); count != received {
				received = count
				lastPacket = now
			}
			if now.Sub(lastPacket) < o.options.ZombieAfter {
				continue
			}
		}

		o.mu.Lock()
		close(current.retired)
		o.current = nil
		o.ready = make(chan struct{})
		o.mu.Unlock()
		if o.logger != nil {
			o.logger.Info("turnrelay: ", reason, "; rebuilding the session")
		}
		current.close()
		current = nil

		o.mu.Lock()
		if ctx.Err() != nil || o.closed {
			o.mu.Unlock()
			return
		}
		current = o.startSession(ctx)
		o.current = current
		close(o.ready)
		o.ready = nil
		o.mu.Unlock()
		received, lastPacket = 0, current.created
	}
}

func (o *Outbound) InterfaceUpdated(context.Context) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.started && !o.closed {
		select {
		case o.updates <- struct{}{}:
		default:
		}
	}
}

func (o *Outbound) Close() error {
	o.mu.Lock()
	o.closed = true
	if o.cancel != nil {
		o.cancel()
	}
	o.mu.Unlock()
	o.watcher.Wait()
	return nil
}

func (o *Outbound) generation(ctx context.Context) (*sessionGeneration, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		o.mu.Lock()
		current, ready, started, closed := o.current, o.ready, o.started, o.closed
		o.mu.Unlock()
		switch {
		case closed:
			return nil, net.ErrClosed
		case !started:
			return nil, errors.New("turnrelay: outbound is not started")
		case current != nil:
			return current, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ready:
		}
	}
}

func (o *Outbound) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	switch N.NetworkName(network) {
	case N.NetworkTCP:
		for {
			current, err := o.generation(ctx)
			if err != nil {
				return nil, err
			}
			conn, err := current.client.DialContext(ctx, network, destination)
			if !current.isRetired() {
				return conn, err
			}
			if conn != nil {
				_ = conn.Close()
			}
		}
	case N.NetworkUDP:
		if !destination.IsIP() || destination.Port == 0 || destination.Addr.IsUnspecified() {
			return nil, errors.New("turnrelay: udp dial requires a numeric peer IP and port")
		}
		packet, err := o.ListenPacket(ctx, destination)
		if err != nil {
			return nil, err
		}
		return &connectedPacket{PacketConn: packet, peer: destination}, nil
	default:
		return nil, errors.New("turnrelay: unsupported network " + network)
	}
}

func (g *sessionGeneration) isRetired() bool {
	select {
	case <-g.retired:
		return true
	default:
		return false
	}
}

func (o *Outbound) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	for {
		current, err := o.generation(ctx)
		if err != nil {
			return nil, err
		}
		packet, err := current.client.ListenPacket(ctx, destination)
		if !current.isRetired() {
			return packet, err
		}
		if packet != nil {
			_ = packet.Close()
		}
	}
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
