//go:build !with_turnrelay

package include

import "github.com/sagernet/sing-box/adapter/outbound"

func registerTurnRelay(*outbound.Registry) {}
