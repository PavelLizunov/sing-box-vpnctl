# DNSTT

Available in sing-box-vpnctl. DNSTT transports a reliable stream in TXT queries over UDP to a DNS resolver. The original `dnstt-server` authenticates the tunnel with its Noise public key and connects each stream to its configured TCP backend. DNSTT uses no TLS or SNI.

### SSH over DNSTT

Set the SSH outbound as the route's final outbound. Configure `dnstt-server` to forward streams to the SSH service, and arrange for the resolver to send queries for the tunnel domain to `dnstt-server`.

```json
{
  "outbounds": [
    {
      "type": "dnstt",
      "tag": "dns-tunnel",
      "server": "192.0.2.53",
      "server_port": 53,
      "domain": "tunnel.example.net",
      "public_key": "<64 hex digits from dnstt-server>"
    },
    {
      "type": "ssh",
      "tag": "ssh-over-dnstt",
      "detour": "dns-tunnel",
      "server": "127.0.0.1",
      "server_port": 22,
      "user": "tunnel",
      "private_key_path": "/path/to/ssh-key",
      "host_key": ["<SSH server host public key>"]
    }
  ],
  "route": { "final": "ssh-over-dnstt" }
}
```

### Fields

| Field | Meaning |
|-------|---------|
| `server` | Required. Address of the UDP DNS resolver or a directly reachable `dnstt-server`. |
| `server_port` | DNS destination port. Defaults to `53`. |
| `domain` | Required. Tunnel domain configured on `dnstt-server`. |
| `public_key` | Required. The 32-byte Noise public key in hexadecimal, as emitted by `dnstt-server -gen-key`. |

[Dial Fields](https://sing-box.sagernet.org/configuration/shared/dial/) apply to the outer UDP socket. `dnstt` always opens streams to the TCP backend configured on the server. The SSH outbound in the example carries TCP connections to their final destinations. UDP application traffic is unsupported.
