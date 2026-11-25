//go:build with_turn

package turn

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/dialer"
	"github.com/sagernet/sing-box/common/interrupt"
	"github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type Options struct {
	option.DialerOptions
	option.ServerOptions
	option.OutboundTLSOptionsContainer
	Username string `json:"username"`
	Password string `json:"password"`
}

func RegisterOutbound(registry *outbound.Registry) {
	outbound.Register[Options](registry, "turn", NewOutbound)
}

type Outbound struct {
	outbound.Adapter
	ctx      context.Context
	dialer   N.Dialer
	server   M.Socksaddr
	username string
	password string

	mu      sync.Mutex
	network context.Context
	cancel  context.CancelFunc
	active  *interrupt.Group
}

func NewOutbound(ctx context.Context, _ adapter.Router, logger log.ContextLogger, tag string, options Options) (adapter.Outbound, error) {
	if options.Server == "" || options.Username == "" || options.Password == "" {
		return nil, errors.New("TURN requires server, username and password")
	}
	if options.TLS == nil || !options.TLS.Enabled || options.TLS.Insecure {
		return nil, errors.New("TURN requires verified TLS")
	}
	if options.ServerPort == 0 {
		options.ServerPort = 5349
	}
	base, err := dialer.New(ctx, options.DialerOptions, options.ServerIsDomain())
	if err != nil {
		return nil, err
	}
	secure, err := tls.NewDialerFromOptions(ctx, logger, base, options.Server, *options.TLS)
	if err != nil {
		return nil, err
	}
	network, cancel := context.WithCancel(ctx)
	return &Outbound{
		Adapter:  outbound.NewAdapterWithDialerOptions("turn", tag, []string{N.NetworkUDP}, options.DialerOptions),
		ctx:      ctx,
		dialer:   secure,
		server:   options.ServerOptions.Build(),
		username: options.Username,
		password: options.Password,
		network:  network,
		cancel:   cancel,
		active:   interrupt.NewGroup(),
	}, nil
}

func (o *Outbound) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	if N.NetworkName(network) != N.NetworkUDP {
		return nil, errors.New("TURN only supports UDP")
	}
	destination = destination.Unwrap()
	if !destination.IsIP() || destination.Port == 0 || destination.Addr.IsUnspecified() {
		return nil, errors.New("TURN requires a numeric peer IP and port")
	}
	conn, err := o.ListenPacket(ctx, destination)
	if err != nil {
		return nil, err
	}
	return &connectedPacket{PacketConn: conn, peer: destination.UDPAddr()}, nil
}

func (o *Outbound) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	if destination.IsDomain() {
		return nil, errors.New("TURN requires a numeric peer IP")
	}
	o.mu.Lock()
	network := o.network
	o.mu.Unlock()
	if err := network.Err(); err != nil {
		return nil, err
	}
	setup, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	stop := context.AfterFunc(network, cancel)
	defer stop()
	stream, err := o.dialer.DialContext(setup, N.NetworkTCP, o.server)
	if err != nil {
		return nil, err
	}
	packet, err := allocate(setup, stream, o.username, o.password, destination.Unwrap().IsIPv6())
	if err != nil {
		return nil, err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if err := network.Err(); err != nil {
		packet.Close()
		return nil, err
	}
	return o.active.NewPacketConn(packet, false), nil
}

func (o *Outbound) InterfaceUpdated(context.Context) { o.reset(true) }

func (o *Outbound) Close() error {
	o.reset(false)
	return nil
}

func (o *Outbound) reset(reopen bool) {
	o.mu.Lock()
	active := o.active
	if o.network.Err() == nil && reopen {
		o.cancel()
		o.network, o.cancel = context.WithCancel(o.ctx)
		o.active = interrupt.NewGroup()
	} else {
		o.cancel()
	}
	o.mu.Unlock()
	active.Interrupt(true)
}

type connectedPacket struct {
	net.PacketConn
	peer *net.UDPAddr
}

func (c *connectedPacket) Read(b []byte) (int, error) {
	for {
		n, from, err := c.ReadFrom(b)
		if err != nil || M.SocksaddrFromNet(from).Unwrap() == M.SocksaddrFromNet(c.peer).Unwrap() {
			return n, err
		}
	}
}

func (c *connectedPacket) Write(b []byte) (int, error) { return c.WriteTo(b, c.peer) }
func (c *connectedPacket) RemoteAddr() net.Addr        { return c.peer }
