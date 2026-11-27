package option

const MieruType = "mieru"

type MieruPortBinding struct {
	Port      uint16 `json:"port,omitempty"`
	PortRange string `json:"port_range,omitempty"`
	Transport string `json:"transport,omitempty" enum:"tcp,udp"`
}

type MieruUser struct {
	UserName string `json:"username"`
	Password string `json:"password"`
}

type MieruInboundOptions struct {
	ListenOptions
	Transport    string             `json:"transport,omitempty" enum:"tcp,udp"`
	PortRange    string             `json:"port_range,omitempty"`
	PortBindings []MieruPortBinding `json:"port_bindings,omitempty"`
	Users        []MieruUser        `json:"users,omitempty"`
}

type MieruOutboundOptions struct {
	DialerOptions
	ServerOptions
	Transport     string             `json:"transport,omitempty" enum:"tcp,udp"`
	PortRange     string             `json:"port_range,omitempty"`
	PortBindings  []MieruPortBinding `json:"port_bindings,omitempty"`
	UserName      string             `json:"username"`
	Password      string             `json:"password"`
	Multiplexing  string             `json:"multiplexing,omitempty" enum:"default,off,low,middle,high"`
	HandshakeMode string             `json:"handshake_mode,omitempty" enum:"default,standard,no_wait"`
}
