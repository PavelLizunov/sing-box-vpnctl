//go:build !with_turnrelay

package include

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTurnRelayOutboundAbsent(t *testing.T) {
	registry := OutboundRegistry()
	_, loaded := registry.CreateOptions("turnrelay")
	require.False(t, loaded)
}
