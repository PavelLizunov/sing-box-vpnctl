//go:build with_trusttunnel && !with_quic

package trusttunnel

import (
	"context"

	TT "github.com/xchacha20-poly1305/sing-trusttunnel"
)

func (o *Outbound) newQUICTransport(context.Context) (TT.RoundTripper, func() error) {
	panic("TrustTunnel QUIC options bypassed validation")
}
