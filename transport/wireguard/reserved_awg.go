package wireguard

import "net/netip"

// reservedFrom reports whether datagrams from source carry reserved bytes that
// must be cleared before the device parses them. It mirrors the send side: the
// value registered for that endpoint, else the bind-wide one. Every other
// datagram is left untouched, because AWG uses the full four-byte message type.
func (c *ClientBind) reservedFrom(source netip.AddrPort) bool {
	c.reservedAccess.RLock()
	reserved, loaded := c.reservedForEndpoint[source]
	c.reservedAccess.RUnlock()
	if !loaded {
		reserved = c.reserved
	}
	return reserved != [3]uint8{}
}
