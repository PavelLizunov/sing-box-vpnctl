package option

import "net/netip"

type VPNCTLUserBandwidthOptions struct {
	BitsPerSecond  uint32                           `json:"bits_per_second"`
	WireGuardPeers map[string]map[netip.Addr]string `json:"wireguard_peers,omitempty"`
}
