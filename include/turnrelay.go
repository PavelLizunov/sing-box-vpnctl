//go:build with_turnrelay

package include

import (
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/protocol/turnrelay"
)

func registerTurnRelay(registry *outbound.Registry) {
	turnrelay.RegisterOutbound(registry)
}
