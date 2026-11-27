//go:build with_mieru

package mieru

import (
	"context"
	"net"
	"sync"

	mieruclient "github.com/enfein/mieru/v3/apis/client"
	mierucommon "github.com/enfein/mieru/v3/apis/common"
	mierumodel "github.com/enfein/mieru/v3/apis/model"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/dialer"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/bufio"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"
	"google.golang.org/protobuf/proto"
)

var _ adapter.Lifecycle = (*Outbound)(nil)

func RegisterOutbound(registry *outbound.Registry) {
	outbound.Register[option.MieruOutboundOptions](registry, Type, NewOutbound)
}

type Outbound struct {
	outbound.Adapter
	logger log.ContextLogger
	client mieruclient.Client

	access  sync.Mutex
	started bool
	closed  bool
}

func NewOutbound(ctx context.Context, _ adapter.Router, logger log.ContextLogger, tag string, options option.MieruOutboundOptions) (adapter.Outbound, error) {
	config, err := buildClientConfig(ctx, options)
	if err != nil {
		return nil, err
	}
	client := mieruclient.NewClient()
	if err := client.Store(config); err != nil {
		return nil, E.Cause(err, "mieru: store outbound config")
	}
	return &Outbound{
		Adapter: outbound.NewAdapterWithDialerOptions(Type, tag, []string{N.NetworkTCP, N.NetworkUDP}, options.DialerOptions),
		logger:  logger,
		client:  client,
	}, nil
}

func buildClientConfig(ctx context.Context, options option.MieruOutboundOptions) (*mieruclient.ClientConfig, error) {
	if options.Server == "" {
		return nil, E.New("mieru: missing server")
	}
	if options.UserName == "" {
		return nil, E.New("mieru: missing username")
	}
	if options.Password == "" {
		return nil, E.New("mieru: missing password")
	}
	portBindings, err := buildPortBindings(options.PortBindings, options.ServerPort, options.PortRange, options.Transport)
	if err != nil {
		return nil, err
	}
	handshake, err := handshakeMode(options.HandshakeMode)
	if err != nil {
		return nil, err
	}
	multiplexing, err := multiplexingLevel(options.Multiplexing)
	if err != nil {
		return nil, err
	}
	server := &appctlpb.ServerEndpoint{PortBindings: portBindings}
	if options.ServerIsDomain() {
		server.DomainName = proto.String(options.Server)
	} else {
		server.IpAddress = proto.String(options.Server)
	}
	profile := &appctlpb.ClientProfile{
		ProfileName: proto.String("sing-box"),
		User: &appctlpb.User{
			Name:     proto.String(options.UserName),
			Password: proto.String(options.Password),
		},
		Servers: []*appctlpb.ServerEndpoint{server},
		Multiplexing: &appctlpb.MultiplexingConfig{
			Level: multiplexing.Enum(),
		},
		HandshakeMode: handshake.Enum(),
	}
	baseDialer, err := dialer.New(ctx, options.DialerOptions, options.ServerIsDomain())
	if err != nil {
		return nil, err
	}
	dialerAdapter := &mieruDialer{dialer: baseDialer}
	config := &mieruclient.ClientConfig{
		Profile:      profile,
		Dialer:       dialerAdapter,
		PacketDialer: dialerAdapter,
		DNSConfig: &mierucommon.ClientDNSConfig{
			BypassDialerDNS: true,
		},
	}
	if options.ServerIsDomain() {
		config.Resolver = newDNSResolver(ctx, baseDialer)
	}
	return config, nil
}

func (o *Outbound) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	if stage == adapter.StartStateInitialize {
		if scope != nil {
			scope.Add(o.Close)
		}
		return nil
	}
	if stage != adapter.StartStateStart {
		return nil
	}
	o.access.Lock()
	defer o.access.Unlock()
	if o.closed {
		return net.ErrClosed
	}
	if o.started {
		return nil
	}
	if err := o.client.Start(); err != nil {
		return E.Cause(err, "mieru: start outbound")
	}
	o.started = true
	o.logger.Info("mieru outbound started")
	return nil
}

func (o *Outbound) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	if err := o.checkRunning(); err != nil {
		return nil, err
	}
	switch N.NetworkName(network) {
	case N.NetworkTCP:
		o.logger.InfoContext(ctx, "mieru outbound TCP connection to ", destination)
		destinationSpec, err := buildNetAddrSpec(destination, "tcp")
		if err != nil {
			return nil, E.Cause(err, "mieru: invalid destination")
		}
		return o.client.DialContext(ctx, destinationSpec)
	case N.NetworkUDP:
		o.logger.InfoContext(ctx, "mieru outbound UDP connection to ", destination)
		packetConn, err := o.listenPacket(ctx, destination)
		if err != nil {
			return nil, err
		}
		return bufio.NewBindPacketConn(packetConn, destination), nil
	default:
		return nil, E.New("mieru: unsupported network ", network)
	}
}

func (o *Outbound) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	if err := o.checkRunning(); err != nil {
		return nil, err
	}
	o.logger.InfoContext(ctx, "mieru outbound UDP connection to ", destination)
	return o.listenPacket(ctx, destination)
}

func (o *Outbound) listenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	destinationSpec, err := buildNetAddrSpec(destination, "udp")
	if err != nil {
		return nil, E.Cause(err, "mieru: invalid destination")
	}
	streamConn, err := o.client.DialContext(ctx, destinationSpec)
	if err != nil {
		return nil, err
	}
	return mierucommon.NewUDPAssociateWrapper(mierucommon.NewPacketOverStreamTunnel(streamConn)), nil
}

func (o *Outbound) Close() error {
	o.access.Lock()
	if o.closed {
		o.access.Unlock()
		return nil
	}
	o.closed = true
	started := o.started
	o.started = false
	o.access.Unlock()
	if !started {
		return nil
	}
	if err := o.client.Stop(); err != nil {
		return E.Cause(err, "mieru: stop outbound")
	}
	return nil
}

func (o *Outbound) checkRunning() error {
	o.access.Lock()
	defer o.access.Unlock()
	if o.closed {
		return net.ErrClosed
	}
	if !o.started {
		return E.New("mieru: outbound is not started")
	}
	return nil
}

type mieruDialer struct {
	dialer N.Dialer
}

var (
	_ mierucommon.Dialer       = (*mieruDialer)(nil)
	_ mierucommon.PacketDialer = (*mieruDialer)(nil)
)

func (d *mieruDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return d.dialer.DialContext(ctx, network, M.ParseSocksaddr(address))
}

func (d *mieruDialer) ListenPacket(ctx context.Context, network, laddr, raddr string) (net.PacketConn, error) {
	return d.dialer.ListenPacket(ctx, M.ParseSocksaddr(raddr))
}

type dnsResolver struct {
	router  adapter.DNSRouter
	options adapter.DNSQueryOptions
}

var _ mierucommon.DNSResolver = (*dnsResolver)(nil)

func newDNSResolver(ctx context.Context, baseDialer N.Dialer) mierucommon.DNSResolver {
	router := service.FromContext[adapter.DNSRouter](ctx)
	if router == nil {
		return nil
	}
	resolver := &dnsResolver{router: router}
	if resolveDialer, isResolveDialer := baseDialer.(dialer.ResolveDialer); isResolveDialer {
		resolver.options = resolveDialer.QueryOptions()
	}
	return resolver
}

func (r *dnsResolver) LookupIP(ctx context.Context, network, host string) ([]net.IP, error) {
	addresses, err := r.router.Lookup(ctx, host, r.options)
	if err != nil {
		return nil, err
	}
	result := make([]net.IP, 0, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if network == "ip4" && !address.Is4() || network == "ip6" && !address.Is6() {
			continue
		}
		result = append(result, address.AsSlice())
	}
	if len(result) == 0 {
		return nil, E.New("mieru: no ", network, " address for ", host)
	}
	return result, nil
}

func buildNetAddrSpec(destination M.Socksaddr, network string) (mierumodel.NetAddrSpec, error) {
	var destinationSpec mierumodel.NetAddrSpec
	if !destination.IsValid() {
		return destinationSpec, E.New("invalid destination")
	}
	if err := destinationSpec.From(destination); err != nil {
		return destinationSpec, err
	}
	destinationSpec.Net = network
	return destinationSpec, nil
}
