package conn

import (
	"bytes"
	"errors"
	"net"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestStdNetBindSendIPv6AfterIPv4PooledAddress(t *testing.T) {
	listener, err := net.ListenUDP("udp6", &net.UDPAddr{IP: net.IPv6loopback})
	if err != nil {
		if errors.Is(err, syscall.EAFNOSUPPORT) || errors.Is(err, syscall.EADDRNOTAVAIL) {
			t.Skipf("IPv6 loopback unavailable: %v", err)
		}
		t.Fatal(err)
	}
	defer listener.Close()
	bind := NewStdNetBind(nil).(*StdNetBind)
	if _, _, err := bind.Open(0); err != nil {
		t.Fatal(err)
	}
	defer bind.Close()

	// Invariant: every pool allocation reproduces an address shortened by an IPv4 send.
	bind.udpAddrPool = sync.Pool{New: func() any {
		ip := bytes.Repeat([]byte{0xee}, net.IPv6len)
		return &net.UDPAddr{IP: ip[:net.IPv4len]}
	}}
	endpoint := &StdNetEndpoint{AddrPort: listener.LocalAddr().(*net.UDPAddr).AddrPort()}
	payload := []byte("wireguard-ipv6-regression")
	if err := bind.Send([][]byte{payload}, endpoint, 0); err != nil {
		t.Fatal(err)
	}
	if err := listener.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 1024)
	n, _, err := listener.ReadFromUDP(buffer)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buffer[:n], payload) {
		t.Fatalf("payload = %q, want %q", buffer[:n], payload)
	}
}
