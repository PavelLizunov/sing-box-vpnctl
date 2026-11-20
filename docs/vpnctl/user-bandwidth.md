# User bandwidth

The optional route setting `vpnctl_user_bandwidth` caps the aggregate TCP
and UDP payload rate for each authenticated inbound user on this process.
Matching user names share a budget across connections and protocols, with one
64 KiB burst allowance. Set `bits_per_second` to 40000000 for 40 Mbit/s.
This budget is shared between upload and download.

For userspace WireGuard and AmneziaWG endpoints that send TCP and UDP through
the router, supply `wireguard_peers` as endpoint tag → assigned tunnel address
→ authenticated user name. Only exact peer addresses are supported. Map each
assigned IP family to the same name. Use host-prefix AllowedIPs for each peer.
WireGuard cryptokey routing enforces those source addresses. Peers missing
from the mapping share a separate budget for their endpoint.

This setting applies to traffic routed in this sing-box process. Anonymous
proxy sessions, ICMP, direct packet forwarding and other processes are outside
this setting. Keep forwarding TCP and UDP through a connection-based outbound.
