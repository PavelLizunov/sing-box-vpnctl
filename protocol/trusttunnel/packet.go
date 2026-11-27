//go:build with_trusttunnel

package trusttunnel

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/sagernet/sing-box/adapter"
	M "github.com/sagernet/sing/common/metadata"
)

const (
	packetAddressSize = 16 + 2
	packetHeaderSize  = 4 + 2*packetAddressSize
	maxPacketPayload  = 65535
)

type packetConn struct {
	stream    *streamConn
	dnsRouter adapter.DNSRouter
	readMu    sync.Mutex
}

func (c *packetConn) ReadFrom(p []byte) (int, net.Addr, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	var header [packetHeaderSize]byte
	if _, err := io.ReadFull(c.stream, header[:]); err != nil {
		return 0, nil, err
	}
	length := binary.BigEndian.Uint32(header[:4])
	if length < 2*packetAddressSize || length > 2*packetAddressSize+maxPacketPayload {
		_ = c.Close()
		return 0, nil, errors.New("invalid TrustTunnel UDP frame size")
	}
	payloadLen := int(length) - 2*packetAddressSize
	ip := netip.AddrFrom16([16]byte(header[4:20])).Unmap()
	if ip != netip.IPv6Loopback() && ([12]byte(header[4:16]) == [12]byte{}) {
		ip = netip.AddrFrom4([4]byte(header[16:20]))
	}
	source := M.SocksaddrFrom(ip, binary.BigEndian.Uint16(header[20:22]))
	n, err := io.ReadFull(c.stream, p[:min(payloadLen, len(p))])
	if err != nil {
		return n, source, err
	}
	if n < payloadLen {
		_, err = io.CopyN(io.Discard, c.stream, int64(payloadLen-n))
	}
	return n, source, err
}

func (c *packetConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	destination := M.SocksaddrFromNet(addr).Unwrap()
	if destination.IsFqdn() && c.dnsRouter != nil {
		addresses, err := c.dnsRouter.Lookup(c.stream.ctx, destination.Fqdn, adapter.DNSQueryOptions{})
		if err != nil {
			return 0, err
		}
		if len(addresses) > 0 {
			destination.Addr = addresses[0].Unmap()
		}
	}
	if !destination.IsIP() {
		return 0, errors.New("TrustTunnel UDP requires an IP destination")
	}
	if len(p) > maxPacketPayload {
		return 0, errors.New("TrustTunnel UDP payload is too large")
	}
	// Client frames have one extra byte for the length of an optional app name.
	frame := make([]byte, packetHeaderSize+1+len(p))
	binary.BigEndian.PutUint32(frame[:4], uint32(2*packetAddressSize+1+len(p)))
	if destination.Addr.Is4() {
		ip := destination.Addr.As4()
		copy(frame[34:38], ip[:])
	} else {
		ip := destination.Addr.As16()
		copy(frame[22:38], ip[:])
	}
	binary.BigEndian.PutUint16(frame[38:40], destination.Port)
	copy(frame[41:], p)
	n, err := c.stream.Write(frame)
	return max(0, n-packetHeaderSize-1), err
}

func (c *packetConn) Close() error                       { return c.stream.Close() }
func (c *packetConn) LocalAddr() net.Addr                { return c.stream.LocalAddr() }
func (c *packetConn) SetDeadline(t time.Time) error      { return c.stream.SetDeadline(t) }
func (c *packetConn) SetReadDeadline(t time.Time) error  { return c.stream.SetReadDeadline(t) }
func (c *packetConn) SetWriteDeadline(t time.Time) error { return c.stream.SetWriteDeadline(t) }
