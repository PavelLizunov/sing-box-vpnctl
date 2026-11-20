package trafficcontrol

import (
	"encoding/json"
	"net/netip"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"

	"golang.org/x/time/rate"
)

const (
	tierTestDefaultBitsPerSecond  = 40_000_000
	tierTestAliceBitsPerSecond    = 8_000_000
	tierTestBobBitsPerSecond      = 24_000_000
	tierTestEndpointBitsPerSecond = 64_000_000
)

func requireTierTestRate(t *testing.T, limiter *rate.Limiter, bitsPerSecond uint32) {
	t.Helper()
	if limiter == nil {
		t.Fatal("expected a limiter")
	}
	want := rate.Limit(bitsPerSecond) / 8
	if got := limiter.Limit(); got != want {
		t.Fatalf("limiter rate = %v bytes/s, want %v bytes/s from %d bits/s", got, want, bitsPerSecond)
	}
}

func TestVPNCTLUserBandwidthAppliesPerUserTiers(t *testing.T) {
	aliceAddress := netip.MustParseAddr("192.0.2.10")
	bandwidth, err := NewUserBandwidth(option.VPNCTLUserBandwidthOptions{
		BitsPerSecond: tierTestDefaultBitsPerSecond,
		UserBitsPerSecond: map[string]uint32{
			"alice":   tierTestAliceBitsPerSecond,
			"bob":     tierTestBobBitsPerSecond,
			"awg3-in": tierTestEndpointBitsPerSecond,
		},
		WireGuardPeers: map[string]map[netip.Addr]string{
			"awg3-in": {aliceAddress: "alice"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	aliceProxy := bandwidth.limiter(adapter.InboundContext{InboundType: "vless", User: "alice"})
	requireTierTestRate(t, aliceProxy, tierTestAliceBitsPerSecond)
	if aliceOtherInbound := bandwidth.limiter(adapter.InboundContext{InboundType: "tuic", User: "alice"}); aliceOtherInbound != aliceProxy {
		t.Fatal("an override must apply to the exact user across every proxy inbound")
	}

	mappedPeer := bandwidth.limiter(adapter.InboundContext{
		InboundType: "wireguard",
		Inbound:     "awg3-in",
		Source:      M.Socksaddr{Addr: aliceAddress},
	})
	if mappedPeer != aliceProxy {
		t.Fatal("a mapped WireGuard peer must share the authenticated user's limiter")
	}

	bob := bandwidth.limiter(adapter.InboundContext{InboundType: "hysteria2", User: "bob"})
	requireTierTestRate(t, bob, tierTestBobBitsPerSecond)
	if bob == aliceProxy {
		t.Fatal("different overridden users must have distinct limiters")
	}

	unknown := bandwidth.limiter(adapter.InboundContext{InboundType: "vless", User: "carol"})
	requireTierTestRate(t, unknown, tierTestDefaultBitsPerSecond)
	if unknown == aliceProxy || unknown == bob {
		t.Fatal("a user without an override must not share an overridden user's limiter")
	}

	// The endpoint tag happens to match an override key, but an endpoint is not
	// an authenticated user: an unmapped peer on it still uses the default.
	endpointNamedUser := bandwidth.limiter(adapter.InboundContext{InboundType: "vless", User: "awg3-in"})
	requireTierTestRate(t, endpointNamedUser, tierTestEndpointBitsPerSecond)
	unmappedPeer := bandwidth.limiter(adapter.InboundContext{
		InboundType: "wireguard",
		Inbound:     "awg3-in",
		Source:      M.Socksaddr{Addr: netip.MustParseAddr("192.0.2.11")},
	})
	requireTierTestRate(t, unmappedPeer, tierTestDefaultBitsPerSecond)
	if unmappedPeer == endpointNamedUser {
		t.Fatal("an anonymous WireGuard peer must not share a named user's limiter")
	}
	if unmappedPeer == aliceProxy {
		t.Fatal("an unmapped WireGuard peer must not share a mapped user's limiter")
	}

	otherEndpointPeer := bandwidth.limiter(adapter.InboundContext{
		InboundType: "wireguard",
		Inbound:     "awg9-in",
		Source:      M.Socksaddr{Addr: netip.MustParseAddr("192.0.2.12")},
	})
	requireTierTestRate(t, otherEndpointPeer, tierTestDefaultBitsPerSecond)
}

func TestVPNCTLUserBandwidthUserTiersJSONField(t *testing.T) {
	var options option.VPNCTLUserBandwidthOptions
	if err := json.Unmarshal([]byte(`{
		"bits_per_second": 40000000,
		"user_bits_per_second": {"alice": 8000000}
	}`), &options); err != nil {
		t.Fatal(err)
	}
	if options.BitsPerSecond != tierTestDefaultBitsPerSecond {
		t.Fatalf("bits_per_second = %d, want %d", options.BitsPerSecond, tierTestDefaultBitsPerSecond)
	}
	if got := options.UserBitsPerSecond["alice"]; got != tierTestAliceBitsPerSecond {
		t.Fatalf("user_bits_per_second[alice] = %d, want %d", got, tierTestAliceBitsPerSecond)
	}

	var withoutTiers option.VPNCTLUserBandwidthOptions
	if err := json.Unmarshal([]byte(`{"bits_per_second": 40000000}`), &withoutTiers); err != nil {
		t.Fatal(err)
	}
	if len(withoutTiers.UserBitsPerSecond) != 0 {
		t.Fatalf("omitted user_bits_per_second must leave no overrides, got %v", withoutTiers.UserBitsPerSecond)
	}
}

func TestVPNCTLUserBandwidthRejectsInvalidUserTiers(t *testing.T) {
	for name, options := range map[string]option.VPNCTLUserBandwidthOptions{
		"empty user": {
			BitsPerSecond:     tierTestDefaultBitsPerSecond,
			UserBitsPerSecond: map[string]uint32{"": tierTestAliceBitsPerSecond},
		},
		"zero override": {
			BitsPerSecond:     tierTestDefaultBitsPerSecond,
			UserBitsPerSecond: map[string]uint32{"alice": 0},
		},
		"below minimum override": {
			BitsPerSecond:     tierTestDefaultBitsPerSecond,
			UserBitsPerSecond: map[string]uint32{"alice": 7},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewUserBandwidth(options); err == nil {
				t.Fatal("an invalid user bandwidth tier must be rejected")
			}
		})
	}

	bandwidth, err := NewUserBandwidth(option.VPNCTLUserBandwidthOptions{
		BitsPerSecond:     tierTestDefaultBitsPerSecond,
		UserBitsPerSecond: map[string]uint32{"alice": 8},
	})
	if err != nil {
		t.Fatalf("the minimum user override must be accepted: %v", err)
	}
	requireTierTestRate(t, bandwidth.limiter(adapter.InboundContext{User: "alice"}), 8)
}
