package dnstt

import (
	"context"
	"time"

	"github.com/net2share/vaydns/client"
	"github.com/net2share/vaydns/dns"
	"github.com/net2share/vaydns/noise"
	"github.com/net2share/vaydns/turbotunnel"
	"github.com/sagernet/sing/common/bufio"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/xtaci/kcp-go/v5"
	"github.com/xtaci/smux"
)

func (o *Outbound) newSession(ctx context.Context) (_ *smux.Session, err error) {
	conn, err := o.dialer.DialContext(ctx, N.NetworkUDP, o.resolver)
	if err != nil {
		return nil, E.Cause(err, "dial DNSTT resolver")
	}
	remote := M.SocksaddrFromNet(conn.RemoteAddr())
	if !remote.IsValid() {
		remote = o.resolver
	}
	// Invariant: The DNS transport and KCP must use the same queue key by address identity.
	packetAddr := remote.UDPAddr()
	transport := bufio.NewUnbindPacketConnWithAddr(conn, remote)
	packets := client.NewDNSPacketConn(transport, packetAddr, o.domain, nil, 253, 0, wireConfig, nil, dns.RRTypeTXT, 0, "")
	go func() {
		select {
		case <-packets.Closed():
		case <-packets.TransportErrors():
			packets.Close()
		}
		conn.Close()
	}()
	defer func() {
		if err != nil {
			packets.Close()
		}
	}()
	reliableConn, err := kcp.NewConn2(packetAddr, nil, 0, 0, packets)
	if err != nil {
		return nil, E.Cause(err, "open DNSTT KCP session")
	}
	defer func() {
		if err != nil {
			reliableConn.Close()
		}
	}()
	reliableConn.SetNoDelay(0, 0, 0, 1)
	reliableConn.SetWindowSize(turbotunnel.QueueSize/2, turbotunnel.QueueSize/2)
	if !reliableConn.SetMtu(o.mtu) {
		return nil, E.New("invalid DNSTT KCP MTU: ", o.mtu)
	}
	stopCancellation := context.AfterFunc(ctx, func() { reliableConn.Close() })
	secureConn, err := noise.NewClient(reliableConn, o.publicKey)
	stopCancellation()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, E.Cause(ctxErr, "DNSTT Noise handshake")
	}
	if err != nil {
		return nil, E.Cause(err, "DNSTT Noise handshake")
	}
	config := smux.DefaultConfig()
	config.Version = 2
	config.KeepAliveTimeout = 2 * time.Minute
	config.MaxStreamBuffer = 1024 * 1024
	mux, err := smux.Client(secureConn, config)
	if err != nil {
		return nil, E.Cause(err, "open DNSTT smux session")
	}
	go func() {
		select {
		case <-mux.CloseChan():
		case <-packets.Closed():
			mux.Close()
		}
		packets.Close()
	}()
	return mux, nil
}
