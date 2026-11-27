package include

import (
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/protocol/dnstt"
)

func registerVPNCTLOutbounds(registry *outbound.Registry) {
	dnstt.RegisterOutbound(registry)
	registerTURN(registry)
	registerTurnRelay(registry)
	registerTrustTunnelOutbound(registry)
}
