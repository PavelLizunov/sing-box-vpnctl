package exit

import (
	"context"
	"encoding/binary"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/transport/v4/deadline"
)

// Demux splits one datagram PacketConn into a KCP side and a UDP side by the
// discriminator byte, preserving the peer address (the server on the client,
// the session address on the server). Both sides write through the same
// underlying conn and prepend their own discriminator.
type Demux struct {
	pc      net.PacketConn
	kcp     chan packet
	udp     chan packet
	done    chan struct{}
	once    sync.Once
	sendSeq atomic.Uint32  // per-datagram sequence for KCP-side frames we send
	est     *lossEstimator // loss estimate from the KCP-side sequence we receive
}

type packet struct {
	b    []byte
	addr net.Addr
}

type sideConn struct {
	d      *Demux
	kind   byte
	in     chan packet
	dl     *deadline.Deadline
	ctx    context.Context
	cancel context.CancelFunc
}

func NewDemux(pc net.PacketConn) *Demux {
	d := &Demux{pc: pc, done: make(chan struct{}), est: newLossEstimator(256, 0.3),
		kcp: make(chan packet, 1024), udp: make(chan packet, 1024)}
	go d.loop()
	return d
}

func (d *Demux) side(kind byte, queue chan packet) *sideConn {
	ctx, cancel := context.WithCancel(context.Background())
	return &sideConn{d: d, kind: kind, in: queue, dl: deadline.New(), ctx: ctx, cancel: cancel}
}

func (d *Demux) KCP() net.PacketConn { return d.side(KindKCP, d.kcp) }
func (d *Demux) UDP() net.PacketConn { return d.side(KindUDP, d.udp) }

func (d *Demux) loop() {
	buf := make([]byte, 65535)
	for {
		n, addr, err := d.pc.ReadFrom(buf)
		if err != nil {
			_ = d.Close()
			return
		}
		if n < 2 {
			continue
		}
		var side chan packet
		var pkt []byte
		switch buf[0] {
		case KindKCP:
			// KCP-side frames carry a 4-byte sequence after the discriminator,
			// used only to estimate the pipe's loss rate; it is stripped here.
			if n < 5 {
				continue
			}
			d.est.observe(binary.BigEndian.Uint32(buf[1:5]))
			side = d.kcp
			pkt = make([]byte, n-5)
			copy(pkt, buf[5:n])
		case KindUDP:
			side = d.udp
			pkt = make([]byte, n-1)
			copy(pkt, buf[1:n])
		default:
			continue
		}
		select {
		case side <- packet{b: pkt, addr: addr}:
		case <-d.done:
			return
		default:
			// Datagram semantics: drop under backpressure; KCP retransmits.
		}
	}
}

func (d *Demux) Close() error {
	d.once.Do(func() {
		close(d.done)
		_ = d.pc.Close()
	})
	return nil
}

func (s *sideConn) ReadFrom(b []byte) (int, net.Addr, error) {
	if s.ctx.Err() != nil {
		return 0, nil, net.ErrClosed
	}
	select {
	case p := <-s.in:
		return copy(b, p.b), p.addr, nil
	case <-s.ctx.Done():
		return 0, nil, net.ErrClosed
	case <-s.d.done:
		return 0, nil, net.ErrClosed
	case <-s.dl.Done():
		return 0, nil, os.ErrDeadlineExceeded
	}
}

type contextPacketWriter interface {
	WriteToContext(context.Context, []byte, net.Addr) (int, error)
}

func (s *sideConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	return s.WriteToContext(s.ctx, b, addr)
}

func (s *sideConn) WriteToContext(ctx context.Context, b []byte, addr net.Addr) (int, error) {
	if s.ctx.Err() != nil {
		return 0, net.ErrClosed
	}
	var out []byte
	if s.kind == KindKCP {
		out = make([]byte, len(b)+5)
		out[0] = s.kind
		binary.BigEndian.PutUint32(out[1:5], s.d.sendSeq.Add(1)-1)
		copy(out[5:], b)
	} else {
		out = make([]byte, len(b)+1)
		out[0] = s.kind
		copy(out[1:], b)
	}
	var err error
	if writer, ok := s.d.pc.(contextPacketWriter); ok {
		writeCtx, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(s.ctx, cancel)
		defer cancel()
		defer stop()
		_, err = writer.WriteToContext(writeCtx, out, addr)
	} else {
		_, err = s.d.pc.WriteTo(out, addr)
	}
	if err != nil {
		return 0, err
	}
	return len(b), nil
}

// LossRate is the smoothed loss estimate of the incoming KCP-side pipe, in
// [0,1], derived from gaps in the received sequence numbers.
func (d *Demux) LossRate() float64 { return d.est.rate() }

func (s *sideConn) Close() error                      { s.cancel(); return nil }
func (s *sideConn) LocalAddr() net.Addr               { return s.d.pc.LocalAddr() }
func (s *sideConn) SetDeadline(t time.Time) error     { s.dl.Set(t); return nil }
func (s *sideConn) SetReadDeadline(t time.Time) error { s.dl.Set(t); return nil }
func (s *sideConn) SetWriteDeadline(time.Time) error  { return nil }
