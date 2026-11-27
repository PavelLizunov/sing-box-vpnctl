package option

import "github.com/sagernet/sing/common/auth"

type TrustTunnelInboundOptions struct {
	ListenOptions
	InboundTLSOptionsContainer
	Users []auth.User `json:"users"`
	UDP   *bool       `json:"udp,omitempty"`
	TrustTunnelQUICOptions
}

type TrustTunnelOutboundOptions struct {
	DialerOptions
	ServerOptions
	OutboundTLSOptionsContainer
	Username    string `json:"username"`
	Password    string `json:"password"`
	HealthCheck bool   `json:"health_check,omitempty"`
	UDP         *bool  `json:"udp,omitempty"`
	TrustTunnelQUICOptions
}

type TrustTunnelQUICOptions struct {
	QUIC                  bool   `json:"quic,omitempty"`
	QUICCongestionControl string `json:"quic_congestion_control,omitempty" enum:"bbr,cubic,reno"`
}
