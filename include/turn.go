//go:build with_turn

package include

import (
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/protocol/turn"
)

func registerTURN(registry *outbound.Registry) {
	turn.RegisterOutbound(registry)
}
