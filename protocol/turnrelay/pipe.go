//go:build with_turnrelay

package turnrelay

import (
	"context"
	"errors"
	"net"
	"os"
	"sync"
	"time"

	"github.com/sagernet/sing-box/protocol/turnrelay/internal/upstream/mux"
)

type relayPipe struct {
	pool  *mux.Pool
	peer  *net.UDPAddr
	ctx   context.Context
	stop  context.CancelFunc
	read  pipeDeadline
	write pipeDeadline
}

type pipeDeadline struct {
	mu   sync.Mutex
	ctx  context.Context
	stop context.CancelFunc
}

func (d *pipeDeadline) set(parent context.Context, at time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stop != nil {
		d.stop()
	}
	if at.IsZero() {
		d.ctx, d.stop = context.WithCancel(parent)
	} else {
		d.ctx, d.stop = context.WithDeadline(parent, at)
	}
}

func (d *pipeDeadline) run(parent, caller context.Context, io func(context.Context) error) error {
	for {
		d.mu.Lock()
		deadline := d.ctx
		d.mu.Unlock()
		err := runPipeIO(deadline, caller, io)
		if err == nil {
			return nil
		}
		if parent.Err() != nil {
			return net.ErrClosed
		}
		if caller != nil && caller.Err() != nil {
			return caller.Err()
		}
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		d.mu.Lock()
		same := d.ctx == deadline
		d.mu.Unlock()
		if same {
			return os.ErrDeadlineExceeded
		}
	}
}

func runPipeIO(deadline, caller context.Context, io func(context.Context) error) error {
	if caller == nil {
		return io(deadline)
	}
	if err := caller.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(deadline)
	defer cancel()
	stop := context.AfterFunc(caller, cancel)
	defer stop()
	return io(ctx)
}

func newRelayPipe(pool *mux.Pool, peer *net.UDPAddr) *relayPipe {
	p := &relayPipe{pool: pool, peer: peer}
	p.ctx, p.stop = context.WithCancel(context.Background())
	p.read.set(p.ctx, time.Time{})
	p.write.set(p.ctx, time.Time{})
	return p
}

func (p *relayPipe) Read(b []byte) (int, error) {
	var packet []byte
	err := p.read.run(p.ctx, nil, func(ctx context.Context) error {
		var err error
		packet, err = p.pool.Read(ctx)
		return err
	})
	return copy(b, packet), err
}

func (p *relayPipe) Write(b []byte) (int, error) {
	return p.WriteToContext(nil, b, p.peer)
}

func (p *relayPipe) WriteToContext(ctx context.Context, b []byte, _ net.Addr) (int, error) {
	err := p.write.run(p.ctx, ctx, func(ctx context.Context) error { return p.pool.Write(ctx, b) })
	if err != nil {
		return 0, err
	}
	return len(b), nil
}

func (p *relayPipe) Close() error {
	p.stop()
	return nil
}

func (p *relayPipe) LocalAddr() net.Addr  { return &net.UDPAddr{IP: net.IPv4zero} }
func (p *relayPipe) RemoteAddr() net.Addr { return p.peer }

func (p *relayPipe) SetDeadline(t time.Time) error {
	if err := p.SetReadDeadline(t); err != nil {
		return err
	}
	return p.SetWriteDeadline(t)
}

func (p *relayPipe) SetReadDeadline(t time.Time) error {
	if p.ctx.Err() != nil {
		return net.ErrClosed
	}
	p.read.set(p.ctx, t)
	return nil
}

func (p *relayPipe) SetWriteDeadline(t time.Time) error {
	if p.ctx.Err() != nil {
		return net.ErrClosed
	}
	p.write.set(p.ctx, t)
	return nil
}

func (p *relayPipe) ReadFrom(b []byte) (int, net.Addr, error) {
	n, err := p.Read(b)
	return n, p.peer, err
}

func (p *relayPipe) WriteTo(b []byte, _ net.Addr) (int, error) { return p.Write(b) }
