//go:build with_turnrelay

package turnrelay

import (
	"context"
	"net"
	"os"
	"sync"
	"time"

	"github.com/sagernet/sing-box/protocol/turnrelay/internal/upstream/mux"
)

type relayPipe struct {
	pool   *mux.Pool
	peer   *net.UDPAddr
	mu     sync.Mutex
	rctx   context.Context
	rstop  context.CancelFunc
	wctx   context.Context
	wstop  context.CancelFunc
	closed chan struct{}
	once   sync.Once
}

func newRelayPipe(pool *mux.Pool, peer *net.UDPAddr) *relayPipe {
	p := &relayPipe{pool: pool, peer: peer, closed: make(chan struct{})}
	p.rctx, p.rstop = context.WithCancel(context.Background())
	p.wctx, p.wstop = context.WithCancel(context.Background())
	return p
}

func (p *relayPipe) Read(b []byte) (int, error) {
	select {
	case <-p.closed:
		return 0, net.ErrClosed
	default:
	}
	p.mu.Lock()
	ctx := p.rctx
	p.mu.Unlock()
	packet, err := p.pool.Read(ctx)
	if err != nil {
		select {
		case <-p.closed:
			return 0, net.ErrClosed
		default:
		}
		if ctx.Err() != nil {
			return 0, os.ErrDeadlineExceeded
		}
		return 0, err
	}
	return copy(b, packet), nil
}

func (p *relayPipe) Write(b []byte) (int, error) {
	select {
	case <-p.closed:
		return 0, net.ErrClosed
	default:
	}
	if err := p.pool.Write(p.wctx, b); err != nil {
		select {
		case <-p.closed:
			return 0, net.ErrClosed
		default:
		}
		return 0, err
	}
	return len(b), nil
}

func (p *relayPipe) Close() error {
	p.once.Do(func() {
		close(p.closed)
		p.wstop()
		p.mu.Lock()
		p.rstop()
		p.mu.Unlock()
	})
	return nil
}

func (p *relayPipe) LocalAddr() net.Addr  { return &net.UDPAddr{IP: net.IPv4zero} }
func (p *relayPipe) RemoteAddr() net.Addr { return p.peer }

func (p *relayPipe) SetDeadline(t time.Time) error { return p.SetReadDeadline(t) }

func (p *relayPipe) SetReadDeadline(t time.Time) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	select {
	case <-p.closed:
		return net.ErrClosed
	default:
	}
	p.rstop()
	if t.IsZero() {
		p.rctx, p.rstop = context.WithCancel(context.Background())
	} else {
		p.rctx, p.rstop = context.WithDeadline(context.Background(), t)
	}
	return nil
}

func (p *relayPipe) SetWriteDeadline(time.Time) error { return nil }

func (p *relayPipe) ReadFrom(b []byte) (int, net.Addr, error) {
	n, err := p.Read(b)
	return n, p.peer, err
}

func (p *relayPipe) WriteTo(b []byte, _ net.Addr) (int, error) { return p.Write(b) }
