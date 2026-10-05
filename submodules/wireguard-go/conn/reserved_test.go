package conn

import (
	"net/netip"
	"sync"
	"testing"
)

func TestHasReservedForEndpoint(t *testing.T) {
	tests := []struct {
		name       string
		registered string
		queried    string
		want       bool
	}{
		{
			name:    "not registered",
			queried: "192.0.2.1:51820",
		},
		{
			name:       "registered exact",
			registered: "192.0.2.1:51820",
			queried:    "192.0.2.1:51820",
			want:       true,
		},
		{
			name:       "registered IPv4 queried mapped",
			registered: "192.0.2.1:51820",
			queried:    "[::ffff:192.0.2.1]:51820",
			want:       true,
		},
		{
			name:       "registered mapped queried IPv4",
			registered: "[::ffff:192.0.2.1]:51820",
			queried:    "192.0.2.1:51820",
			want:       true,
		},
		{
			name:       "different port",
			registered: "192.0.2.1:51820",
			queried:    "192.0.2.1:51821",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bind := NewStdNetBind(nil).(*StdNetBind)
			if tt.registered != "" {
				bind.SetReservedForEndpoint(netip.MustParseAddrPort(tt.registered), [3]byte{1, 2, 3})
			}
			if got := bind.hasReservedForEndpoint(netip.MustParseAddrPort(tt.queried)); got != tt.want {
				t.Fatalf("hasReservedForEndpoint(%q) = %v, want %v", tt.queried, got, tt.want)
			}
		})
	}
}

func TestHasReservedForEndpointConcurrent(t *testing.T) {
	bind := NewStdNetBind(nil).(*StdNetBind)
	destination := netip.MustParseAddrPort("192.0.2.1:51820")
	source := netip.MustParseAddrPort("[::ffff:192.0.2.1]:51820")
	bind.SetReservedForEndpoint(destination, [3]byte{1, 2, 3})

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 10000; i++ {
			bind.SetReservedForEndpoint(destination, [3]byte{1, 2, 3})
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for i := 0; i < 10000; i++ {
			bind.hasReservedForEndpoint(source)
		}
	}()
	close(start)
	wg.Wait()

	if !bind.hasReservedForEndpoint(source) {
		t.Fatal("reserved endpoint not found after concurrent access")
	}
}
