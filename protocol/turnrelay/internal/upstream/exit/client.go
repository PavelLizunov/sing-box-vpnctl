package exit

import (
	"context"
	"errors"
	"math/rand/v2"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pion/transport/v4/deadline"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/xtaci/kcp-go/v5"
	"github.com/xtaci/smux"
)

var ErrNetwork = errors.New("exit: DialContext supports tcp only; use ListenPacket for udp")

type ClientOptions struct {
	Logf func(string, ...any)
}

// Client is the proxy side of the exit protocol over one datagram pipe to
// the exit server: TCP through smux streams over a lazily established KCP
// session, UDP through framed datagrams with one association per
// ListenPacket conn.
type Client struct {
	pipe    net.PacketConn
	server  net.Addr
	demux   *Demux
	udpPipe *fecConn       // demux.UDP() wrapped in adaptive FEC (realtime flush)
	useq    *flowSequencer // per-flow send sequence for UDP reordering
	reorder *reorderBuffer // per-flow receive reordering for UDP
	o       ClientOptions

	mu      sync.Mutex
	ks      *kcp.UDPSession
	sess    *smux.Session
	kcpPipe *fecConn

	amu    sync.Mutex
	assocs map[uint16]*clientAssoc
	next   uint16

	closed chan struct{}
	once   sync.Once
	wg     sync.WaitGroup
}

func NewClient(pipe net.PacketConn, server net.Addr, o ClientOptions) *Client {
	if o.Logf == nil {
		o.Logf = func(string, ...any) {}
	}
	c := &Client{pipe: pipe, server: server, demux: NewDemux(pipe), o: o, assocs: map[uint16]*clientAssoc{}, closed: make(chan struct{})}
	// FEC-protect the UDP passthrough too (adaptive block FEC, short flush for
	// real-time), so datagram apps get the same loss recovery as the KCP path.
	c.udpPipe = newRealtimeFECConn(c.demux.UDP(), c.demux.LossRate)
	c.useq = newFlowSequencer()
	c.reorder = newReorderBuffer(defaultReleaseAfter)
	c.next = uint16(rand.Uint32())
	c.wg.Go(c.udpLoop)
	return c
}

func (c *Client) Close() error {
	c.once.Do(func() {
		c.amu.Lock()
		close(c.closed)
		for id, a := range c.assocs {
			a.cancel()
			delete(c.assocs, id)
		}
		c.amu.Unlock()
		_ = c.demux.Close()
		c.mu.Lock()
		if c.sess != nil {
			_ = c.sess.Close()
		}
		if c.kcpPipe != nil {
			_ = c.kcpPipe.Close()
		}
		c.ks, c.sess = nil, nil
		c.mu.Unlock()
		if c.udpPipe != nil {
			_ = c.udpPipe.Close()
		}
		c.wg.Wait()
	})
	return nil
}

// session returns the live smux session, (re)establishing KCP and smux when
// there is none or the previous one died. It fails fast once the client is
// closed, instead of standing up an orphaned session that Close will never
// tear down.
func (c *Client) session() (*smux.Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.closed:
		return nil, net.ErrClosed
	default:
	}
	if c.sess != nil && !c.sess.IsClosed() {
		return c.sess, nil
	}
	if c.ks != nil {
		_ = c.ks.Close()
		_ = c.kcpPipe.Close()
		c.ks, c.sess, c.kcpPipe = nil, nil, nil
	}
	// Adaptive block FEC wraps the pipe; kcp-go's own FEC is off (0,0).
	pipe := newFECConn(c.demux.KCP(), c.demux.LossRate)
	ks, err := kcp.NewConn4(rand.Uint32(), c.server, nil, 0, 0, true, pipe)
	if err != nil {
		_ = pipe.Close()
		return nil, err
	}
	tuneKCP(ks)
	sess, err := smux.Client(ks, smuxConfig())
	if err != nil {
		_ = ks.Close()
		return nil, err
	}
	c.ks, c.sess, c.kcpPipe = ks, sess, pipe
	c.o.Logf("exit: session established")
	return sess, nil
}

func (c *Client) DialContext(ctx context.Context, network string, dest M.Socksaddr) (net.Conn, error) {
	if !strings.HasPrefix(network, "tcp") {
		return nil, ErrNetwork
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sess, err := c.session()
	if err != nil {
		return nil, err
	}
	st, err := sess.OpenStream()
	if err != nil {
		return nil, err
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = st.SetWriteDeadline(dl)
	}
	writeDone := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(writeDone)
		_ = st.SetWriteDeadline(time.Now())
	})
	err = WriteStreamHeader(st, dest)
	if !stop() {
		<-writeDone
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	_ = st.SetWriteDeadline(time.Time{})
	return st, nil
}

// ListenPacket returns a conn bound to a fresh association id; dest is
// ignored (every WriteTo names its own destination).
func (c *Client) ListenPacket(ctx context.Context, dest M.Socksaddr) (net.PacketConn, error) {
	c.amu.Lock()
	defer c.amu.Unlock()
	select {
	case <-c.closed:
		return nil, net.ErrClosed
	default:
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(c.assocs) == 1<<16 {
		return nil, errors.New("exit: UDP association IDs exhausted")
	}
	for {
		c.next++
		if _, taken := c.assocs[c.next]; !taken {
			break
		}
	}
	lifetime, cancel := context.WithCancel(context.Background())
	a := &clientAssoc{c: c, id: c.next, in: make(chan packet, 256), dl: deadline.New(), ctx: lifetime, cancel: cancel}
	c.assocs[a.id] = a
	return a, nil
}

func (c *Client) udpLoop() {
	c.udpPipe.receiveUDP(c.reorder, func(seq uint32, _ net.Addr, frame []byte) {
		id, from, payload, err := DecodeUDPFrame(frame)
		if err != nil {
			return
		}
		c.amu.Lock()
		defer c.amu.Unlock()
		a := c.assocs[id]
		if a == nil {
			return
		}
		fromAddr := from.UDPAddr()
		deliver := func(payload []byte) {
			select {
			case <-a.ctx.Done():
			case a.in <- packet{b: payload, addr: fromAddr}:
			default:
			}
		}
		for _, p := range c.reorder.push(udpFlow{id: id, peer: from.String()}, seq, payload, time.Now(), deliver) {
			deliver(p)
		}
	})
}

type clientAssoc struct {
	c      *Client
	id     uint16
	in     chan packet
	dl     *deadline.Deadline
	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once
}

func (a *clientAssoc) ReadFrom(b []byte) (int, net.Addr, error) {
	if a.ctx.Err() != nil {
		return 0, nil, net.ErrClosed
	}
	select {
	case p := <-a.in:
		return copy(b, p.b), p.addr, nil
	case <-a.ctx.Done():
		return 0, nil, net.ErrClosed
	case <-a.c.closed:
		return 0, nil, net.ErrClosed
	case <-a.dl.Done():
		return 0, nil, os.ErrDeadlineExceeded
	}
}

func (a *clientAssoc) WriteTo(b []byte, addr net.Addr) (int, error) {
	if a.ctx.Err() != nil {
		return 0, net.ErrClosed
	}
	dest := M.SocksaddrFromNet(addr)
	frame, err := EncodeUDPFrame(a.id, dest, b)
	if err != nil {
		return 0, err
	}
	// Stamp a per-flow sequence so the server can reorder this conversation.
	a.c.amu.Lock()
	if a.ctx.Err() != nil {
		a.c.amu.Unlock()
		return 0, net.ErrClosed
	}
	seq, ok := a.c.useq.next(udpFlow{id: a.id, peer: dest.String()})
	a.c.amu.Unlock()
	if !ok {
		return 0, errUDPFlowLimit
	}
	if _, err := a.c.udpPipe.WriteToContext(a.ctx, prependSeq(seq, frame), a.c.server); err != nil {
		if a.ctx.Err() != nil {
			return 0, net.ErrClosed
		}
		return 0, err
	}
	return len(b), nil
}

func (a *clientAssoc) Close() error {
	a.once.Do(func() {
		a.c.amu.Lock()
		defer a.c.amu.Unlock()
		a.cancel()
		if a.c.assocs[a.id] == a {
			delete(a.c.assocs, a.id)
			key := udpFlow{id: a.id}
			a.c.reorder.remove(key)
			a.c.useq.remove(key)
		}
	})
	return nil
}

func (a *clientAssoc) LocalAddr() net.Addr               { return a.c.pipe.LocalAddr() }
func (a *clientAssoc) SetDeadline(t time.Time) error     { a.dl.Set(t); return nil }
func (a *clientAssoc) SetReadDeadline(t time.Time) error { a.dl.Set(t); return nil }
func (a *clientAssoc) SetWriteDeadline(time.Time) error  { return nil }
