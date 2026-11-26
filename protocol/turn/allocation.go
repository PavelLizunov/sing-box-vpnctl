//go:build with_turn

package turn

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/pion/logging"
	"github.com/pion/transport/v5/stdnet"
	"github.com/pion/turn/v5"
	M "github.com/sagernet/sing/common/metadata"
)

type allocation struct {
	net.PacketConn
	stream        net.Conn
	done          chan struct{}
	ipv6          bool
	readMu        sync.Mutex
	mu            sync.Mutex
	readDeadline  time.Time
	writeDeadline time.Time
	writeTimer    *time.Timer
	writers       int
	writeTimedOut bool
}

func allocate(ctx context.Context, stream net.Conn, username, password string, ipv6 bool) (*allocation, error) {
	family := turn.RequestedAddressFamilyIPv4
	if ipv6 {
		family = turn.RequestedAddressFamilyIPv6
	}
	peer := M.SocksaddrFromNet(stream.RemoteAddr())
	if !peer.IsIP() {
		// Invariant: a detour may hide the relay IP. Pion writes only to the already connected TLS stream.
		peer = M.ParseSocksaddr("0.0.0.0:0")
	}
	client, err := turn.NewClient(&turn.ClientConfig{
		TURNServerAddr: peer.String(), Conn: turn.NewSTUNConn(stream),
		Username: username, Password: password, RequestedAddressFamily: family,
		Net: &stdnet.Net{}, LoggerFactory: &logging.DefaultLoggerFactory{Writer: io.Discard},
	})
	if err != nil {
		stream.Close()
		return nil, err
	}
	a := &allocation{stream: stream, done: make(chan struct{}), ipv6: ipv6}
	ready := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { stream.Close() })
	go func() {
		readFrames(stream, client)
		stream.Close()
		client.Close()
		<-ready
		if a.PacketConn != nil {
			a.PacketConn.Close()
		}
		a.mu.Lock()
		if a.writeTimer != nil {
			a.writeTimer.Stop()
		}
		close(a.done)
		a.mu.Unlock()
	}()
	a.PacketConn, err = client.AllocateWithContext(ctx)
	close(ready)
	if !stop() || err != nil {
		a.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	return a, nil
}

func readFrames(stream net.Conn, client *turn.Client) {
	var frame [65556]byte
	for {
		if _, err := io.ReadFull(stream, frame[:4]); err != nil {
			return
		}
		size := int(binary.BigEndian.Uint16(frame[2:4]))
		switch frame[0] >> 6 {
		case 0:
			size += 20
		case 1:
			// Protocol: ChannelData over TLS is padded to four bytes, including frames shorter than a STUN header.
			size = 4 + ((size + 3) &^ 3)
		default:
			return
		}
		if _, err := io.ReadFull(stream, frame[4:size]); err != nil {
			return
		}
		if _, err := client.HandleInbound(frame[:size], stream.RemoteAddr()); err != nil {
			return
		}
	}
}

func (a *allocation) Close() error {
	a.stream.Close()
	<-a.done
	return nil
}

func (a *allocation) WriteTo(p []byte, addr net.Addr) (int, error) {
	dst := M.SocksaddrFromNet(addr).Unwrap()
	if !dst.IsIP() || dst.IsIPv6() != a.ipv6 || dst.Port == 0 || dst.Addr.IsUnspecified() || dst.Addr.Zone() != "" {
		return 0, errors.New("TURN peer must have a numeric IP of the allocation family and a port")
	}
	// Protocol: a Send indication must fit its peer address, DATA and FINGERPRINT in a 16-bit STUN body.
	if len(p) > 65507 || (a.ipv6 && len(p) > 65496) {
		return 0, errors.New("TURN datagram too large")
	}
	a.mu.Lock()
	if expired(a.writeDeadline) || a.writeTimedOut {
		a.mu.Unlock()
		return 0, os.ErrDeadlineExceeded
	}
	a.writers++
	a.mu.Unlock()
	n, err := a.PacketConn.WriteTo(p, dst.UDPAddr())
	a.mu.Lock()
	a.writers--
	if err != nil && a.writeTimedOut {
		err = os.ErrDeadlineExceeded
	}
	a.mu.Unlock()
	return n, err
}

func (a *allocation) SetWriteDeadline(t time.Time) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	select {
	case <-a.done:
		return net.ErrClosed
	default:
	}
	a.writeDeadline = t
	if a.writeTimer != nil {
		a.writeTimer.Stop()
	}
	if !t.IsZero() {
		a.writeTimer = time.AfterFunc(time.Until(t), a.interruptWrite)
	}
	return nil
}

func expired(t time.Time) bool {
	return !t.IsZero() && !time.Now().Before(t)
}

func (a *allocation) interruptWrite() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.writers > 0 && expired(a.writeDeadline) {
		// Quirk: a TURN permission transaction can wait for a reply after the TLS write has completed.
		a.writeTimedOut = true
		a.stream.SetDeadline(time.Now())
	}
}

func (a *allocation) ReadFrom(p []byte) (int, net.Addr, error) {
	a.readMu.Lock()
	defer a.readMu.Unlock()
	a.mu.Lock()
	t := a.readDeadline
	a.mu.Unlock()
	if expired(t) {
		return 0, nil, os.ErrDeadlineExceeded
	}
	return a.PacketConn.ReadFrom(p)
}

func (a *allocation) SetReadDeadline(t time.Time) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.readDeadline = t
	return a.PacketConn.SetReadDeadline(t)
}

func (a *allocation) SetDeadline(t time.Time) error {
	if err := a.SetReadDeadline(t); err != nil {
		return err
	}
	return a.SetWriteDeadline(t)
}
