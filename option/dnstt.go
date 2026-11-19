package option

type DNSTTOutboundOptions struct {
	DialerOptions
	ServerOptions
	Domain    string `json:"domain"`
	PublicKey string `json:"public_key"`
}
