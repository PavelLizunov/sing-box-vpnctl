package exit

import (
	"context"
	"net"
	"testing"
	"time"

	M "github.com/sagernet/sing/common/metadata"
	"github.com/stretchr/testify/require"
)

func TestKCPReconnectClosesOnlyOldFEC(t *testing.T) {
	left, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	right, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = left.Close(); _ = right.Close() })
	client := NewClient(left, right.LocalAddr(), ClientOptions{})
	defer client.Close()
	remote := NewDemux(right)
	defer remote.Close()
	udp := newRealtimeFECConn(remote.UDP(), func() float64 { return 0 })
	defer udp.Close()
	peer := M.ParseSocksaddr("127.0.0.1:1234")
	assoc, err := client.ListenPacket(t.Context(), peer)
	require.NoError(t, err)
	defer assoc.Close()
	first, err := client.session()
	require.NoError(t, err)
	previous := client.kcpPipe
	require.NoError(t, first.Close())
	second, err := client.session()
	require.NoError(t, err)
	require.NotSame(t, first, second)
	select {
	case <-previous.done:
	default:
		t.Fatal("old FEC reader was left running")
	}
	frame, err := EncodeUDPFrame(assoc.(*clientAssoc).id, peer, []byte("after reconnect"))
	require.NoError(t, err)
	_, err = udp.WriteTo(prependSeq(0, frame), left.LocalAddr())
	require.NoError(t, err)
	require.NoError(t, assoc.SetReadDeadline(time.Now().Add(time.Second)))
	reply := make([]byte, 64)
	n, from, err := assoc.ReadFrom(reply)
	require.NoError(t, err)
	require.Equal(t, peer.String(), from.String())
	require.Equal(t, "after reconnect", string(reply[:n]))
}

func TestClientFlushesQuietUDPGap(t *testing.T) {
	socket, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	remote, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer remote.Close()
	client := NewClient(socket, remote.LocalAddr(), ClientOptions{})
	defer client.Close()
	demux := NewDemux(remote)
	defer demux.Close()
	fec := newRealtimeFECConn(demux.UDP(), func() float64 { return 0 })
	defer fec.Close()
	pc, err := client.ListenPacket(t.Context(), M.Socksaddr{})
	require.NoError(t, err)
	defer pc.Close()
	peer := M.ParseSocksaddr("192.0.2.1:53")
	for _, seq := range []uint32{0, 2, 4} {
		frame, err := EncodeUDPFrame(pc.(*clientAssoc).id, peer, []byte{byte(seq)})
		require.NoError(t, err)
		_, err = fec.WriteTo(prependSeq(seq, frame), socket.LocalAddr())
		require.NoError(t, err)
	}
	require.NoError(t, pc.SetReadDeadline(time.Now().Add(time.Second)))
	for _, want := range []byte{0, 2, 4} {
		var b [1]byte
		n, addr, err := pc.ReadFrom(b[:])
		require.NoError(t, err)
		require.Equal(t, 1, n)
		require.Equal(t, peer.String(), addr.String())
		require.Equal(t, want, b[0])
	}
	_, err = pc.WriteTo(make([]byte, 65507), peer)
	require.ErrorIs(t, err, errUDPFrameSize)
	require.NoError(t, pc.Close())
	_, err = pc.WriteTo([]byte{1}, peer)
	require.ErrorIs(t, err, net.ErrClosed)
	require.NoError(t, client.Close())
	_, err = client.ListenPacket(t.Context(), peer)
	require.ErrorIs(t, err, net.ErrClosed)
}

type sessionPacketConn struct {
	net.PacketConn
	ctx context.Context
}

func (p sessionPacketConn) SessionContext(net.Addr) (context.Context, bool) {
	return p.ctx, true
}

func TestServerBoundsAndReleasesSessionUDPAssociations(t *testing.T) {
	transport, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, expire := context.WithCancel(t.Context())
	defer expire()
	server := NewServer(sessionPacketConn{PacketConn: transport, ctx: ctx}, ServerOptions{MaxAssociations: 1})
	defer server.Close()
	peer := M.ParseSocksaddr("192.0.2.10:1234").UDPAddr()
	add := func(id uint16) (*assoc, error) {
		server.amu.Lock()
		defer server.amu.Unlock()
		return server.assocFor(t.Context(), udpFlow{id: id, session: peer.String()}, peer)
	}
	a, err := add(1)
	require.NoError(t, err)
	_, err = add(2)
	require.ErrorContains(t, err, "association limit")
	expire()
	require.Eventually(t, func() bool {
		server.amu.Lock()
		defer server.amu.Unlock()
		return len(server.assocs) == 0
	}, time.Second, time.Millisecond)
	_, err = a.pc.WriteTo([]byte{1}, peer)
	require.ErrorIs(t, err, net.ErrClosed)
	_, err = add(2)
	require.ErrorIs(t, err, net.ErrClosed)
}

func TestClientAssociationIDExhaustion(t *testing.T) {
	client := &Client{assocs: make(map[uint16]*clientAssoc, 1<<16)}
	for id := range 1 << 16 {
		client.assocs[uint16(id)] = nil
	}
	_, err := client.ListenPacket(t.Context(), M.Socksaddr{})
	require.ErrorContains(t, err, "IDs exhausted")
}

func TestServerFlushesQuietUDPGap(t *testing.T) {
	local, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	remote, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer remote.Close()
	target, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer target.Close()
	server := NewServer(local, ServerOptions{AllowPrivate: true})
	serve := make(chan error, 1)
	ctx, cancel := context.WithCancel(t.Context())
	defer func() {
		cancel()
		<-serve
	}()
	go func() { serve <- server.Serve(ctx) }()
	demux := NewDemux(remote)
	defer demux.Close()
	fec := newRealtimeFECConn(demux.UDP(), func() float64 { return 0 })
	defer fec.Close()
	for _, seq := range []uint32{0, 2} {
		frame, err := EncodeUDPFrame(1, M.SocksaddrFromNet(target.LocalAddr()), []byte{byte(seq)})
		require.NoError(t, err)
		_, err = fec.WriteTo(prependSeq(seq, frame), local.LocalAddr())
		require.NoError(t, err)
	}
	require.NoError(t, target.SetReadDeadline(time.Now().Add(time.Second)))
	for _, want := range []byte{0, 2} {
		var buf [1]byte
		n, _, err := target.ReadFrom(buf[:])
		require.NoError(t, err)
		require.Equal(t, 1, n)
		require.Equal(t, want, buf[0])
	}
}

func TestSequencerKeepsActiveFlowAcrossTablePressure(t *testing.T) {
	seq := newFlowSequencer()
	active := udpFlow{peer: "active"}
	first, ok := seq.next(active)
	require.True(t, ok)
	for id := range defaultMaxFlows - 1 {
		_, ok := seq.next(udpFlow{id: uint16(id), peer: "other"})
		require.True(t, ok)
	}
	_, ok = seq.next(udpFlow{peer: "overflow"})
	require.False(t, ok)
	next, ok := seq.next(active)
	require.True(t, ok)
	require.Equal(t, first+1, next)
}
