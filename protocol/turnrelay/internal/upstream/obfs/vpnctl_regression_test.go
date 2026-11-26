package obfs

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSRTPSourcePortReuse(t *testing.T) {
	l, err := Listen(ModeSRTP, "127.0.0.1:0", ListenOptions{})
	require.NoError(t, err)
	defer l.Close()
	w, err := New(ModeSRTP, Options{})
	require.NoError(t, err)
	address := "127.0.0.1:0"
	for range 2 {
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		pc, err := net.ListenPacket("udp4", address)
		require.NoError(t, err)
		defer pc.Close()
		address = pc.LocalAddr().String()
		c, err := w.Client(ctx, pc, l.Addr())
		require.NoError(t, err)
		server, err := l.Accept(ctx)
		cancel()
		require.NoError(t, err)
		require.NoError(t, server.Close())
		require.NoError(t, c.Close())
	}
}
