package option

import "net/netip"

type VPNCTLUserBandwidthOptions struct {
	BitsPerSecond     uint32                           `json:"bits_per_second"`
	UserBitsPerSecond map[string]uint32                `json:"user_bits_per_second,omitempty"`
	WireGuardPeers    map[string]map[netip.Addr]string `json:"wireguard_peers,omitempty"`
}
