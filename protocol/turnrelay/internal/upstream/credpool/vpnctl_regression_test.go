package credpool_test

import (
	"testing"

	"github.com/sagernet/sing-box/protocol/turnrelay/internal/upstream/credpool"
	"github.com/sagernet/sing-box/protocol/turnrelay/internal/upstream/provider/static"
	"github.com/stretchr/testify/require"
)

func TestStaticRelayCanBeReacquiredAfterEviction(t *testing.T) {
	p := credpool.New(static.New("127.0.0.1:3478", "user", "pass"), credpool.Options{Links: []string{"static"}})
	lease, err := p.Acquire(t.Context(), 0)
	require.NoError(t, err)
	p.FailedRelay(lease)
	replacement, err := p.Acquire(t.Context(), 0)
	require.NoError(t, err)
	p.Release(replacement)
}
