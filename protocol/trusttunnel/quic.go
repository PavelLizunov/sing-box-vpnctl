//go:build with_trusttunnel && with_quic

package trusttunnel

import (
	"context"
	"crypto/tls"
	"time"

	"github.com/sagernet/quic-go"
	"github.com/sagernet/quic-go/http3"
	qtls "github.com/sagernet/sing-quic"
	congestion_meta1 "github.com/sagernet/sing-quic/congestion_meta1"
	congestion_meta2 "github.com/sagernet/sing-quic/congestion_meta2"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/common/ntp"

	TT "github.com/xchacha20-poly1305/sing-trusttunnel"
)

func (o *Outbound) newQUICTransport(session context.Context) (TT.RoundTripper, func() error) {
	timeFunc := ntp.TimeFuncFromContext(o.ctx)
	if timeFunc == nil {
		timeFunc = time.Now
	}
	transport := &http3.Transport{
		DisableCompression: true,
		QUICConfig: &quic.Config{
			Versions:                   []quic.Version{quic.Version1},
			HandshakeIdleTimeout:       o.tlsConfig.HandshakeTimeout(),
			MaxIdleTimeout:             TT.DefaultQuicMaxIdleTimeout,
			InitialStreamReceiveWindow: TT.DefaultQuicStreamReceiveWindow,
		},
		Dial: func(ctx context.Context, _ string, _ *tls.Config, config *quic.Config) (*quic.Conn, error) {
			rawConn, err := o.dialSession(ctx, session, N.NetworkUDP)
			if err != nil {
				return nil, err
			}
			conn, err := qtls.Dial(ctx, rawConn, o.tlsConfig, config)
			if err != nil {
				_ = rawConn.Close()
				return nil, err
			}
			context.AfterFunc(conn.Context(), func() { _ = rawConn.Close() })
			switch o.congestion {
			case "cubic", "reno":
				conn.SetCongestionControl(congestion_meta1.NewCubicSender(
					congestion_meta1.DefaultClock{TimeFunc: timeFunc}, conn.InitialPacketSize(), o.congestion == "reno",
				))
			default:
				conn.SetCongestionControl(congestion_meta2.NewBbrSenderWithProfile(conn.InitialPacketSize(), congestion_meta2.ProfileStandard))
			}
			return conn, nil
		},
	}
	return transport, transport.Close
}
