# sing-box-vpnctl

A fork of [sing-box](https://github.com/SagerNet/sing-box) that adds AmneziaWG and an XHTTP client transport. It is the engine of [VPNRouter](https://github.com/PavelLizunov/VPNRouter) and [vpnctl](https://github.com/PavelLizunov/vpnctl), and a drop-in sing-box for anyone who needs these two protocols.

**[Download the latest release](https://github.com/PavelLizunov/sing-box-vpnctl/releases/latest)** · [All releases](https://github.com/PavelLizunov/sing-box-vpnctl/releases)

Platforms: Linux (amd64, arm64, armv7), Windows (amd64, arm64), macOS (universal), Android (library).

[Русская версия](README.ru.md)

## What the fork adds

- **AmneziaWG 2.0 and 3.1** in the WireGuard endpoint: junk packets, prefixes, magic header ranges, signature packets with protocol masquerade, random trailers, header protection, content padding, rekey interval. See [AmneziaWG options](docs/vpnctl/awg.md).
- **XHTTP client transport** (Xray "splithttp") for VLESS and similar outbounds: packet-up, stream-up and stream-one modes, connection reuse with limits. See [XHTTP options](docs/vpnctl/xhttp.md).
- The `user` field in the Clash API `/connections` output, and the V2Ray stats API in release builds.

Everything else is sing-box, unchanged.

## Quick start

1. Download the archive for your platform from the latest release and unpack it.
2. Check a configuration: `sing-box check -c config.json`
3. Run it: `sing-box run -c config.json`

The configuration format is the [upstream one](https://sing-box.sagernet.org/configuration/), plus the options below.

## Example: an AmneziaWG endpoint

```json
{
  "type": "wireguard",
  "tag": "awg",
  "address": ["10.0.0.2/32"],
  "private_key": "<base64 private key>",
  "peers": [
    {
      "address": "198.51.100.1",
      "port": 51820,
      "public_key": "<base64 public key>",
      "allowed_ips": ["0.0.0.0/0"]
    }
  ],
  "jc": 4, "jmin": 40, "jmax": 70,
  "s1": 20, "s2": 30, "s3": 20, "s4": 30,
  "h1": 12345678, "h2": 23456789,
  "h3": 34567890, "h4": 45678901
}
```

It goes into the `endpoints` list. Without the AmneziaWG options the endpoint is plain WireGuard.

## Example: an XHTTP outbound

```json
{
  "type": "vless",
  "tag": "xhttp-out",
  "server": "example.com",
  "server_port": 443,
  "uuid": "00000000-0000-0000-0000-000000000000",
  "tls": { "enabled": true, "server_name": "example.com" },
  "transport": {
    "type": "xhttp",
    "path": "/xhttp",
    "mode": "auto"
  }
}
```

## Limits

- XHTTP is a client only and speaks HTTP/2 only: h2 with TLS, h2c without. No HTTP/1.1, no HTTP/3, no separate download settings.
- AmneziaWG is tested between two instances of this engine: handshake and data, every extension alone and all together, on Linux, macOS and Windows. It is not tested here against the official AmneziaWG implementation or on an Android device.
- A read deadline that expires on an XHTTP connection ends that connection; it cannot be extended afterwards.

## How it tracks upstream

The fork changes only its own layer. Every file outside the list in `release/FORK_OWNED` must stay byte-identical to the upstream commit in `release/FORK_UPSTREAM_BASE`, and CI fails otherwise. A core update is a merge of the next upstream tag.

## Guides

- [AmneziaWG options](docs/vpnctl/awg.md)
- [XHTTP options](docs/vpnctl/xhttp.md)
- [DNS recipes](docs/vpnctl/dns.md)
- [Builds, releases and verifying a download](docs/vpnctl/build.md)
- [Architecture](ARCHITECTURE.md)

## Licence

GPL-3.0-or-later, the same as upstream. See [LICENSE](LICENSE).
