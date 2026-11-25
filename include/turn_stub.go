//go:build !with_turn

package include

import "github.com/sagernet/sing-box/adapter/outbound"

func registerTURN(*outbound.Registry) {}
