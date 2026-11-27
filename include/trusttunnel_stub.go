//go:build !with_trusttunnel

package include

import (
	"context"
	"errors"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/adapter/outbound"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
)

var errTrustTunnelNotIncluded = errors.New("TrustTunnel is not included in this build, rebuild with -tags with_trusttunnel")

func registerTrustTunnelInbound(registry *inbound.Registry) {
	inbound.Register(registry, C.TypeTrustTunnel, func(context.Context, adapter.Router, log.ContextLogger, string, option.TrustTunnelInboundOptions) (adapter.Inbound, error) {
		return nil, errTrustTunnelNotIncluded
	})
}

func registerTrustTunnelOutbound(registry *outbound.Registry) {
	outbound.Register(registry, C.TypeTrustTunnel, func(context.Context, adapter.Router, log.ContextLogger, string, option.TrustTunnelOutboundOptions) (adapter.Outbound, error) {
		return nil, errTrustTunnelNotIncluded
	})
}
