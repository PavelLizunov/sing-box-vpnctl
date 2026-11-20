package trafficcontrol

import (
	"context"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"
)

func TestVPNCTLUserBandwidthUsesAuthenticatedIdentity(t *testing.T) {
	address := netip.MustParseAddr("192.0.2.5")
	bandwidth, err := NewUserBandwidth(option.VPNCTLUserBandwidthOptions{
		BitsPerSecond: 40_000_000,
		WireGuardPeers: map[string]map[netip.Addr]string{
			"awg3-in": {address: "alice"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	alice := bandwidth.limiter(adapter.InboundContext{InboundType: "vless", User: "alice"})
	otherInbound := bandwidth.limiter(adapter.InboundContext{InboundType: "tuic", User: "alice"})
	wireGuard := bandwidth.limiter(adapter.InboundContext{
		InboundType: "wireguard",
		Inbound:     "awg3-in",
		Source:      M.Socksaddr{Addr: address},
	})
	if alice == nil || alice != otherInbound || alice != wireGuard {
		t.Fatal("flows for the same authenticated user must share one budget")
	}
	bob := bandwidth.limiter(adapter.InboundContext{InboundType: "hysteria2", User: "bob"})
	if bob == nil || bob == alice {
		t.Fatal("different users must have independent budgets")
	}
	unmapped := bandwidth.limiter(adapter.InboundContext{InboundType: "wireguard", Inbound: "unmapped", Source: M.Socksaddr{Addr: address}})
	otherPeer := bandwidth.limiter(adapter.InboundContext{
		InboundType: "wireguard",
		Inbound:     "unmapped",
		Source:      M.Socksaddr{Addr: netip.MustParseAddr("2001:db8::2")},
	})
	if unmapped == nil || unmapped != otherPeer || unmapped == alice {
		t.Fatal("unmapped WireGuard peers must share their endpoint's independent fallback budget")
	}
	missingPeer := bandwidth.limiter(adapter.InboundContext{InboundType: "wireguard", Inbound: "awg3-in"})
	if missingPeer == nil || missingPeer == unmapped || missingPeer == alice {
		t.Fatal("a peer missing from an existing mapping must use that endpoint's fallback")
	}
	if bandwidth.limiter(adapter.InboundContext{User: "unmapped"}) == unmapped {
		t.Fatal("an account and an endpoint with the same name must not share a budget")
	}
	if bandwidth.limiter(adapter.InboundContext{}) != nil {
		t.Fatal("sessions without an authenticated identity must not share a budget")
	}
}

func TestVPNCTLUserBandwidthCapsParallelStreams(t *testing.T) {
	const rate = 40_000_000
	const transferSize = 512 * 1024
	bandwidth, err := NewUserBandwidth(option.VPNCTLUserBandwidthOptions{BitsPerSecond: rate})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	data := make([]byte, transferSize)
	results := make(chan error, 2)
	start := time.Now()
	for _, inbound := range []string{"first", "second"} {
		server, client := net.Pipe()
		wrapped := bandwidth.RoutedConnection(ctx, server, adapter.InboundContext{Inbound: inbound, User: "alice"}, nil, nil)
		defer wrapped.Close()
		defer client.Close()
		go func() {
			_, writeErr := wrapped.Write(data)
			results <- writeErr
		}()
		go func() {
			_, _ = io.CopyN(io.Discard, client, transferSize)
		}()
	}
	for range 2 {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	minimum := time.Second * (2*transferSize - userBandwidthBurst) / (rate / 8)
	if elapsed := time.Since(start); elapsed < minimum-time.Millisecond {
		t.Fatalf("parallel flows completed in %v, beneath the shared budget floor %v", elapsed, minimum)
	}
}

func TestVPNCTLUserBandwidthRejectsInvalidConfiguration(t *testing.T) {
	for _, options := range []option.VPNCTLUserBandwidthOptions{
		{},
		{BitsPerSecond: 40_000_000, WireGuardPeers: map[string]map[netip.Addr]string{
			"awg-in": {netip.MustParseAddr("192.0.2.21"): ""},
		}},
	} {
		if _, err := NewUserBandwidth(options); err == nil {
			t.Fatal("configured bandwidth with an invalid identity or rate must be rejected")
		}
	}
}
