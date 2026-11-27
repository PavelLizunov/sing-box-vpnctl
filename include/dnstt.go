package include

import (
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/protocol/dnstt"
)

func registerVPNCTLInbounds(registry *inbound.Registry) {
	registerTrustTunnelInbound(registry)
	registerMieruInbound(registry)
}

func registerVPNCTLOutbounds(registry *outbound.Registry) {
	dnstt.RegisterOutbound(registry)
	registerTURN(registry)
	registerTurnRelay(registry)
	registerTrustTunnelOutbound(registry)
	registerMieruOutbound(registry)
}
