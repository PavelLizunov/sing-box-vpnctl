//go:build with_mieru

package include

import (
	"context"
	"testing"

	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/stretchr/testify/require"
)

func TestMieruRegistered(t *testing.T) {
	logger := log.NewNOPFactory().Logger()

	outboundOptions, loaded := OutboundRegistry().CreateOptions("mieru")
	require.True(t, loaded)
	require.IsType(t, &option.MieruOutboundOptions{}, outboundOptions)
	_, err := OutboundRegistry().CreateOutbound(context.Background(), nil, logger, "mieru-out", "mieru", outboundOptions)
	require.ErrorContains(t, err, "mieru: missing server")

	inboundOptions, loaded := InboundRegistry().CreateOptions("mieru")
	require.True(t, loaded)
	require.IsType(t, &option.MieruInboundOptions{}, inboundOptions)
	_, err = InboundRegistry().Create(context.Background(), nil, logger, "mieru-in", "mieru", inboundOptions)
	require.ErrorContains(t, err, "mieru: inbound requires at least one user")
}
