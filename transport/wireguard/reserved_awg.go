package wireguard

import "net/netip"

func (c *ClientBind) reservedFrom(source netip.AddrPort) bool {
	c.reservedAccess.RLock()
	reserved, loaded := c.reservedForEndpoint[source]
	c.reservedAccess.RUnlock()
	if !loaded {
		reserved = c.reserved
	}
	return reserved != [3]uint8{}
}
