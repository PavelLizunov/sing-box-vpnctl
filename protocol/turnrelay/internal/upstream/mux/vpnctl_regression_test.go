package mux

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestServerClosesBeforeAndAfterAuth(t *testing.T) {
	s := NewServer(ServerOptions{Password: "secret"})
	defer s.Close()
	preAuth, pending := net.Pipe()
	defer pending.Close()
	auth, client := net.Pipe()
	defer client.Close()
	done := make(chan error, 2)
	go func() { done <- s.Handle(t.Context(), preAuth) }()
	go func() { done <- s.Handle(t.Context(), auth) }()
	require.NoError(t, pending.SetDeadline(time.Now().Add(time.Second)))
	require.NoError(t, client.SetDeadline(time.Now().Add(time.Second)))
	var session SessionAddr
	_, err := pending.Write(EncodeHello(session))
	require.NoError(t, err)
	_, err = client.Write(EncodeHello(session))
	require.NoError(t, err)
	_, err = client.Write(EncodeAuth(AuthTag("secret", session)))
	require.NoError(t, err)
	pc := s.PacketConn()
	require.Eventually(t, func() bool {
		_, err := pc.WriteTo([]byte{1}, session)
		return err == nil
	}, time.Second, time.Millisecond)
	closed := make(chan struct{})
	go func() {
		_ = s.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("shutdown blocked")
	}
	for range 2 {
		require.Error(t, <-done)
	}
}
