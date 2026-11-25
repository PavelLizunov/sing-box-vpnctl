// Package relay opens one TURN allocation and keeps it alive.
package relay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/pion/logging"
	"github.com/pion/stun/v4"
	"github.com/pion/transport/v5/stdnet"
	"github.com/pion/turn/v5"
)

type Options struct {
	Server     string // host:port of the TURN relay
	Username   string
	Password   string
	UDP        bool // true: UDP transport (default and recommended); false: TCP
	PeerIsIPv6 bool // request an IPv6 relayed address
	Logger     logging.LoggerFactory
	// DialContext opens the socket towards the relay. nil uses the net
	// package directly. It is called once per allocation with network "udp"
	// (or "tcp" when UDP is false) and the resolved relay host:port; a UDP
	// conn must be connected to that address, since it is driven as a
	// PacketConn whose every write goes to the relay. Socket buffers are
	// enlarged only when the returned conn is a *net.UDPConn. This is the
	// hook a sing-box outbound uses for detour and bind_interface.
	DialContext func(ctx context.Context, network, address string) (net.Conn, error)
}

type Allocation struct {
	client   *turn.Client
	relayed  net.PacketConn
	base     io.Closer
	server   net.Addr
	once     sync.Once
	closeErr error
}

// connectedUDP makes a connected UDP socket usable as net.PacketConn.
type connectedUDP struct{ *net.UDPConn }

func (c *connectedUDP) WriteTo(b []byte, _ net.Addr) (int, error) { return c.Write(b) }

// connectedConn does the same for any connected datagram net.Conn that a
// custom DialContext returns: reads are attributed to the relay, writes
// ignore the destination.
type connectedConn struct {
	net.Conn
	peer net.Addr
}

func (c *connectedConn) ReadFrom(b []byte) (int, net.Addr, error) {
	n, err := c.Conn.Read(b)
	return n, c.peer, err
}

func (c *connectedConn) WriteTo(b []byte, _ net.Addr) (int, error) { return c.Conn.Write(b) }

func (c *connectedConn) RemoteAddr() net.Addr { return c.peer }

// dialUDP opens the connected UDP socket to raddr, through o.DialContext
// when set.
func dialUDP(ctx context.Context, o Options, raddr *net.UDPAddr) (net.PacketConn, io.Closer, error) {
	if o.DialContext == nil {
		c, err := net.DialUDP("udp", nil, raddr)
		if err != nil {
			return nil, nil, err
		}
		setBuffers(c)
		return &connectedUDP{c}, c, nil
	}
	c, err := o.DialContext(ctx, "udp", raddr.String())
	if err != nil {
		return nil, nil, err
	}
	if uc, ok := c.(*net.UDPConn); ok {
		setBuffers(uc)
		return &connectedUDP{uc}, uc, nil
	}
	return &connectedConn{Conn: c, peer: raddr}, c, nil
}

func dialTCP(ctx context.Context, o Options, address string) (net.Conn, error) {
	if o.DialContext == nil {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", address)
	}
	return o.DialContext(ctx, "tcp", address)
}

func dialCustom(ctx context.Context, o Options) (string, net.PacketConn, io.Closer, error) {
	network := "udp"
	if !o.UDP {
		network = "tcp"
	}
	c, err := o.DialContext(ctx, network, o.Server)
	if err != nil {
		return "", nil, nil, fmt.Errorf("relay: dial %s: %w", network, err)
	}
	address := "0.0.0.0:0"
	peer := net.Addr(&net.UDPAddr{IP: net.IPv4zero})
	if remote := c.RemoteAddr(); remote != nil {
		if ipPort, parseErr := netip.ParseAddrPort(remote.String()); parseErr == nil {
			address = ipPort.String()
			peer = net.UDPAddrFromAddrPort(ipPort)
		}
	}
	// Invariant: Pion uses a numeric identifier for this connected socket, including detours that hide the relay IP.
	connected := &connectedConn{Conn: c, peer: peer}
	if !o.UDP {
		return address, turn.NewSTUNConn(connected), c, nil
	}
	if uc, ok := c.(*net.UDPConn); ok {
		setBuffers(uc)
		return address, &connectedUDP{uc}, uc, nil
	}
	return address, connected, c, nil
}

func setBuffers(c *net.UDPConn) {
	_ = c.SetReadBuffer(640 * 1024)
	_ = c.SetWriteBuffer(640 * 1024)
}

func resolveUDP(ctx context.Context, address string) (*net.UDPAddr, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if _, err := netip.ParseAddr(host); err != nil {
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		host = ips[0].String()
	}
	return net.ResolveUDPAddr("udp", net.JoinHostPort(host, port))
}

func Allocate(ctx context.Context, o Options) (*Allocation, error) {
	if o.Logger == nil {
		f := logging.NewDefaultLoggerFactory()
		f.DefaultLogLevel = logging.LogLevelWarn
		o.Logger = f
	}
	var (
		turnConn   net.PacketConn
		base       io.Closer
		serverAddr string
		err        error
	)
	if o.DialContext != nil {
		serverAddr, turnConn, base, err = dialCustom(ctx, o)
		if err != nil {
			return nil, err
		}
	} else {
		var raddr *net.UDPAddr
		raddr, err = resolveUDP(ctx, o.Server)
		if err != nil {
			return nil, fmt.Errorf("relay: resolve %s: %w", o.Server, err)
		}
		serverAddr = raddr.String()
		if o.UDP {
			turnConn, base, err = dialUDP(ctx, o, raddr)
			if err != nil {
				return nil, fmt.Errorf("relay: dial udp: %w", err)
			}
		} else {
			c, dialErr := dialTCP(ctx, o, serverAddr)
			if dialErr != nil {
				return nil, fmt.Errorf("relay: dial tcp: %w", dialErr)
			}
			turnConn, base = turn.NewSTUNConn(c), c
		}
	}
	stop := context.AfterFunc(ctx, func() { _ = base.Close() })
	defer stop()
	family := turn.RequestedAddressFamilyIPv4
	if o.PeerIsIPv6 {
		family = turn.RequestedAddressFamilyIPv6
	}
	client, err := turn.NewClient(&turn.ClientConfig{
		STUNServerAddr:         serverAddr,
		TURNServerAddr:         serverAddr,
		Conn:                   turnConn,
		Net:                    new(stdnet.Net), // zero value: no interface enumeration (Huawei ROMs deny NETLINK)
		Username:               o.Username,
		Password:               o.Password,
		RequestedAddressFamily: family,
		LoggerFactory:          o.Logger,
	})
	if err != nil {
		_ = base.Close()
		return nil, fmt.Errorf("relay: client: %w", err)
	}
	if err := client.Listen(); err != nil {
		client.Close()
		_ = base.Close()
		return nil, fmt.Errorf("relay: listen: %w", err)
	}
	relayed, err := client.AllocateWithContext(ctx)
	if err != nil {
		client.Close()
		_ = base.Close()
		return nil, fmt.Errorf("relay: allocate: %w", err)
	}
	server, _ := net.ResolveUDPAddr("udp", serverAddr)
	allocation := &Allocation{client: client, relayed: relayed, base: base, server: server}
	if !stop() || ctx.Err() != nil {
		_ = allocation.Close()
		return nil, ctx.Err()
	}
	return allocation, nil
}

func (a *Allocation) Relayed() net.PacketConn { return a.relayed }
func (a *Allocation) RelayedAddr() net.Addr   { return a.relayed.LocalAddr() }

func (a *Allocation) Close() error {
	a.once.Do(func() {
		timer := time.AfterFunc(350*time.Millisecond, func() { _ = a.base.Close() })
		a.closeErr = a.relayed.Close()
		timer.Stop()
		_ = a.base.Close()
		a.client.Close()
	})
	return a.closeErr
}

// Keepalive sends STUN Binding requests until ctx ends; VK relays drop
// silent allocations well before the 10 minute lifetime.
func (a *Allocation) Keepalive(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			msg, err := stun.Build(stun.TransactionID, stun.BindingRequest)
			if err == nil {
				_, _ = a.client.PerformTransactionWithContext(msg, a.server, false, ctx)
			}
		}
	}
}

// turnCode extracts the STUN/TURN numeric error code from err, if err (or
// something it wraps) is a *stun.TurnError. turn.Client's AllocateWithContext
// returns this type directly for any TURN error response (see pion/turn v5.1.1
// client.go's sendAllocateRequest), and Allocate's fmt.Errorf("...: %w", err)
// wrapping keeps it reachable via errors.As.
func turnCode(err error) (int, bool) {
	var te *stun.TurnError
	if errors.As(err, &te) {
		return int(te.ErrorCodeAttr.Code), true
	}
	return 0, false
}

func IsQuotaError(err error) bool {
	if err == nil {
		return false
	}
	if c, ok := turnCode(err); ok {
		return c == 486
	}
	return strings.Contains(strings.ToLower(err.Error()), "quota")
}

func IsAuthError(err error) bool {
	if err == nil {
		return false
	}
	if c, ok := turnCode(err); ok {
		// 400 is only accepted here as a *typed* TURN error code: pion/turn
		// v5.1.1's server answers a rejected custom AuthHandler (wrong
		// username, or a message-integrity/password mismatch) with a bare
		// 400 rather than the RFC 5389 401; coturn and VK's relays answer
		// 401. Requiring the typed code (rather than a bare "400" substring
		// match against err.Error()) keeps this from misclassifying
		// unrelated failures whose message happens to contain "400", such
		// as a dial error against a host:port ending in 400.
		return c == 401 || c == 438 || c == 400
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "unauthorized") || strings.Contains(s, "stale nonce") ||
		strings.Contains(s, "invalid credential")
}
