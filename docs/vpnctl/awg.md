# AmneziaWG options

These options sit directly in a `wireguard` endpoint, next to `private_key` and `peers`. With none of them set the endpoint is plain WireGuard. They need a build with the `with_awg` tag (all release builds have it); without the tag a configuration that sets any of them is rejected instead of silently falling back to plain WireGuard.

Both sides of a tunnel must use the same values for the prefixes, the magic headers and the AmneziaWG 3.1 extensions.

## Junk packets

- `jc` — how many junk packets are sent before a handshake. At most 128.
- `jmin`, `jmax` — the smallest and the largest size of a junk packet in bytes. `jmin` may not exceed `jmax`; `jmax` is at most 65507.

## Prefixes

Random bytes placed in front of a packet.

- `s1` — before a handshake initiation.
- `s2` — before a handshake response.
- `s3` — before a cookie reply.
- `s4` — before every data packet.

## Magic headers

- `h1`, `h2`, `h3`, `h4` — values that replace the four WireGuard message types (initiation, response, cookie reply, data). Each is one number or an inclusive range written as `"min-max"`; with a range the sender picks a value inside it for every packet. The four must not overlap.

## Signature packets and masquerade

- `i1` … `i5` — packets sent before the handshake, written in the AmneziaWG "controlled packet sequence" form (for example `<b 0x…>`, `<r 16>`, `<t>`). The text is case-sensitive and the order matters.
- `ip` — build the first packet for you so it looks like another protocol: `quic`, `dns`, `stun` or `sip`.
- `id` — the domain used in that packet. Required for `quic`, `dns` and `sip`, optional for `stun`.
- `ib` — a browser name for the `quic` form: `chrome`, `firefox` or `curl`. Its effect is limited.

`ip` and `id` cannot be combined with an explicit `i1`. With `ip` set to `sip` the generated packets fill `i1` and `i2`, so an explicit `i2` is rejected too.

## AmneziaWG 3.1 extensions

- `random_trailers` — `true` adds a random number of bytes after handshake packets and random-length padding inside data packets.
- `header_protection_key` — 64 hexadecimal characters (32 bytes). Masks handshake packets and the header of data packets with a stream keyed by this value. Requires every one of `s1` … `s4` to be at least 12.
- `content_padding_addition` — a number or a `"min-max"` range of bytes added to the content of data packets.
- `rekey_after_time` — seconds, or a `"min-max"` range sampled on every check, after which the initiator starts a new handshake. Unset or `"0"` keeps the WireGuard default of 120 seconds. The range `0-4294967295` is rejected.
- `disable_cookies` — `true` turns off the cookie replies that protect against handshake floods. Leave it unset unless the other side requires it.

## Example

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
  "h3": 34567890, "h4": 45678901,
  "random_trailers": true,
  "header_protection_key": "<64 hex characters>",
  "content_padding_addition": "10-50",
  "rekey_after_time": "60-120"
}
```

## What is tested

Handshake and data transfer between two instances of this engine, for plain AmneziaWG 2.0 and for every 3.1 extension alone and all together, on Linux, macOS and Windows. Not tested here: the official AmneziaWG implementation as the other side, and an Android device.
