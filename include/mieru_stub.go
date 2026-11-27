//go:build !with_mieru

package include

import (
	"context"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
)

const mieruNotIncludedMessage = "mieru is not included in this build, rebuild with -tags with_mieru"

func registerMieruInbound(registry *inbound.Registry) {
	inbound.Register[option.MieruInboundOptions](registry, option.MieruType, func(context.Context, adapter.Router, log.ContextLogger, string, option.MieruInboundOptions) (adapter.Inbound, error) {
		return nil, E.New(mieruNotIncludedMessage)
	})
}

func registerMieruOutbound(registry *outbound.Registry) {
	outbound.Register[option.MieruOutboundOptions](registry, option.MieruType, func(context.Context, adapter.Router, log.ContextLogger, string, option.MieruOutboundOptions) (adapter.Outbound, error) {
		return nil, E.New(mieruNotIncludedMessage)
	})
}
