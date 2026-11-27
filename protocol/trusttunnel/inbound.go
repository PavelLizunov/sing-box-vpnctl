//go:build with_trusttunnel

package trusttunnel

import (
	"context"
	stdTLS "crypto/tls"
	"errors"
	"net"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/common/listener"
	"github.com/sagernet/sing-box/common/tls"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/auth"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	TT "github.com/xchacha20-poly1305/sing-trusttunnel"
	"golang.org/x/net/http2"
)

func RegisterInbound(registry *inbound.Registry) {
	inbound.Register(registry, C.TypeTrustTunnel, NewInbound)
}

type Inbound struct {
	inbound.Adapter
	ctx         context.Context
	router      adapter.Router
	logger      log.ContextLogger
	options     option.TrustTunnelInboundOptions
	tlsConfig   tls.ServerConfig
	tcpListener *listener.Listener
	tcpAddress  M.Socksaddr
	udp         bool
}

func NewInbound(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.TrustTunnelInboundOptions) (adapter.Inbound, error) {
	if err := validateQUIC(options.TrustTunnelQUICOptions); err != nil {
		return nil, err
	}
	if len(options.Users) == 0 {
		return nil, errors.New("TrustTunnel inbound requires at least one user")
	}
	users := make(map[string]struct{}, len(options.Users))
	for _, user := range options.Users {
		if err := validateCredentials(user.Username, user.Password); err != nil {
			return nil, err
		}
		if _, exists := users[user.Username]; exists {
			return nil, errors.New("duplicate TrustTunnel username")
		}
		users[user.Username] = struct{}{}
	}
	tlsEnabled := options.TLS != nil && options.TLS.Enabled
	udp := tlsEnabled
	if options.UDP != nil {
		udp = *options.UDP
	}
	if !tlsEnabled && (options.QUIC || udp) {
		return nil, errors.New("TLS is required for TrustTunnel inbound with QUIC or UDP")
	}
	handler := &Inbound{
		Adapter: inbound.NewAdapter(C.TypeTrustTunnel, tag),
		ctx:     ctx, router: router, logger: logger, options: options, udp: udp,
	}
	if tlsEnabled {
		config, err := tls.NewServer(ctx, logger, *options.TLS)
		if err != nil {
			return nil, err
		}
		if _, err = config.STDConfig(); err != nil {
			return nil, err
		}
		config.SetNextProtos([]string{http2.NextProtoTLS})
		handler.tlsConfig = config
	}
	handler.tcpListener = listener.New(listener.Options{Context: ctx, Logger: logger, Listen: options.ListenOptions})
	return handler, nil
}

func (h *Inbound) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	if stage != adapter.StartStateStart {
		return nil
	}
	if h.tlsConfig != nil {
		if err := h.tlsConfig.Start(); err != nil {
			return err
		}
		scope.Add(h.tlsConfig.Close)
	}
	tcpListener, err := h.tcpListener.ListenTCP()
	if err != nil {
		return err
	}
	scope.Add(h.tcpListener.Close)
	h.tcpAddress = M.SocksaddrFromNet(tcpListener.Addr())

	service := TT.NewService(TT.ServiceOptions{
		Ctx: scope.Context(), Logger: h.logger, Handler: h,
		QUICCongestionControl: h.options.QUICCongestionControl,
	})
	service.UpdateUsers(h.options.Users)
	scope.Add(service.Close)
	if h.options.QUIC {
		udpOptions := h.options.ListenOptions
		udpOptions.ListenPort = h.tcpAddress.Port
		udpListener := listener.New(listener.Options{Context: h.ctx, Logger: h.logger, Listen: udpOptions})
		packetConn, err := udpListener.ListenUDP()
		if err != nil {
			return err
		}
		if err := service.Start(nil, packetConn, quicServerTLS{h.tlsConfig}); err != nil {
			_ = packetConn.Close()
			return err
		}
	}
	if h.tlsConfig != nil {
		config, err := h.tlsConfig.STDConfig()
		if err != nil {
			return err
		}
		// Use net/http's native TLS handling instead of the library's lazy listener.
		tcpListener = stdTLS.NewListener(tcpListener, config)
	}
	return service.Start(tcpListener, nil, nil)
}

func (h *Inbound) metadata(ctx context.Context, source, destination M.Socksaddr) adapter.InboundContext {
	user, _ := auth.UserFromContext[string](ctx)
	return adapter.InboundContext{
		Inbound: h.Tag(), InboundType: h.Type(), InboundDetour: h.options.Detour,
		Source: source, Destination: destination, User: user,
	}
}

func (h *Inbound) NewConnectionEx(ctx context.Context, conn net.Conn, source, destination M.Socksaddr, onClose N.CloseHandlerFunc) {
	h.router.RouteConnectionEx(ctx, conn, h.metadata(ctx, source, destination), onClose)
}

func (h *Inbound) NewPacketConnectionEx(ctx context.Context, conn N.PacketConn, source, destination M.Socksaddr, onClose N.CloseHandlerFunc) {
	if !h.udp {
		N.CloseOnHandshakeFailure(conn, onClose, errors.New("TrustTunnel inbound UDP is disabled"))
		return
	}
	metadata := h.metadata(ctx, source, destination)
	metadata.UDPTimeout = time.Duration(h.options.UDPTimeout)
	h.router.RoutePacketConnectionEx(ctx, conn, metadata, onClose)
}

// Keep the core's certificate reload callback while restricting QUIC to h3.
// The callback normally returns the live TCP config, whose ALPN is h2.
type quicServerTLS struct {
	tls.ServerConfig
}

func (c quicServerTLS) Clone() tls.Config {
	return quicServerTLS{c.ServerConfig.Clone().(tls.ServerConfig)}
}

func (c quicServerTLS) STDConfig() (*stdTLS.Config, error) {
	config, err := c.ServerConfig.STDConfig()
	if err != nil {
		return nil, err
	}
	config = config.Clone()
	config.NextProtos = []string{"h3"}
	if getConfig := config.GetConfigForClient; getConfig != nil {
		config.GetConfigForClient = func(info *stdTLS.ClientHelloInfo) (*stdTLS.Config, error) {
			current, err := getConfig(info)
			if current == nil || err != nil {
				return current, err
			}
			current = current.Clone()
			current.NextProtos = config.NextProtos
			return current, nil
		}
	}
	return config, nil
}
