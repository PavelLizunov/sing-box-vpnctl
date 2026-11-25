package exit

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sagernet/sing/common/bufio"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/xtaci/kcp-go/v5"
	"github.com/xtaci/smux"
)

var ErrPrivateDestination = errors.New("exit: destination is private, loopback or link-local")

type ServerOptions struct {
	DialTimeout     time.Duration // per destination dial, default 10s
	MaxAssociations int
	MaxStreams      int           // concurrent streams per session, default 256
	AllowPrivate    bool          // serve private/loopback/link-local destinations
	Bind            string        // optional local address for outbound sockets
	UDPTimeout      time.Duration // idle timeout per UDP association, default 60s
	// Resolver resolves an FQDN destination to IPs. nil uses the system
	// resolver. It runs off the shared UDP read loop (per association), so a
	// slow lookup stalls only its own association, never other sessions.
	Resolver func(ctx context.Context, host string) ([]netip.Addr, error)
	Logf     func(string, ...any)
}

func (o *ServerOptions) defaults() {
	if o.DialTimeout == 0 {
		o.DialTimeout = 10 * time.Second
	}
	if o.MaxStreams == 0 {
		o.MaxStreams = 256
	}
	if o.MaxAssociations <= 0 {
		o.MaxAssociations = 256
	}
	if o.UDPTimeout == 0 {
		o.UDPTimeout = 60 * time.Second
	}
	if o.Resolver == nil {
		o.Resolver = func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		}
	}
	if o.Logf == nil {
		o.Logf = func(string, ...any) {}
	}
}

// Server terminates the exit protocol on a multi-session PacketConn (one
// peer address per client session) and dials out: smux streams over KCP for
// TCP, framed datagrams for UDP.
type Server struct {
	o            ServerOptions
	demux        *Demux
	udpPipe      *fecConn       // demux.UDP() wrapped in adaptive FEC (realtime flush)
	useq         *flowSequencer // per-flow send sequence for UDP reordering (replies)
	reorder      *reorderBuffer // per-flow receive reordering for UDP (client->dest)
	dialer       net.Dialer
	lc           net.ListenConfig
	amu          sync.Mutex
	assocs       map[udpFlow]*assoc
	assocCounts  map[string]int
	pc           net.PacketConn
	assocWorkers sync.WaitGroup
	once         sync.Once
	closed       chan struct{}
}

// maxAssocCache bounds the per-association resolve cache so a client naming
// many distinct destinations cannot grow it without limit.
const maxAssocCache = 512

type udpItem struct {
	dest    M.Socksaddr
	payload []byte
}

type assoc struct {
	pc          net.PacketConn
	last        atomic.Int64
	out         chan udpItem              // datagrams awaiting resolve and forward
	cache       map[string]netip.AddrPort // dest.String() -> checked target; only the send goroutine touches it
	ctx         context.Context
	cancel      context.CancelFunc
	stopSession func() bool
	once        sync.Once
}

// stop closes the association's socket and signals its goroutines to exit,
// exactly once.
func (a *assoc) stop() {
	a.once.Do(func() {
		a.stopSession()
		a.cancel()
		_ = a.pc.Close()
	})
}

// target resolves and policy-checks a destination, caching the result per
// association so repeat datagrams to the same destination skip DNS. Only the
// association's send goroutine calls it, so the cache needs no lock.
func (a *assoc) target(ctx context.Context, s *Server, dest M.Socksaddr) (netip.AddrPort, error) {
	key := dest.String()
	if t, ok := a.cache[key]; ok {
		return t, nil
	}
	rctx, cancel := context.WithTimeout(ctx, s.o.DialTimeout)
	defer cancel()
	t, err := s.resolveChecked(rctx, dest)
	if err != nil {
		return netip.AddrPort{}, err
	}
	if len(a.cache) < maxAssocCache {
		a.cache[key] = t
	}
	return t, nil
}

func NewServer(pc net.PacketConn, o ServerOptions) *Server {
	o.defaults()
	s := &Server{o: o, demux: NewDemux(pc), pc: pc, assocs: map[udpFlow]*assoc{}, assocCounts: map[string]int{}, closed: make(chan struct{})}
	// FEC-protect the UDP passthrough (adaptive block FEC, short flush), the
	// mirror of the client's udpPipe, so datagram apps get loss recovery.
	s.udpPipe = newRealtimeFECConn(s.demux.UDP(), s.demux.LossRate)
	s.useq = newFlowSequencer()
	s.reorder = newReorderBuffer(defaultReleaseAfter)
	if o.Bind != "" {
		if ip := net.ParseIP(o.Bind); ip != nil {
			s.dialer.LocalAddr = &net.TCPAddr{IP: ip}
		}
	}
	return s
}

func (s *Server) Close() error {
	s.once.Do(func() {
		close(s.closed)
		if s.udpPipe != nil {
			_ = s.udpPipe.Close()
		}
		_ = s.demux.Close()
		s.amu.Lock()
		for key, a := range s.assocs {
			s.removeAssoc(key, a)
		}
		s.amu.Unlock()
		s.assocWorkers.Wait()
	})
	return nil
}

// Serve blocks until ctx ends or the pipe fails.
func (s *Server) Serve(ctx context.Context) error {
	pipe := newFECConn(s.demux.KCP(), s.demux.LossRate)
	defer pipe.Close()
	ln, err := kcp.ServeConn(nil, 0, 0, pipe)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	stop := context.AfterFunc(ctx, func() { _ = ln.Close() })
	defer func() {
		cancel()
		stop()
		_ = ln.Close()
		_ = s.Close()
		workers.Wait()
	}()
	workers.Go(func() { s.serveUDP(ctx) })
	workers.Go(func() { s.reapAssocs(ctx) })
	workers.Go(func() {
		<-s.closed
		_ = ln.Close()
	})
	for {
		ks, err := ln.AcceptKCP()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		tuneKCP(ks)
		workers.Go(func() { s.serveSession(ctx, ks) })
	}
}

func (s *Server) serveSession(ctx context.Context, ks *kcp.UDPSession) {
	defer ks.Close()
	sess, err := smux.Server(ks, smuxConfig())
	if err != nil {
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	stop := context.AfterFunc(ctx, func() { _ = sess.Close() })
	defer func() {
		cancel()
		stop()
		_ = sess.Close()
		workers.Wait()
	}()
	var streams atomic.Int32
	var capLogged bool
	for {
		st, err := sess.AcceptStream()
		if err != nil {
			return
		}
		if int(streams.Load()) >= s.o.MaxStreams {
			_ = st.Close()
			if !capLogged {
				s.o.Logf("exit: session %s hit the stream cap (%d)", ks.RemoteAddr(), s.o.MaxStreams)
				capLogged = true
			}
			continue
		}
		streams.Add(1)
		workers.Go(func() {
			defer streams.Add(-1)
			s.handleStream(ctx, st)
		})
	}
}

func (s *Server) handleStream(ctx context.Context, st *smux.Stream) {
	defer st.Close()
	_ = st.SetReadDeadline(time.Now().Add(s.o.DialTimeout))
	dest, err := ReadStreamHeader(st)
	if err != nil {
		return
	}
	_ = st.SetReadDeadline(time.Time{})
	dctx, cancel := context.WithTimeout(ctx, s.o.DialTimeout)
	defer cancel()
	target, err := s.resolveChecked(dctx, dest)
	if err != nil {
		s.o.Logf("exit: refuse %s: %v", dest, err)
		return
	}
	rc, err := s.dialer.DialContext(dctx, "tcp", target.String())
	if err != nil {
		s.o.Logf("exit: dial %s: %v", dest, err)
		return
	}
	_ = bufio.CopyConn(ctx, st, rc)
}

// resolveChecked resolves a destination on the server and applies the
// private-destination policy to the address actually dialed.
func (s *Server) resolveChecked(ctx context.Context, dest M.Socksaddr) (netip.AddrPort, error) {
	var ips []netip.Addr
	if dest.IsFqdn() {
		var err error
		ips, err = s.o.Resolver(ctx, dest.Fqdn)
		if err != nil {
			return netip.AddrPort{}, err
		}
		if len(ips) == 0 {
			return netip.AddrPort{}, errors.New("exit: no address for " + dest.Fqdn)
		}
	} else {
		ips = []netip.Addr{dest.Addr}
	}
	for _, ip := range ips {
		if !s.o.AllowPrivate && isPrivate(ip.Unmap()) {
			return netip.AddrPort{}, ErrPrivateDestination
		}
	}
	return netip.AddrPortFrom(ips[0].Unmap(), dest.Port), nil
}

func isPrivate(ip netip.Addr) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast()
}

// serveUDP reads UDP frames from every session and forwards them from a
// per-(session, assoc) socket; replies go back as frames to that session.
func (s *Server) serveUDP(ctx context.Context) {
	s.udpPipe.receiveUDP(s.reorder, func(seq uint32, peer net.Addr, frame []byte) {
		id, dest, payload, err := DecodeUDPFrame(frame)
		if err != nil {
			return
		}
		key := udpFlow{id: id, session: peer.String()}
		s.amu.Lock()
		defer s.amu.Unlock()
		a, err := s.assocFor(ctx, key, peer)
		if err != nil {
			return
		}
		a.last.Store(time.Now().UnixNano())
		deliver := func(payload []byte) {
			select {
			case <-a.ctx.Done():
			case a.out <- udpItem{dest: dest, payload: payload}:
			default:
			}
		}
		key.peer = dest.String()
		for _, p := range s.reorder.push(key, seq, payload, time.Now(), deliver) {
			deliver(p)
		}
	})
}

type sessionContextSource interface {
	SessionContext(net.Addr) (context.Context, bool)
}

func (s *Server) assocFor(ctx context.Context, key udpFlow, peer net.Addr) (*assoc, error) {
	select {
	case <-s.closed:
		return nil, net.ErrClosed
	default:
	}
	if a := s.assocs[key]; a != nil {
		return a, a.ctx.Err()
	}
	if s.assocCounts[key.session] >= s.o.MaxAssociations {
		return nil, errors.New("exit: UDP association limit reached")
	}
	parent := ctx
	if source, ok := s.pc.(sessionContextSource); ok {
		var live bool
		parent, live = source.SessionContext(peer)
		if !live || parent.Err() != nil {
			return nil, net.ErrClosed
		}
	}
	bind := ":0"
	if s.o.Bind != "" {
		bind = net.JoinHostPort(s.o.Bind, "0")
	}
	pc, err := s.lc.ListenPacket(ctx, "udp", bind)
	if err != nil {
		return nil, err
	}
	lifetime, cancel := context.WithCancel(parent)
	a := &assoc{pc: pc, out: make(chan udpItem, 256),
		cache: map[string]netip.AddrPort{}, ctx: lifetime, cancel: cancel}
	a.stopSession = context.AfterFunc(lifetime, func() {
		s.amu.Lock()
		defer s.amu.Unlock()
		if s.assocs[key] == a {
			s.removeAssoc(key, a)
		}
	})
	a.last.Store(time.Now().UnixNano())
	s.assocs[key] = a
	s.assocCounts[key.session]++
	// reply reader: datagrams from the outbound socket back to the client
	s.assocWorkers.Go(func() {
		rb := make([]byte, 65535)
		for {
			n, from, err := pc.ReadFrom(rb)
			if err != nil {
				return
			}
			saddr := M.SocksaddrFromNet(from)
			frame, err := EncodeUDPFrame(key.id, saddr, rb[:n])
			if err != nil {
				continue
			}
			a.last.Store(time.Now().UnixNano())
			// Stamp a per-flow sequence so the client reorders replies.
			flow := key
			flow.peer = saddr.String()
			seq, ok := s.useq.next(flow)
			if ok {
				_, _ = s.udpPipe.WriteToContext(a.ctx, prependSeq(seq, frame), peer)
			}
		}
	})
	// sender: resolve (off the shared read loop, with a per-association cache)
	// and forward each queued datagram
	s.assocWorkers.Go(func() {
		for {
			select {
			case <-a.ctx.Done():
				return
			case <-s.closed:
				return
			case it := <-a.out:
				target, err := a.target(a.ctx, s, it.dest)
				if err != nil {
					continue
				}
				a.last.Store(time.Now().UnixNano())
				_, _ = a.pc.WriteTo(it.payload, net.UDPAddrFromAddrPort(target))
			}
		}
	})
	return a, nil
}

func (s *Server) removeAssoc(key udpFlow, a *assoc) {
	a.stop()
	s.reorder.remove(key)
	delete(s.assocs, key)
	s.assocCounts[key.session]--
	if s.assocCounts[key.session] == 0 {
		delete(s.assocCounts, key.session)
	}
}

func (s *Server) reapAssocs(ctx context.Context) {
	t := time.NewTicker(s.o.UDPTimeout / 2)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.closed:
			return
		case now := <-t.C:
			s.amu.Lock()
			for key, a := range s.assocs {
				if now.UnixNano()-a.last.Load() > int64(s.o.UDPTimeout) {
					s.removeAssoc(key, a)
				}
			}
			s.amu.Unlock()
		}
	}
}
