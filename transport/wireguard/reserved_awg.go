package wireguard

import "net/netip"

// Protocol: Clear reserved bytes only for configured endpoints because AWG uses all four message-type bytes.
func (c *ClientBind) reservedFrom(source netip.AddrPort) bool {
	c.reservedAccess.RLock()
	reserved, loaded := c.reservedForEndpoint[source]
	c.reservedAccess.RUnlock()
	if !loaded {
		reserved = c.reserved
	}
	return reserved != [3]uint8{}
}
