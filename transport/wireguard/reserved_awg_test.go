package wireguard

import (
	"net/netip"
	"sync"
	"testing"
)

func TestClientBindReservedFrom(t *testing.T) {
	registered := netip.MustParseAddrPort("192.0.2.1:51820")
	cleared := netip.MustParseAddrPort("192.0.2.2:51820")
	other := netip.MustParseAddrPort("192.0.2.3:51820")
	for _, tc := range []struct {
		name     string
		bindWide [3]uint8
		source   netip.AddrPort
		want     bool
	}{
		{"registered endpoint", [3]uint8{}, registered, true},
		{"other endpoint without bind-wide bytes", [3]uint8{}, other, false},
		{"other endpoint with bind-wide bytes", [3]uint8{7, 8, 9}, other, true},
		{"endpoint registered with zero bytes", [3]uint8{7, 8, 9}, cleared, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bind := &ClientBind{reservedForEndpoint: make(map[netip.AddrPort][3]uint8), reserved: tc.bindWide}
			bind.SetReservedForEndpoint(registered, [3]byte{1, 2, 3})
			bind.SetReservedForEndpoint(cleared, [3]byte{})
			if got := bind.reservedFrom(tc.source); got != tc.want {
				t.Fatalf("reservedFrom(%v) = %v, want %v", tc.source, got, tc.want)
			}
		})
	}
}

func TestClientBindReservedFromConcurrent(t *testing.T) {
	bind := &ClientBind{reservedForEndpoint: make(map[netip.AddrPort][3]uint8)}
	endpoint := netip.MustParseAddrPort("192.0.2.1:51820")
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 5000; i++ {
			bind.SetReservedForEndpoint(endpoint, [3]byte{1, 2, 3})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 5000; i++ {
			bind.reservedFrom(endpoint)
		}
	}()
	wg.Wait()
	if !bind.reservedFrom(endpoint) {
		t.Fatal("registered endpoint not found after concurrent access")
	}
}
