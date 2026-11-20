package trafficcontrol

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	"golang.org/x/time/rate"
)

const (
	userBandwidthBurst = 64 * 1024
	userBandwidthChunk = 16 * 1024
)

var _ adapter.ConnectionTracker = (*UserBandwidth)(nil)

type UserBandwidth struct {
	options option.VPNCTLUserBandwidthOptions
	users   sync.Map
}

type userBandwidthIdentity struct {
	user             string
	unmappedEndpoint string
}

func NewUserBandwidth(options option.VPNCTLUserBandwidthOptions) (*UserBandwidth, error) {
	if options.BitsPerSecond < 8 {
		return nil, fmt.Errorf("vpnctl_user_bandwidth.bits_per_second must be at least 8")
	}
	for tag, peers := range options.WireGuardPeers {
		for address, user := range peers {
			if tag == "" || !address.IsValid() || address.Is4In6() || user == "" {
				return nil, fmt.Errorf("vpnctl_user_bandwidth has an invalid WireGuard peer mapping")
			}
		}
	}
	return &UserBandwidth{options: options}, nil
}

func (b *UserBandwidth) limiter(metadata adapter.InboundContext) *rate.Limiter {
	identity := userBandwidthIdentity{user: metadata.User}
	if metadata.InboundType == "wireguard" {
		identity.user = b.options.WireGuardPeers[metadata.Inbound][metadata.Source.Addr.Unmap()]
		if identity.user == "" {
			identity.unmappedEndpoint = metadata.Inbound
		}
	} else if identity.user == "" {
		return nil
	}
	if existing, ok := b.users.Load(identity); ok {
		return existing.(*rate.Limiter)
	}
	next := rate.NewLimiter(rate.Limit(b.options.BitsPerSecond)/8, userBandwidthBurst)
	existing, _ := b.users.LoadOrStore(identity, next)
	return existing.(*rate.Limiter)
}

func (b *UserBandwidth) RoutedConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, _ adapter.Rule, _ adapter.Outbound) net.Conn {
	limiter := b.limiter(metadata)
	if limiter == nil {
		return conn
	}
	ctx, cancel := context.WithCancel(ctx)
	return &userBandwidthConn{Conn: conn, ctx: ctx, cancel: cancel, limiter: limiter}
}

func (b *UserBandwidth) RoutedPacketConnection(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext, _ adapter.Rule, _ adapter.Outbound) N.PacketConn {
	limiter := b.limiter(metadata)
	if limiter == nil {
		return conn
	}
	ctx, cancel := context.WithCancel(ctx)
	return &userBandwidthPacketConn{PacketConn: conn, ctx: ctx, cancel: cancel, limiter: limiter}
}

func (b *UserBandwidth) RoutedFlow(context.Context, adapter.InboundContext, adapter.Rule, adapter.Outbound) tun.FlowTracker {
	return nil
}

type userBandwidthConn struct {
	net.Conn
	ctx     context.Context
	cancel  context.CancelFunc
	limiter *rate.Limiter
	writeMu sync.Mutex
}

func (c *userBandwidthConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p[:min(len(p), userBandwidthChunk)])
	if n != 0 {
		if waitErr := c.limiter.WaitN(c.ctx, n); waitErr != nil {
			return 0, waitErr
		}
	}
	return n, err
}

func (c *userBandwidthConn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	var written int
	for written < len(p) {
		end := min(written+userBandwidthChunk, len(p))
		if err := c.limiter.WaitN(c.ctx, end-written); err != nil {
			return written, err
		}
		n, err := c.Conn.Write(p[written:end])
		written += n
		if err != nil {
			return written, err
		}
		if written < end {
			return written, io.ErrShortWrite
		}
	}
	return written, nil
}

func (c *userBandwidthConn) CloseWrite() error {
	writer, _ := N.UnwrapCountWriter(c.Conn, nil)
	if closer, ok := N.UnwrapWriter(writer).(N.WriteCloser); ok {
		return closer.CloseWrite()
	}
	return c.Close()
}

func (c *userBandwidthConn) Upstream() any {
	return c.Conn
}

func (c *userBandwidthConn) ReaderReplaceable() bool {
	return false
}

func (c *userBandwidthConn) WriterReplaceable() bool {
	return false
}

func (c *userBandwidthConn) Close() error {
	c.cancel()
	return c.Conn.Close()
}

type userBandwidthPacketConn struct {
	N.PacketConn
	ctx     context.Context
	cancel  context.CancelFunc
	limiter *rate.Limiter
}

func (c *userBandwidthPacketConn) ReadPacket(buffer *buf.Buffer) (M.Socksaddr, error) {
	address, err := c.PacketConn.ReadPacket(buffer)
	if err != nil {
		return address, err
	}
	return address, c.limiter.WaitN(c.ctx, buffer.Len())
}

func (c *userBandwidthPacketConn) WritePacket(buffer *buf.Buffer, address M.Socksaddr) error {
	if err := c.limiter.WaitN(c.ctx, buffer.Len()); err != nil {
		buffer.Release()
		return err
	}
	return c.PacketConn.WritePacket(buffer, address)
}

func (c *userBandwidthPacketConn) Upstream() any {
	return c.PacketConn
}

func (c *userBandwidthPacketConn) ReaderReplaceable() bool {
	return false
}

func (c *userBandwidthPacketConn) WriterReplaceable() bool {
	return false
}

func (c *userBandwidthPacketConn) Close() error {
	c.cancel()
	return c.PacketConn.Close()
}
