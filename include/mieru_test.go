//go:build !with_mieru

package include

import (
	"context"
	"testing"

	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/stretchr/testify/require"
)

func TestMieruAbsentByDefault(t *testing.T) {
	logger := log.NewNOPFactory().Logger()

	outboundOptions, loaded := OutboundRegistry().CreateOptions("mieru")
	require.True(t, loaded, "the default build must still recognise the mieru outbound type")
	require.IsType(t, &option.MieruOutboundOptions{}, outboundOptions)
	_, err := OutboundRegistry().CreateOutbound(context.Background(), nil, logger, "mieru-out", "mieru", outboundOptions)
	require.Error(t, err)
	require.Contains(t, err.Error(), mieruNotIncludedMessage)

	inboundOptions, loaded := InboundRegistry().CreateOptions("mieru")
	require.True(t, loaded, "the default build must still recognise the mieru inbound type")
	require.IsType(t, &option.MieruInboundOptions{}, inboundOptions)
	_, err = InboundRegistry().Create(context.Background(), nil, logger, "mieru-in", "mieru", inboundOptions)
	require.Error(t, err)
	require.Contains(t, err.Error(), mieruNotIncludedMessage)
}
