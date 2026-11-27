//go:build with_mieru

package mieru

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"
	"time"

	mierucommon "github.com/enfein/mieru/v3/apis/common"
	"github.com/enfein/mieru/v3/apis/constant"
	"github.com/enfein/mieru/v3/apis/model"
	mieruserver "github.com/enfein/mieru/v3/apis/server"
	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/common/listener"
	"github.com/sagernet/sing-box/common/uot"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/control"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"
	"google.golang.org/protobuf/proto"

	"github.com/database64128/tfo-go/v2"
)

const (
	acceptErrorBackoffInitial = 100 * time.Millisecond
	acceptErrorBackoffMax     = 5 * time.Second
)

var _ adapter.Lifecycle = (*Inbound)(nil)

func RegisterInbound(registry *inbound.Registry) {
	inbound.Register[option.MieruInboundOptions](registry, Type, NewInbound)
}

type Inbound struct {
	inbound.Adapter
	ctx    context.Context
	cancel context.CancelFunc
	router adapter.ConnectionRouterEx
	logger log.ContextLogger
	server mieruserver.Server

	access  sync.Mutex
	started bool
	closed  bool
	conns   map[net.Conn]struct{}
	tasks   sync.WaitGroup
}

func NewInbound(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.MieruInboundOptions) (adapter.Inbound, error) {
	config, err := buildServerConfig(options)
	if err != nil {
		return nil, err
	}
	factory := &listenerFactory{ctx: ctx, options: options.ListenOptions}
	serverConfig := &mieruserver.ServerConfig{
		Config:                config,
		StreamListenerFactory: factory,
		PacketListenerFactory: factory,
	}
	server := mieruserver.NewServer()
	if err := server.Store(serverConfig); err != nil {
		return nil, E.Cause(err, "mieru: invalid inbound config")
	}
	lifetime, cancel := context.WithCancel(ctx)
	return &Inbound{
		Adapter: inbound.NewAdapter(Type, tag),
		ctx:     lifetime,
		cancel:  cancel,
		router:  uot.NewRouter(router, logger),
		logger:  logger,
		server:  server,
		conns:   make(map[net.Conn]struct{}),
	}, nil
}

func buildServerConfig(options option.MieruInboundOptions) (*appctlpb.ServerConfig, error) {
	if len(options.Users) == 0 {
		return nil, E.New("mieru: inbound requires at least one user")
	}
	users := make([]*appctlpb.User, 0, len(options.Users))
	seen := make(map[string]struct{}, len(options.Users))
	for index, user := range options.Users {
		if user.UserName == "" {
			return nil, fmt.Errorf("mieru: inbound users[%d] has an empty username", index)
		}
		if user.Password == "" {
			return nil, fmt.Errorf("mieru: inbound users[%d] has an empty password", index)
		}
		if _, loaded := seen[user.UserName]; loaded {
			return nil, fmt.Errorf("mieru: inbound user %q is duplicated", user.UserName)
		}
		seen[user.UserName] = struct{}{}
		users = append(users, &appctlpb.User{
			Name:     proto.String(user.UserName),
			Password: proto.String(user.Password),
		})
	}
	portBindings, err := buildPortBindings(options.PortBindings, options.ListenPort, options.PortRange, options.Transport)
	if err != nil {
		return nil, err
	}
	config := &appctlpb.ServerConfig{
		PortBindings: portBindings,
		Users:        users,
	}
	if options.Listen != nil {
		listenAddress := netip.Addr(*options.Listen)
		if listenAddress.IsValid() {
			config.ListenIPAddress = proto.String(listenAddress.String())
		}
	}
	return config, nil
}

func (o *Inbound) Start(stage adapter.StartStage, scope *adapter.Scope) error {
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
	if err := o.server.Start(); err != nil {
		return E.Cause(err, "mieru: start inbound")
	}
	o.started = true
	o.tasks.Add(1)
	go o.acceptLoop()
	o.logger.Info("mieru inbound started")
	return nil
}

func (o *Inbound) acceptLoop() {
	defer o.tasks.Done()
	backoff := acceptErrorBackoffInitial
	for {
		conn, request, err := o.server.Accept()
		if err != nil {
			if o.ctx.Err() != nil || !o.server.IsRunning() {
				return
			}
			o.logger.Debug("mieru: accept failed: ", err)
			timer := time.NewTimer(backoff)
			select {
			case <-timer.C:
			case <-o.ctx.Done():
				timer.Stop()
				return
			}
			backoff = min(backoff*2, acceptErrorBackoffMax)
			continue
		}
		backoff = acceptErrorBackoffInitial
		if !o.trackConnection(conn) {
			_ = conn.Close()
			return
		}
		o.tasks.Add(1)
		go o.handleConnection(conn, request)
	}
}

func (o *Inbound) handleConnection(conn net.Conn, request *model.Request) {
	defer o.tasks.Done()
	ctx := log.ContextWithNewID(o.ctx)
	metadata := adapter.InboundContext{
		Inbound:     o.Tag(),
		InboundType: o.Type(),
	}
	if remoteAddr := conn.RemoteAddr(); remoteAddr != nil {
		metadata.Source = M.SocksaddrFromNet(remoteAddr)
	}
	switch request.Command {
	case constant.Socks5ConnectCmd:
		metadata.Network = N.NetworkTCP
	case constant.Socks5UDPAssociateCmd:
		metadata.Network = N.NetworkUDP
	default:
		o.untrackConnection(conn)
		_ = conn.Close()
		o.logger.WarnContext(ctx, "mieru: unsupported command ", request.Command)
		return
	}
	metadata.Destination = socksaddrFromAddrSpec(request.DstAddr)
	if userContext, isUserContext := conn.(mierucommon.UserContext); isUserContext {
		metadata.User = userContext.UserName()
	}

	response := &model.Response{
		Reply: constant.Socks5ReplySuccess,
		BindAddr: model.AddrSpec{
			IP:   net.IPv4zero,
			Port: 0,
		},
	}
	if err := response.WriteToSocks5(conn); err != nil {
		o.untrackConnection(conn)
		_ = conn.Close()
		o.logger.DebugContext(ctx, "mieru: write handshake response: ", err)
		return
	}

	onClose := func(error) {
		o.untrackConnection(conn)
	}
	switch request.Command {
	case constant.Socks5ConnectCmd:
		if metadata.User != "" {
			o.logger.InfoContext(ctx, "[", metadata.User, "] mieru inbound TCP connection from ", metadata.Source, " to ", metadata.Destination)
		} else {
			o.logger.InfoContext(ctx, "mieru inbound TCP connection from ", metadata.Source, " to ", metadata.Destination)
		}
		o.router.RouteConnectionEx(ctx, conn, metadata, onClose)
	case constant.Socks5UDPAssociateCmd:
		if metadata.User != "" {
			o.logger.InfoContext(ctx, "[", metadata.User, "] mieru inbound UDP connection from ", metadata.Source, " to ", metadata.Destination)
		} else {
			o.logger.InfoContext(ctx, "mieru inbound UDP connection from ", metadata.Source, " to ", metadata.Destination)
		}
		o.router.RoutePacketConnectionEx(ctx, &packetConn{PacketConn: mierucommon.NewPacketOverStreamTunnel(conn)}, metadata, onClose)
	}
}

func (o *Inbound) Close() error {
	o.access.Lock()
	if o.closed {
		o.access.Unlock()
		return nil
	}
	o.closed = true
	started := o.started
	o.cancel()
	conns := make([]net.Conn, 0, len(o.conns))
	for conn := range o.conns {
		conns = append(conns, conn)
	}
	clear(o.conns)
	o.access.Unlock()

	for _, conn := range conns {
		_ = conn.Close()
	}
	var err error
	if started {
		err = o.server.Stop()
	}
	o.tasks.Wait()
	return err
}

func (o *Inbound) trackConnection(conn net.Conn) bool {
	o.access.Lock()
	defer o.access.Unlock()
	if o.closed {
		return false
	}
	o.conns[conn] = struct{}{}
	return true
}

func (o *Inbound) untrackConnection(conn net.Conn) {
	o.access.Lock()
	defer o.access.Unlock()
	delete(o.conns, conn)
}

func socksaddrFromAddrSpec(spec model.AddrSpec) M.Socksaddr {
	if spec.FQDN != "" {
		return M.Socksaddr{
			Fqdn: spec.FQDN,
			Port: uint16(spec.Port),
		}
	}
	if spec.IP != nil {
		address, loaded := netip.AddrFromSlice(spec.IP)
		if loaded {
			return M.Socksaddr{
				Addr: address.Unmap(),
				Port: uint16(spec.Port),
			}
		}
	}
	return M.Socksaddr{}
}

type packetConn struct {
	net.PacketConn
}

var _ N.PacketConn = (*packetConn)(nil)

func (c *packetConn) ReadPacket(buffer *buf.Buffer) (M.Socksaddr, error) {
	n, _, err := c.PacketConn.ReadFrom(buffer.FreeBytes())
	if err != nil {
		return M.Socksaddr{}, err
	}
	buffer.Truncate(n)
	if buffer.Len() < 3 {
		return M.Socksaddr{}, io.ErrShortBuffer
	}
	buffer.Advance(3)
	var address model.AddrSpec
	if err := address.ReadFromSocks5(buffer); err != nil {
		return M.Socksaddr{}, err
	}
	return socksaddrFromAddrSpec(address), nil
}

func (c *packetConn) WritePacket(buffer *buf.Buffer, destination M.Socksaddr) error {
	header := buf.NewSize(3 + M.MaxSocksaddrLength)
	defer header.Release()
	common.Must(header.WriteZeroN(3))
	var address model.AddrSpec
	if destination.IsFqdn() {
		address.FQDN = destination.Fqdn
	} else {
		address.IP = destination.Addr.AsSlice()
	}
	address.Port = int(destination.Port)
	if err := address.WriteToSocks5(header); err != nil {
		return err
	}
	packet := buf.NewSize(header.Len() + buffer.Len())
	defer packet.Release()
	common.Must1(packet.Write(header.Bytes()))
	common.Must1(packet.Write(buffer.Bytes()))
	_, err := c.PacketConn.WriteTo(packet.Bytes(), nil)
	return err
}

type listenerFactory struct {
	ctx     context.Context
	options option.ListenOptions
}

var (
	_ mierucommon.StreamListenerFactory = (*listenerFactory)(nil)
	_ mierucommon.PacketListenerFactory = (*listenerFactory)(nil)
)

func (f *listenerFactory) Listen(ctx context.Context, network, address string) (net.Listener, error) {
	var listenConfig net.ListenConfig
	if err := f.applyControl(&listenConfig, false); err != nil {
		return nil, err
	}
	if f.options.DisableTCPKeepAlive {
		listenConfig.KeepAlive = -1
		listenConfig.KeepAliveConfig.Enable = false
	} else {
		keepIdle := time.Duration(f.options.TCPKeepAlive)
		if keepIdle == 0 {
			keepIdle = C.TCPKeepAliveInitial
		}
		keepInterval := time.Duration(f.options.TCPKeepAliveInterval)
		if keepInterval == 0 {
			keepInterval = C.TCPKeepAliveInterval
		}
		listenConfig.KeepAliveConfig = net.KeepAliveConfig{
			Enable:   true,
			Idle:     keepIdle,
			Interval: keepInterval,
		}
	}
	if f.options.TCPMultiPath {
		listenConfig.SetMultipathTCP(true)
	}
	return listener.ListenNetworkNamespace[net.Listener](f.ctx, f.options.NetNs, func() (net.Listener, error) {
		if f.options.TCPFastOpen {
			var tfoConfig tfo.ListenConfig
			tfoConfig.ListenConfig = listenConfig
			return tfoConfig.Listen(ctx, network, address)
		}
		return listenConfig.Listen(ctx, network, address)
	})
}

func (f *listenerFactory) ListenPacket(ctx context.Context, network, address string) (net.PacketConn, error) {
	var listenConfig net.ListenConfig
	if err := f.applyControl(&listenConfig, true); err != nil {
		return nil, err
	}
	return listener.ListenNetworkNamespace[net.PacketConn](f.ctx, f.options.NetNs, func() (net.PacketConn, error) {
		return listenConfig.ListenPacket(ctx, network, address)
	})
}

func (f *listenerFactory) applyControl(listenConfig *net.ListenConfig, packet bool) error {
	if f.options.BindInterface != "" {
		networkManager := service.FromContext[adapter.NetworkManager](f.ctx)
		if networkManager == nil {
			return E.New("mieru: bind_interface requires a running sing-box instance")
		}
		listenConfig.Control = control.Append(listenConfig.Control, control.BindToInterface(networkManager.InterfaceFinder(), f.options.BindInterface, -1))
	}
	if f.options.RoutingMark != 0 {
		listenConfig.Control = control.Append(listenConfig.Control, control.RoutingMark(uint32(f.options.RoutingMark)))
	}
	if f.options.ReuseAddr {
		listenConfig.Control = control.Append(listenConfig.Control, control.ReuseAddr())
	}
	if packet {
		udpFragment := f.options.UDPFragmentDefault
		if f.options.UDPFragment != nil {
			udpFragment = *f.options.UDPFragment
		}
		if udpFragment {
			listenConfig.Control = control.Append(listenConfig.Control, control.EnableUDPFragment())
		} else {
			listenConfig.Control = control.Append(listenConfig.Control, control.DisableUDPFragment())
		}
	}
	return nil
}
