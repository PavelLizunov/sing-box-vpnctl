package option_test

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/trafficcontrol"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type bandwidthTestPacketConn struct {
	N.PacketConn
	size    int
	written int
}

func (c *bandwidthTestPacketConn) ReadPacket(buffer *buf.Buffer) (M.Socksaddr, error) {
	buffer.Extend(c.size)
	return M.Socksaddr{}, nil
}

func (c *bandwidthTestPacketConn) WritePacket(buffer *buf.Buffer, _ M.Socksaddr) error {
	c.written += buffer.Len()
	buffer.Release()
	return nil
}

func (c *bandwidthTestPacketConn) Close() error {
	return nil
}

func TestVPNCTLUserBandwidthSharesTCPAndDatagramBudget(t *testing.T) {
	const chunk = 16 * 1024
	bandwidth, err := trafficcontrol.NewUserBandwidth(option.VPNCTLUserBandwidthOptions{BitsPerSecond: 8})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	alice := adapter.InboundContext{User: "alice"}
	server, client := net.Pipe()
	tcp := bandwidth.RoutedConnection(ctx, server, alice, nil, nil)
	defer tcp.Close()
	defer client.Close()
	if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	go func() {
		_, _ = io.CopyN(io.Discard, client, chunk)
	}()
	if n, err := tcp.Write(make([]byte, chunk)); err != nil || n != chunk {
		t.Fatalf("TCP write: %d bytes, %v", n, err)
	}

	packet1 := &bandwidthTestPacketConn{size: chunk}
	udp1 := bandwidth.RoutedPacketConnection(ctx, packet1, alice, nil, nil)
	defer udp1.Close()
	packet2 := &bandwidthTestPacketConn{size: chunk}
	udp2 := bandwidth.RoutedPacketConnection(ctx, packet2, alice, nil, nil)
	defer udp2.Close()
	bob := bandwidth.RoutedPacketConnection(ctx, &bandwidthTestPacketConn{size: chunk}, adapter.InboundContext{User: "bob"}, nil, nil)
	defer bob.Close()

	buffer := buf.NewSize(chunk)
	defer buffer.Release()
	if _, err := udp1.ReadPacket(buffer); err != nil || buffer.Len() != chunk {
		t.Fatalf("UDP read: %d bytes, %v", buffer.Len(), err)
	}
	writePacket := func(conn N.PacketConn) error {
		datagram := buf.NewSize(chunk)
		datagram.Extend(chunk)
		return conn.WritePacket(datagram, M.Socksaddr{})
	}
	for _, conn := range []N.PacketConn{udp1, udp2} {
		if err := writePacket(conn); err != nil {
			t.Fatal(err)
		}
	}
	if err := writePacket(udp2); err == nil {
		t.Fatal("TCP and UDP reads and writes must exhaust Alice's shared burst")
	}
	if packet1.written != chunk || packet2.written != chunk {
		t.Fatalf("expected one forwarded datagram per connection, got %d and %d bytes", packet1.written, packet2.written)
	}
	if err := writePacket(bob); err != nil {
		t.Fatalf("Alice's exhausted burst must not throttle Bob: %v", err)
	}
}
