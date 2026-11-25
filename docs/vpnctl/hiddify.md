# Hiddify ecosystem and censorship transports

A comparative note for sing-box-vpnctl. It covers the Hiddify organisation and the five
components named in the brief, plus twelve protocol or transport families that Hiddify ships,
against upstream sing-box and this fork. Public sources only: source, manifests, first-party
documentation and issue trackers at pinned revisions. No third-party code or service was run.

**Pinned for this note**

- Local baseline: `fe24739a8b8c1ebd573269e50b124366310e4d3e` (2026-11-20), which is
  `v1.14.4-vpnctl.1` plus native DNSTT and per-user bandwidth work; upstream base
  `c005f35300ce76427dc285481ec5695f5b179cd2` (`v1.14.4`, 2026-10-24).
- Hiddify engine: `hiddify/hiddify-sing-box` branch `v5`,
  `4891a0c68ef27353dfaab3a35832239e60176e7f` (2026-11-25).
- Hiddify core and app: `4e5a9a357003d0a995360586702fd24c53042e3f` (2026-11-19) and
  `66c17f83a55233ef4a1c80a4606df811227cc210` (2026-11-22).
- All other revisions are in [§3](#3-pinned-revisions).

## 1. Bottom line

- Hiddify is an ecosystem, not one protocol: Flutter app, Go core, a sing-box fork, a
  subscription converter, a panel, and forks of the libraries the engine depends on.
- Of the twelve protocol/transport items, five are client-only glue around an external project
  (DNSTT, MasterDNS, Soar, GooseRelay, Psiphon). Call, Bond, XDRIVE, Sudoku, TrustTunnel, Mieru
  and AmneziaWG have an in-tree server side, though some still depend on an external service.
- The local fork already has AmneziaWG, client XHTTP, native DNSTT and per-user bandwidth. The
  concrete gaps that matter most are DNSTT resolver pooling/DoH/DoT, TrustTunnel, Mieru,
  Psiphon, the sing-tun reliability fixes and the ray2sing parsers.
- Licence is the main gate. The engine is GPL-3.0-or-later with a naming clause; the core and
  app add a **non-commercial** condition. Do not copy core or app code into a commercial
  product without written permission.
- Best fit for a router/CPE product: TrustTunnel, Mieru, the DNSTT upgrade, Psiphon as a
  last-resort outbound, the sing-tun fixes, and AmneziaWG parity.
- Worst fit: Call Tunnel (untested end to end, account and service-term risk), XDRIVE (very slow,
  storage quota, open liveness issue), GooseRelay (Apps Script quota), and the Hiddify app
  itself (non-commercial licence, large permission surface).
- DNS-tunnel performance claims from MasterDNS and Soar are vendor-reported. The engine CPU
  numbers are Hiddify's own. No independent measurements were found for any item.
- Hiddify tracks the sing-box 1.15 pre-release line while this fork is on stable 1.14.4. The
  Hiddify branch does not declare an exact upstream base commit, so "1.15" is the only safe
  statement.

## 2. Method

### What was read

For every item: the registry or parser entry, the option struct, the `Start`/`Close` path where
present, build tags and stubs, `go.mod`/`pubspec.yaml`, the first-party feature page, the
licence file, and the relevant upstream project at a pinned commit. For Hiddify core, also the
Router Mode implementation and the build Makefile. For the app, the Android manifest, the iOS
packet-tunnel files and the licence. For sing-tun, a file-by-file diff against SagerNet's
`dev` branch.

### Evidence grades

| Grade | Meaning |
|---|---|
| **A** | Read in the pinned source, test or manifest. |
| **B** | First-party documentation, not independently executed. |
| **C** | Vendor benchmark or claim; no independent reproduction. |
| **U** | Explicitly unknown or not stated. |

Ratings in [§7](#7-comparative-matrix) use these grades. A code read is not a security audit;
"security" below means protocol design and threat model, not a review of every line.

### Rating scales

- **Complexity**: S = thin wrapper under ~500 lines of glue; M = one subsystem, 0.5–2k lines;
  L = protocol plus crypto plus lifecycle, or a large external library; XL = multi-platform
  product surface.
- **Performance**: first-party measurement where one exists, otherwise vendor claim or
  "unmeasured".
- **Priority**: P1 = adopt or port in the next cycle; P2 = prototype after P1; P3 = defer,
  niche or risky; P4 = reference only.
- **Licence**: SPDX label from the pinned licence file, not legal advice.

### What this note does not do

It does not run any of these protocols, does not test interoperability with a third-party
server, does not measure throughput, and does not deploy anything. It also does not assess
Hiddify Manager beyond its role in the ecosystem.

## 3. Pinned revisions

The commit hash is the pin. Every source link in §5–§8 resolves at one of these revisions or at
the corresponding upstream revision in this table.

| Repository | Revision | Date | Used for |
|---|---|---|---|
| `SagerNet/sing-box` | `c005f35300ce76427dc285481ec5695f5b179cd2` (v1.14.4) | 2026-10-24 | Local upstream base |
| `PavelLizunov/sing-box-vpnctl` | `fe24739a8b8c1ebd573269e50b124366310e4d3e` | 2026-11-20 | Local baseline |
| `hiddify/hiddify-sing-box` | `4891a0c68ef27353dfaab3a35832239e60176e7f` (v5) | 2026-11-25 | Engine and protocol code |
| `hiddify/hiddify-core` | `4e5a9a357003d0a995360586702fd24c53042e3f` | 2026-11-19 | Router Mode, CLI, library |
| `hiddify/hiddify-app` | `66c17f83a55233ef4a1c80a4606df811227cc210` | 2026-11-22 | UI, manifests, licence |
| `hiddify/ray2sing` | `a722bf1b184d7fcf10248dee45fc099d62646ca8` | 2026-11-22 | Converter |
| `hiddify/sing-tun` | `fe80f0918269863a13628e53e4557b038882504f` (dev) | 2026-11-17 | TUN fork |
| `SagerNet/sing-tun` | `2e93ae4ab5ab5f838a7a6014d1082edebda4f5cf` (dev) | 2026-11-25 | Diff base |
| `SUDOKU-ASCII/sudoku` | `b1d1f0e767653d2c70e2ff814a01264c7dedf1f4` | 2026-10-31 | Sudoku reference |
| `enfein/mieru` | `9812207bb8aefc3dd4dcc6a5ce268bf5e28b6c54` | 2026-11-23 | Mieru library |
| `TrustTunnel/TrustTunnel` | `113cdc89559b9d2698e1477b3d0af89e95c0b7cf` | 2026-11-19 | TrustTunnel protocol |
| `Psiphon-Labs/psiphon-tunnel-core` | `5dccaa36ae176ba974bcebc2124f172ecec51b08` | 2026-11-24 | Psiphon core |
| `CluvexStudio/psiphon-tunnel-core` | `2956a96b56fe09b99e72a8eb4bed3e79d29e3543` (`shirokhorshid`) | 2026-09-29 | Hiddify's Psiphon fork |
| `amnezia-vpn/amneziawg-go` | `08fe3d33bbb116be09a6183d02609311b8b0c9e8` | 2026-11-16 | AmneziaWG upstream |
| `hiddify/amneziawg-go` | `a2ae165a1b3f062ce9dd3dc3353ebd1c591847a5` (v5) | 2026-11-05 | Hiddify's AWG fork |
| `masterking32/MasterDnsVPN` | `7198b45b86c7def2e226e41bcca3eca8ca2ea502` | 2026-11-18 | MasterDNS upstream |
| `hiddify/MasterDnsVPN` | `a6b693698ee8246ca415a994bb6da40d66dddba7` (`hiddify`) | 2026-11-16 | Hiddify's MasterDNS fork |
| `getlantern/soar` | `007179d4b40bd589d2b0240abffae33c43899839` | 2026-11-11 | Soar upstream |
| `hiddify/soar` | `e18587bc9fb62f326124d1f3eb0ceb31708bd077` (`hiddify`) | 2026-11-16 | Hiddify's Soar fork |
| `Kianmhz/GooseRelayVPN` | `0ed803f635890cbc021f71d7c8746ca1d2f262b1` | 2026-05-29 | GooseRelay upstream |
| `hiddify/GooseRelayVPN` | `0e68c2a3ae4c13088d8d8aba6ba279910924e96f` | 2026-04-29 | Hiddify's GooseRelay fork |
| `net2share/vaydns` | `a0ff70110d5e96686ab8f4c8e1c44cd09be07a75` | 2026-04-10 | DNSTT/VayDNS client |
| `hiddify/vaydns` | `75d556afd23a8f4cf93392f4a8e218aac1fa3ff0` | 2026-11-03 | Hiddify's VayDNS fork |
| `xchacha20-poly1305/sing-trusttunnel` | `27f689c5105a7f20bc64d5865c49f481cb8ca68c` | 2026-11-21 | TrustTunnel Go library |
| `XTLS/Xray-core` | `b56ffef338c8096c7dbe78eaa76f1cb05a328d6f` | 2026-11-25 | XDRIVE origin, licence |
| `kulikov0/headless-client` | `a63a0e177d6bf9edbf9b51502a55860c6d3a2832` | 2026-10-29 | Call Tunnel WebRTC |
| `shtorm-7/sing-box-extended` | `d9442d14b19ddd02605bf3ee893a171947038e19` | 2026-10-19 | Source of Bond, Sudoku, Call ports |
| `hiddify/Hiddify-Manager` | `455b60f7798df00b8322ffaadddd06d9bef7c568` | 2026-11-17 | Panel role only |

The comparison uses the pre-integration baseline. The [TURN decision and test results](transports.md) cover the subsequent experiment.

## 4. Baseline

| | Upstream sing-box v1.14.4 | Local `fe24739a` | Hiddify `4891a0c` |
|---|---|---|---|
| Base | Stable v1.14.4 | Stable v1.14.4 + fork layer | sing-box 1.15 pre-release line; exact base not declared |
| AmneziaWG | No | Inside the `wireguard` endpoint, flat options, 2.0 and 3.1 subset | Separate `awg` endpoint, nested options, 3.1 plus rekey/reject/keepalive knobs, MTU discovery, fallback address, peer names |
| XHTTP | No | Client only, HTTP/2 or h2c | XHTTP with modes, padding, XMUX, split download |
| DNSTT | No | Native outbound, original-compatible, one resolver | Native outbound, ~58k resolver list, DoH/DoT/uTLS, smart pool, reconnection controls |
| VLESS post-quantum encryption | No | No | Yes (`mlkem768x25519plus`) |
| Other protocols | Upstream set | None | Call, Bond, Sudoku, Mieru, TrustTunnel, Psiphon, MasterDNS, Soar, GooseRelay, XDRIVE |
| Router Mode | TUN and `auto_redirect` primitives | Not a product feature | Managed router/gateway mode in core |
| Monitoring | URL test | URL test | Background delay and exit-IP monitoring, groups, DNS-tunnel timeouts |
| TLS / REALITY | `tls.fragment`, `tls.record_fragment`, REALITY | Same as upstream | `tls_fragment` with sizes/pauses, TLS tricks, hybrid PQ key share in REALITY |
| Licence | GPL-3.0-or-later plus naming clause | Same text as upstream | Same engine licence text |

Local evidence: [AWG options](awg.md), [XHTTP options](xhttp.md), [DNSTT](dnstt.md),
[build and release](build.md), [`FORK_OWNED`](../../release/FORK_OWNED), and
[`protocol/dnstt/outbound.go`](../../protocol/dnstt/outbound.go). Hiddify evidence is linked in
the item notes.

The local fork is deliberately narrow: AWG, XHTTP, DNSTT, per-user attribution and bandwidth,
V2Ray stats. Hiddify is a broad superset. That makes Hiddify useful as a source of individual
features, not as a merge target.

Local has no equivalent for Call, MasterDNS, Soar, GooseRelay, XDRIVE, Bond, Psiphon,
TrustTunnel, Sudoku or Mieru; each would be new surface. DNSTT and AmneziaWG are the two items
where the local fork has a comparable implementation, so their notes include a direct
comparison.

## 5. Hiddify organisation and stack

### 5.1 Organisation

| Repository | Role | Language | Stars | Licence |
|---|---|---|---|---|
| `hiddify-app` | Flutter client for Android/TV, iOS, Windows, macOS, Linux | Dart | 34,469 | GPL-3.0 plus non-commercial terms |
| `hiddify-core` | Go engine wrapper, CLI, gomobile/desktop library, Router Mode, extensions | Go | 238 | GPL-3.0 plus non-commercial terms |
| `hiddify-sing-box` | The engine: sing-box plus protocols, transports and fixes | Go | 72 | GPL-3.0-or-later plus naming clause |
| `Hiddify-Manager` | Multi-user server panel, 20+ protocols, Telegram proxy | Python | 9,677 | GPL-3.0 |
| `ray2sing` | Share-link and subscription converter | Go | 35 | CC0-1.0 |
| `hiddify.com` | Documentation site | Python | 124 | Not assessed |

Hiddify also maintains forks of `sing-tun`, `amneziawg-go`, `psiphon-tunnel-core`,
`MasterDnsVPN`, `soar`, `vaydns`, `GooseRelayVPN`, `warp-plus`, `xray-core`, `sing`,
`sing-mux`, `sing-vmess`, `gvisor`, `tailscale` and `sing-openvpn`. The stack is therefore not
just an app on upstream sing-box; it is a set of coordinated forks.

The app advertises VPN and proxy modes, per-app routing on Android, connection sharing, free
WARP and Psiphon profiles, and 30 languages ([app README](https://github.com/hiddify/hiddify-app/blob/66c17f83a55233ef4a1c80a4606df811227cc210/README.md)).
The core is the same engine for the app, the CLI, OpenWrt, Docker and desktop libraries
([core README](https://github.com/hiddify/hiddify-core/blob/4e5a9a357003d0a995360586702fd24c53042e3f/README.md)).

### 5.2 Router Mode

Router Mode is the strongest non-protocol product feature in the set. It is implemented in
`hiddify-core`, not in the engine: [`v2/config/router_mode.go`](https://github.com/hiddify/hiddify-core/blob/4e5a9a357003d0a995360586702fd24c53042e3f/v2/config/router_mode.go)
and [`docs/router-mode.md`](https://github.com/hiddify/hiddify-core/blob/4e5a9a357003d0a995360586702fd24c53042e3f/docs/router-mode.md).

What it does:

- Forces TUN on and `auto_redirect` on. `auto_redirect` is upstream sing-box's nftables
  redirect; Hiddify refuses to fall back to a plain TUN because a plain TUN is not a router.
- TCP is redirected by the `prerouting` and `output` chains; UDP and ICMP are marked and routed
  into the TUN by an `ip rule`; DNS on port 53 is answered by the proxy's DNS.
- Connections from outside are skipped by `ct direction reply`; reply traffic to the router's
  SSH or a port forward keeps the main table.
- The router's own addresses, interface subnets and private destinations stay direct.
- On OpenWrt, sing-tun adds the TUN to firewall4 through
  `/etc/nftables.d/0-sing-box-auto-redirect.nft`.
- At start it enables IPv4/IPv6 forwarding and loads the nfqueue modules; at stop it restores
  the saved sysctl values ([`router_host_linux.go`](https://github.com/hiddify/hiddify-core/blob/4e5a9a357003d0a995360586702fd24c53042e3f/v2/hcore/router_host_linux.go)).

Platforms: OpenWrt (`option mode 'router'`), any Linux gateway with `--router`, Docker with
`ROUTER_MODE=1`, MikroTik RouterOS 7 via a side box or container, and the Linux app under
Settings → Service mode → Router. Requirements: Linux, root, nftables, nfqueue, `/dev/net/tun`;
a container cannot load the kernel modules.

Assessment: high product value for a router, but it is a privileged, host-wide change. The
implementation is careful about restoring forwarding and about not capturing forwarded
connections, but this note did not run it. Upstream sing-box already exposes `auto_redirect`;
the Hiddify value is the managed mode, the OpenWrt packaging and the failure handling, not the
nftables primitive itself.

## 6. Non-protocol improvements

These are the engine changes that are not a new protocol. They matter more for a product than
another niche transport.

| Change | What it does | Evidence | Measured effect | Priority |
|---|---|---|---|---|
| Adaptive processors | `GOMAXPROCS` follows load, one processor at idle | [doc](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/docs/hiddify/features/adaptive-processors.en.md), [`hiddify/procs`](https://github.com/hiddify/hiddify-sing-box/tree/4891a0c68ef27353dfaab3a35832239e60176e7f/hiddify/procs) | Hysteria2 13.3 → 5.3 CPU s/GB; TUIC 8.7 → 3.6; WireGuard 13.3 → 9.2 on macOS (vendor) | P1 for CPE |
| Paced reads | Waits briefly on busy sockets so reads batch | [doc](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/docs/hiddify/features/paced-reads.en.md) | WireGuard 17–26 → 9 CPU s/GB; Linux runs 26/61 → 19/34 (vendor) | P1/P2 |
| Read ahead | 64 KiB socket reads for VMess/Shadowsocks | [doc](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/docs/hiddify/features/read-ahead.en.md) | VMess 11.2 → 14.2 Gbit/s; SS AES 11.9 → 19.5 (vendor, loopback) | P2 |
| TLS fragment | Split ClientHello with size and pause, works on REALITY | [doc](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/docs/hiddify/features/tls-fragment.en.md) | Handshake slower; bypass depends on DPI | P2 |
| TLS tricks | Mixed-case SNI and padding extension | [doc](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/docs/hiddify/features/tls-tricks.en.md) | No throughput effect; bypass depends on DPI | P2 |
| REALITY hybrid key share | Sends X25519MLKEM768; optional classical fallback | [doc](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/docs/hiddify/features/reality-hiddify.en.md) | Compatibility with Xray servers from v26.9.8 | P1 |
| PROXY protocol | Inbound header and direct-outbound send | [doc](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/docs/hiddify/features/proxy-protocol.en.md) | Deployment compatibility | P2 |
| Proxy security marks | Marks each outbound secure/not secure with reason | [doc](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/docs/hiddify/features/proxy-security.en.md) | UI/UX; flags XOR, no-cipher, insecure TLS | P2 |
| Monitoring | Delay and exit-IP tests, groups, DNS-tunnel timeouts | [doc](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/docs/hiddify/features/groups/monitoring.en.md), [`common/monitoring`](https://github.com/hiddify/hiddify-sing-box/tree/4891a0c68ef27353dfaab3a35832239e60176e7f/common/monitoring) | Better group selection; not a throughput change | P1 |
| WARP/Psiphon edge scanner | Scores endpoints and CDN edges per network, then fails over | [Psiphon doc](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/docs/hiddify/features/protocols/psiphon.en.md), [WARP scanner](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/docs/hiddify/features/protocols/warp-scanner.en.md) | Reachability gain; network-dependent, not a throughput number | P2 |
| Fallback group | First member that connects wins; failed member parked | [doc](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/docs/hiddify/features/groups/fallback.en.md) | Faster failover than URL-test interval | P1/P2 |
| Hot reload and lazy endpoints | Replace changed outbounds without restart; start endpoints on demand | `h_box_hotreload.go`, `h_box_lazy.go` in the engine | Less downtime on subscription change | P2 |

The measured CPU numbers are Hiddify's own, on macOS and Linux; they were not reproduced here.
The mechanism is plausible and independent of the protocols, so it is the cheapest large win
for a CPE if the local engine's data path is the bottleneck.

## 7. Comparative matrix

### 7.1 Role and integration

| Item | Kind | Registration / type | Build tag | Client/server | Main dependencies | Platforms |
|---|---|---|---|---|---|---|
| hiddify-sing-box | Engine fork | n/a | Many; release list includes `with_awg`, `with_sudoku`, `with_call`, `with_bond`, `with_xdrive`, `with_kcp` | Both | sing-box tree, Xray-core, Psiphon, Mieru, Soar, MasterDNS, TrustTunnel, AWG, VayDNS | Linux, Windows, macOS, Android, iOS |
| hiddify-core | Core/CLI/library | n/a; uses the engine registry | Core/protocol tags in Makefile | Both (app client; CLI can run any inbound) | Engine, Xray-core, ray2sing, WARP+, libxray, gRPC | Linux/OpenWrt/Docker, Android, iOS, desktop |
| hiddify-app | Flutter UI | n/a | n/a | Client | Core via FFI/gRPC; Flutter, drift, Sentry | Android/TV, iOS, Windows, macOS, Linux |
| ray2sing | Converter | `configTypes`, `endpointParsers` maps | n/a | n/a | `sing-box/include`, wireguard-go | CLI, WASM |
| sing-tun | TUN library | n/a | n/a | n/a | gVisor, netlink, nftables | Linux, Windows, macOS, iOS; Android via Hiddify |
| Call Tunnel | WebRTC call tunnel | Inbound/outbound `call` | `with_call` | Both | `headless-client`, Pion RTP/datachannel, service APIs | All Go targets |
| DNSTT | DNS tunnel | Outbound `dnstt` | None | Client only | VayDNS fork, `common/dnspool`, KCP, smux | All Go targets |
| MasterDNS | DNS tunnel | Outbound `masterdns` | None | Client only | `masterdnsvpn-go` fork, `common/dnspool` | All Go targets |
| Soar | DNS tunnel | Outbound `soar` | None | Client only | `getlantern/soar`, `common/dnspool` | All Go targets |
| GooseRelay | Apps Script relay | Outbound `gooserelay` | None | Client only | `GooseRelayVPN`, UDP-over-TCP | All Go targets |
| XDRIVE | Storage transport | V2Ray transport `xdrive` in `transport` | `with_xdrive` | Both (VLESS) | Xray-core transport, VLESS encryption | All Go targets |
| Bond | Link aggregation | Inbound/outbound `bond` | `with_bond` | Both | Own code | All Go targets |
| Psiphon | Network client | Outbound `psiphon` | None | Client only | Psiphon core fork, edge scanner | All supported app targets |
| AmneziaWG | WireGuard obfuscation | Endpoint `awg` | `with_awg` | Both | `amneziawg-go` fork | All Go targets |
| TrustTunnel | HTTP CONNECT VPN | Inbound/outbound `trusttunnel` | None; `with_quic` for HTTP/3 | Both | `sing-trusttunnel` | All Go targets |
| Sudoku | Obfuscated proxy | Inbound/outbound `sudoku` | `with_sudoku` | Both | Own code | All Go targets |
| Mieru | Encrypted proxy | Inbound/outbound `mieru` | None | Both | `enfein/mieru/v3` | All Go targets |

The engine registry is [`include/registry.go`](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/include/registry.go).
Build-tag stubs reject a configuration with a clear error when the tag is absent, for example
[`include/h_call_stub.go`](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/include/h_call_stub.go).
The core Makefile puts the protocol tags into every release build, including Android and iOS
([`Makefile`](https://github.com/hiddify/hiddify-core/blob/4e5a9a357003d0a995360586702fd24c53042e3f/Makefile)).

### 7.2 Assessment

| Item | Complexity | Performance | Security | Licence | Priority | Evidence / unknowns |
|---|---|---|---|---|---|---|
| hiddify-sing-box | XL | Upstream plus measured CPU wins | Large surface; 1.15 pre-release line | GPL-3.0-or-later + naming | P1 (source) | A/B; exact upstream base U |
| hiddify-core | XL | Router Mode and adaptive procs | Third-party extensions; non-commercial | GPL-3.0 + non-commercial | P3 | A; sandbox and build gaps U |
| hiddify-app | XL | UI | Broad permissions; non-commercial | GPL-3.0 + non-commercial | P4 | A manifests; store builds U |
| ray2sing | L | n/a | Parses untrusted links; links the full engine | CC0 (binary GPL) | P1 | A; fuzz coverage U |
| sing-tun | L | Reliability fixes, no benchmark | Privileged; changes host firewall | GPL-3.0-or-later | P1 | A diff; OS coverage U |
| Call Tunnel | XL | Low, unmeasured | Link is the key; account/service risk | GPL-3.0-or-later | P3 | A code, B docs; E2E U |
| DNSTT | M | Low; pool improves availability | Noise; resolver metadata visible | GPL-3.0 (lib CC0) | P1 | A code/B docs; interop U |
| MasterDNS | M | Vendor: up to ~9× DNSTT | XOR default is weak; homebrew protocol | MIT (integration GPL) | P2 | A glue, C vendor; independent bench U |
| Soar | M | Vendor: fastest DNS tunnel | Ed25519 + X25519 + ChaCha20 | Apache-2.0 | P2 | A glue, C vendor; server/bench U |
| GooseRelay | S | Apps Script quota; low | AES-256-GCM; domain-fronting terms | MIT | P3 | A/B; quota and policy U |
| XDRIVE | M | Very low; hundreds of ms RTT | Cloud storage metadata; secrets in config | MPL-2.0/Xray | P3 | A/B; liveness and quota U |
| Bond | M | Aggregate of legs; unmeasured | No added crypto | GPL-3.0-or-later | P2 | A/B; benchmarks U |
| Psiphon | L | Shared, variable; TCP only | Mature core; shared exit | GPL-3.0 | P2 | A/B; scanner efficacy U |
| AmneziaWG | L | WireGuard speed; official 3.1 interop not stated | WireGuard crypto; obfuscation only | MIT | P1 | A code/B docs; 3.1 interop U |
| TrustTunnel | M | HTTP/2 or HTTP/3; no benchmark | TLS + password; new protocol | Apache-2.0 protocol; GPL library | P1 | A/B; official interop U |
| Sudoku | M | ≥4× upload overhead | Custom crypto; unaudited | GPL-3.0 | P2 | A/B; only self-interop tested |
| Mieru | M | Encrypted/padded; no benchmark | Custom crypto; unaudited | GPL-3.0 | P2 | A/B; official interop U |

## 8. Item notes

### 8.1 hiddify-sing-box

The engine is the sing-box tree with Hiddify's protocols, transports and fixes. The README says
"sing-box (v1.15)" ([README](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/README.md)).
The branch has no `release/FORK_UPSTREAM_BASE` file, unlike this fork, so the exact base commit
is unknown. The only 1.15 tags at the pin are alpha tags (`v1.15.0-alpha.13`); upstream's
latest stable release is v1.14.4, and GitHub's latest published release for this fork is the
older `v1.13.0.h5` (2026-02-09). The branch, not the release page, is the current code.

- **Registration**: one central registry; build tags and stubs per optional protocol.
- **Client/server**: both, except the client-only outbounds listed above.
- **Dependencies**: `go.mod` requires Go 1.27 and pulls Xray-core, Psiphon, Mieru, Soar,
  MasterDNS, TrustTunnel, AWG, VayDNS and WebRTC. The local release toolchain is Go 1.26.x, so a
  port must either raise the toolchain or backport the code. This is a large dependency graph
  even before the app.
- **Performance**: upstream data path plus the CPU work in §6; no end-to-end benchmark.
- **Security**: inherits sing-box's design, but adds a large amount of new code and several
  custom crypto protocols. Some of it is ported from sing-box-extended and is not independently
  audited.
- **Licence**: GPL-3.0-or-later plus "no derivative work may use the name or imply association"
  ([LICENSE](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/LICENSE)).
- **Priority**: P1 as a source of individual ports. Do not merge the tree wholesale.

### 8.2 hiddify-core

The core is a Go wrapper around the engine, with a CLI, a gomobile library for Android/iOS, a
desktop shared library, an extension system, a gRPC service and Router Mode. The build targets
are visible in the [Makefile](https://github.com/hiddify/hiddify-core/blob/4e5a9a357003d0a995360586702fd24c53042e3f/Makefile):
Android AAR, iOS/macOS XCFramework, Windows DLL, Linux `.so` and CLI, OpenWrt package and
Docker.

- **Registration**: no protocol registry of its own; it builds a config and calls the engine.
- **Client/server**: app-facing client, but the CLI can run any engine inbound, so it is a
  general core.
- **Lifecycle**: `Start` queues and returns `STARTING`; the outcome arrives through a
  `CoreInfoListener`; states include `STOPPED`, `STARTING`, `STARTED`, `STOPPING`,
  `HOT_RELOADING`, `WAITING` and `NO_AVAILABLE_PROXIES`
  ([`start.go`](https://github.com/hiddify/hiddify-core/blob/4e5a9a357003d0a995360586702fd24c53042e3f/v2/hcore/start.go),
  [`hcore.proto`](https://github.com/hiddify/hiddify-core/blob/4e5a9a357003d0a995360586702fd24c53042e3f/v2/hcore/hcore.proto)).
- **Router Mode**: see §5.2.
- **Security**: the extension system accepts third-party extensions that can modify configs, run
  actions and show UI inside the app. The repository does not document a sandbox. Treat
  extensions as trusted code.
- **Licence**: GPL-3.0 plus Section 7 terms, including **NonCommercial** and a ban on ads
  ([LICENSE.md](https://github.com/hiddify/hiddify-core/blob/4e5a9a357003d0a995360586702fd24c53042e3f/LICENSE.md)).
- **Priority**: P3. Borrow the Router Mode design, not the code, unless the licence is resolved.

### 8.3 hiddify-app

Flutter client for Android/TV, iOS, Windows, macOS and Linux. Version `5.9.15+50915`
([`pubspec.yaml`](https://github.com/hiddify/hiddify-app/blob/66c17f83a55233ef4a1c80a4606df811227cc210/pubspec.yaml)).

- **Manifests**: Android declares a `VpnService` with `BIND_VPN_SERVICE`, always-on support, a
  separate `ProxyService`, boot and notification permissions, `QUERY_ALL_PACKAGES` and
  `REQUEST_IGNORE_BATTERY_OPTIMIZATIONS`
  ([`AndroidManifest.xml`](https://github.com/hiddify/hiddify-app/blob/66c17f83a55233ef4a1c80a4606df811227cc210/android/app/src/main/AndroidManifest.xml)).
  iOS has a `PacketTunnelProvider` network extension; desktop uses the core shared library.
- **Dependencies**: Flutter, gRPC, drift, Sentry, `installed_apps` (for per-app routing).
- **Security**: broad package visibility is required for per-app routing, but it is also a
  privacy surface. Sentry is a crash-reporting dependency; its configuration was not assessed.
- **Licence**: "Hiddify Extended GNU General Public License v3" with non-commercial, naming,
  source-availability and share-alike conditions
  ([LICENSE.md](https://github.com/hiddify/hiddify-app/blob/66c17f83a55233ef4a1c80a4606df811227cc210/LICENSE.md)).
- **Priority**: P4. It is a UI/product, not a protocol source, and the licence blocks
  commercial reuse.

### 8.4 ray2sing

A converter from share links and subscriptions to sing-box JSON. The parser maps are in
[`convert.go`](https://github.com/hiddify/ray2sing/blob/a722bf1b184d7fcf10248dee45fc099d62646ca8/ray2sing/convert.go):
VMess, VLESS, Trojan, Shadowsocks, TUIC, Hysteria/Hysteria2, SSH, Naive, AnyTLS, Snell,
SOCKS/HTTP, Mieru, Psiphon, DNSTT, MasterDNS, Soar, SlipNet, MASQUE, Tor, WireGuard/AWG,
WARP, OpenVPN and AmneziaVPN.

- **Registration**: two prefix maps plus an endpoint map; unknown schemes are rejected.
- **Dependencies**: imports `sing-box/include`, so the converter links the whole engine and
  wireguard-go. The repo itself is 8.1k lines of Go across 65 files.
- **Security**: parses untrusted subscription content. The code has tests but no visible fuzz
  target for the link parsers.
- **Licence**: CC0-1.0. A binary that links sing-box is GPL-3.0 in practice.
- **Priority**: P1 for parser coverage and link formats; reuse the parsers, not the whole
  engine, if binary size matters.

### 8.5 sing-tun

SagerNet's transparent-proxy library, forked by Hiddify at `fe80f091`. A diff against SagerNet
`dev` at `2e93ae4a` shows 78 paths that differ or are added. The notable changes:

- `cleanup_linux.go`: remove leftover policy rules, redirect rules and nftables tables after a
  killed process, so the host does not lose networking
  ([file](https://github.com/hiddify/sing-tun/blob/fe80f0918269863a13628e53e4557b038882504f/cleanup_linux.go)).
- `h_address_conflict.go`: distinguish stale and live holders of a tunnel address, mainly for
  Windows ([file](https://github.com/hiddify/sing-tun/blob/fe80f0918269863a13628e53e4557b038882504f/h_address_conflict.go)).
- `h_udp_egress.go`: use ephemeral source ports for client UDP members so Windows Firewall does
  not prompt ([file](https://github.com/hiddify/sing-tun/blob/fe80f0918269863a13628e53e4557b038882504f/h_udp_egress.go)).
- `h_stack_system_accept.go`: recover the system-stack accept loop after EMFILE or a dead
  listener ([file](https://github.com/hiddify/sing-tun/blob/fe80f0918269863a13628e53e4557b038882504f/h_stack_system_accept.go)).
- nfqueue and monitor changes for Router Mode and SYN-only handling.

- **Registration**: none; it is a library.
- **Client/server**: n/a.
- **Platforms**: Linux, Windows, macOS, iOS; Android through Hiddify's builds.
- **Licence**: GPL-3.0-or-later.
- **Priority**: P1. These are targeted, GPL-compatible reliability fixes that fit the local
  fork without adopting Hiddify's whole stack.

### 8.6 Call Tunnel

Inbound and outbound type `call`, build tag `with_call`. The transport is 18.4k lines plus the
MIT-licensed `headless-client` WebRTC library. It supports VK Calls, Yandex Telemost, Dion,
WB Stream and Bitrix24; data travels as VP8 video frames encrypted with XChaCha20-Poly1305, or
over a WebRTC data channel
([docs](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/docs/hiddify/features/protocols/call.en.md),
[`transport/call/config.go`](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/transport/call/config.go)).

- **Registration**: `inbound.Register` and `outbound.Register` under `C.TypeCall`; stubbed
  without the tag.
- **Client/server**: both; the server side logs in with an account and creates or joins a call.
  The inbound listens on no port.
- **Lifecycle**: retries after 5 s, 10 s, 30 s, then every minute; connections fail with
  "call: not connected to the call yet" until the bridge is up; closing leaves the call; VK may
  require a captcha on the client side.
- **Performance**: explicitly "not measured in Hiddify"; the service's video bitrate limits it.
- **Security**: the call link is the key; anyone with the link, including the service, can read
  the tunnel. The service sees a long, non-human call and can ban the account. This is not a
  threat model for a product.
- **Licence**: GPL-3.0-or-later.
- **Priority**: P3. Only relevant where video calls to a specific domestic service are the one
  thing that passes, and even then the account and service-term risk is high. Hiddify itself
  says it is not tested end to end.

### 8.7 DNSTT

Outbound type `dnstt`, no build tag, 784 lines of glue. It uses the VayDNS fork of DNSTT, KCP
for reliability, Noise for encryption and smux for multiplexing
([`outbound.go`](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/protocol/hiddify/dnstt/outbound.go)).
Client only; the DNSTT/VayDNS server is external and forwards each stream to a fixed TCP backend.

- **Resolvers**: `auto` adds a built-in list of about 58,000 public resolvers; DoH and DoT are
  supported with a uTLS fingerprint; a pretest query checks a resolver before a real tunnel
  probe; scores from -10 to 10 are saved; up to 10 working resolvers are kept.
- **Modes**: one tunnel over all resolvers, one tunnel per resolver, or a smart pool with
  load balance, deadline, probe interval and duplicate.
- **Lifecycle**: ready after the first resolver passes; keepalive 10 s; session check 20 s;
  reconnect 1–30 s; idle timeout 60 s (2 min in original-DNSTT compatibility mode). Monitoring
  gives DNS tunnels 3× the normal timeout.
- **Security**: Noise protects the payload; the resolver and the network see the domain and
  timing. TCP only.
- **Licence**: GPL-3.0; the VayDNS library is CC0.
- **Local comparison**: this fork has a 292-line original-compatible outbound using
  `net2share/vaydns v0.2.8`, one resolver and smux, with no pool, DoH/DoT, scores or reconnect
  controls ([`protocol/dnstt/outbound.go`](../../protocol/dnstt/outbound.go),
  [DNSTT note](dnstt.md)).
- **Priority**: P1. Port the resolver tester, smart pool, DoH/DoT and reconnect policy into the
  existing local DNSTT. The wire compatibility with Hiddify's VayDNS fork is unknown; the local
  outbound is deliberately original-compatible.

### 8.8 MasterDNS

Outbound type `masterdns`, no build tag, 570 lines of glue around `masterdnsvpn-go`
([`outbound.go`](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/protocol/hiddify/masterdns/outbound.go)).
Client only; the MasterDnsVPN server connects to the destination itself in `socks5` mode, or
forwards to a fixed target in `tcp` mode.

- **Design**: MTU probes to every resolver, packet duplication over different resolvers, its
  own ARQ, optional zstd/lz4/zlib compression, eight balancing modes, auto-disable of timeout
  resolvers and background recheck.
- **Crypto**: `none`, `xor` (default), `chacha20`, AES-128/192/256-GCM. XOR is explicitly a
  weak cipher in Hiddify's own proxy-security table; use AES or ChaCha20 if this is adopted.
- **Performance**: the upstream README claims a 5–7-byte header versus ~59 for DNSTT and up to
  ~9× the speed; its 10 MB local test is 0.270 s versus 2.492 s for DNSTT
  ([README](https://github.com/masterking32/MasterDnsVPN/blob/7198b45b86c7def2e226e41bcca3eca8ca2ea502/README.MD)).
  These are vendor numbers, not independently reproduced.
- **Lifecycle**: MTU tests, one session, `ready_timeout` 30 s, resolver health checks and
  rechecks; TCP only, with UDP-over-TCP for servers that support it.
- **Licence**: MIT for MasterDnsVPN; the integration is GPL-3.0.
- **Priority**: P2. A credible DNSTT alternative if resolver loss is the dominant problem, but
  the default XOR and the lack of independent benchmarks keep it behind the DNSTT upgrade.

### 8.9 Soar

Outbound type `soar`, no build tag, 691 lines of glue around Lantern's `getlantern/soar`
([`outbound.go`](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/protocol/hiddify/soar/outbound.go)).
Client only; the Soar server is the authority for a delegated zone and connects to destinations
itself.

- **Design**: X25519 session key exchange signed by the server's Ed25519 key; ChaCha20-Poly1305
  data with an 8-byte truncated Poly1305 tag (forgery probability 2^-64 per attempt); a signed
  answer proves the query arrived; fast loss detection at both ends; 101-character names by
  default; AAAA or TCP fallbacks where TXT/UDP is filtered. The protocol is pre-release.
- **Discovery**: verified resolvers from previous runs, the device's resolvers, bundled
  country lists (Iran, China, Russia) and public resolvers; the smart pool is on by default.
- **Performance**: the project claims it is the fastest and most reliable DNS tunnel, with
  benchmarks on `soar.lantern.io`; no independent reproduction here. It is designed for
  memory-constrained mobile clients.
- **Lifecycle**: one session, `session_idle`, max 64 streams, 128 in-flight queries; `detour`
  is not supported.
- **Licence**: Apache-2.0.
- **Priority**: P2. The Apache-2.0 library is attractive, but it is a new project and needs a
  server deployment and an independent benchmark before it displaces DNSTT.

### 8.10 GooseRelay

Outbound type `gooserelay`, no build tag, 294 lines of glue around `kianmhz/GooseRelayVPN`
([`outbound.go`](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/protocol/hiddify/gooserelay/outbound.go)).
Client only. The path is: client → Google front IP with `SNI=www.google.com` → Apps Script
forwarder → the user's VPS exit server.

- **Crypto**: AES-256-GCM with a shared `tunnel_key`; Google does not hold the key.
- **Lifecycle**: at start it checks every deployment ID concurrently, confirms the script is
  public and the key matches, and is ready when one passes; all failing means the outbound
  fails. Sessions use HTTPS batches and long polling.
- **Limits**: TCP only; Apps Script has a daily request quota, so heavy use needs several
  deployments; Google can change routing or block the pattern; domain fronting may violate the
  provider's terms.
- **Licence**: MIT.
- **Priority**: P3. It is a clever fallback for Google-reachable networks, but the quota and
  third-party dependency make it unsuitable as a primary transport.

### 8.11 XDRIVE

V2Ray transport `xdrive`, build tag `with_xdrive`, 297 lines of glue around Xray-core's
transport ([`transport.go`](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/transport/v2rayxdrive/transport.go)).
Client and server never connect: they exchange files through Google Drive, S3, a template of
HTTP requests, or a local folder.

- **Registration**: `v2ray.RegisterXDriveConstructor(NewServer, NewClient)` in the transport
  package, selected by `"type": "xdrive"` inside a VLESS `transport` block.
- **Constraints**: Xray allows XDRIVE only for VLESS with `encryption`; this fork has the
  post-quantum VLESS encryption, the local v1.14.4 baseline does not. The client has no
  Mux.Cool and opens one session per connection; the server accepts Xray's Mux.Cool.
- **Performance**: every write is a file upload and the other side polls; the docs say hundreds
  of milliseconds or more per round trip and API quota limits. It is a last-resort transport,
  not daily traffic.
- **Liveness**: Xray issue [#7208](https://github.com/XTLS/Xray-core/issues/7208) is open: a
  vanished client leaves the server uploading the rest of a response and holding the session
  for minutes. That is a known operational risk.
- **Security**: the storage provider sees access patterns and file metadata; the payload is
  protected by VLESS encryption; the config holds OAuth/S3 secrets. Storage terms and account
  bans are real risks.
- **Licence**: Xray-core is MPL-2.0; the Hiddify port lives in a GPL-3.0 tree.
- **Priority**: P3. Only worth it when every direct route and every CDN is blocked and a
  storage service is reachable.

### 8.12 Bond

Inbound and outbound type `bond`, build tag `with_bond`, 653 lines of own code, ported from
sing-box-extended ([`protocol/bond`](https://github.com/hiddify/hiddify-sing-box/tree/4891a0c68ef27353dfaab3a35832239e60176e7f/protocol/bond)).
It opens one leg per configured outbound for each connection and joins them on the server.

- **Design**: a short header carries connection ID, leg number, leg count, ratios and
  destination; frames are at most 64 KiB and are split by upload/download ratios. Up to 255
  legs; ratios must sum to 100. UDP travels over TCP inside the bonded connection.
- **Lifecycle**: the server waits for all legs up to the connect timeout; a leg that never
  arrives fails the connection. Bond does not add reliability: one failed leg fails the bonded
  connection.
- **Security**: no crypto of its own; each leg inherits its protocol's security. A weak leg
  exposes the part of the stream it carries.
- **Performance**: no benchmark; the benefit is aggregating throttled links, not lowering
  latency. Frame overhead and head-of-line blocking are unmeasured.
- **Licence**: GPL-3.0-or-later.
- **Priority**: P2. Interesting for multi-link CPEs but not a substitute for failover; needs a
  benchmark and a clear product story.

### 8.13 Psiphon

Outbound type `psiphon`, no build tag, 3.2k lines of glue plus the Psiphon tunnel core
([`outbound.go`](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/protocol/psiphon/outbound.go)).
Client only; Psiphon runs the servers, so there is no server of your own.

- **Design**: the official Psiphon core, built from the `CluvexStudio` `shirokhorshid` fork
  with CDN-fronted protocols and an edge scanner. The outbound can pick an exit region, uses a
  local SOCKS bridge to avoid loops, and supports hot reload.
- **Lifecycle**: starts asynchronously; shows "Connecting..." until the first tunnel; the core
  is restarted after 5 s, doubling to 5 min; a network change reconnects; `establish_tunnel_timeout`
  is 3 min; a deep search is offered when the normal search fails.
- **Performance**: shared infrastructure, often slower than a private server; TCP only. The
  scanner's benefit is network-dependent.
- **Security**: a mature, widely used core; the trade-off is a shared exit and a large
  third-party dependency.
- **Licence**: GPL-3.0.
- **Priority**: P2. A good last-resort outbound for a consumer app, but heavy for a router
  image and not a replacement for owned servers.

### 8.14 AmneziaWG

Endpoint type `awg`, build tag `with_awg`, 765 lines of glue around the AmneziaWG Go library
([`endpoint.go`](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/protocol/awg/endpoint.go),
[`option/awg.go`](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/option/awg.go)).

- **Registration**: `endpoint.Register` under `constant.TypeAwg`; stubbed without the tag.
- **Features**: junk packets, prefixes `s1`–`s4`, magic headers `h1`–`h4` (number or range),
  special packets `i1`–`i5`, protocol masquerade (`ip`, `id`, `ib`), header protection, content
  padding, random trailers, rekey/rekey-timeout/reject-after/keepalive/max-handshake-attempts,
  disable-cookies, peer `name`, `reserved` bytes for WARP, MTU discovery and fallback addresses.
- **Client/server**: both; a client with one peer dials out, a server sets `listen_port`.
- **Lifecycle**: a URL test runs right after start; MTU discovery is on by default (ICMP through
  the tunnel); `fallback_address` tries other addresses of the single peer when no handshake
  comes; the status tracker reports readiness.
- **Security**: WireGuard's Noise/ChaCha20-Poly1305 crypto; the obfuscation is not a security
  boundary. Key distribution is the same as WireGuard.
- **Licence**: AmneziaWG is MIT; the endpoint code is GPL-3.0.
- **Local comparison**: local AWG is a flat option set on the `wireguard` endpoint, with the
  core 2.0/3.1 fields but not `rekey_timeout`, `reject_after_time`, `keepalive_timeout`,
  `max_handshake_attempts`, peer names, MTU discovery or fallback addresses
  ([`option/wireguard_awg.go`](../../option/wireguard_awg.go), [AWG note](awg.md)).
- **Priority**: P1 for parity. The local fork already has official-interop tests (synthetic UAPI,
  isolated Linux namespace); port the missing knobs and lifecycle features, then verify them
  against the official implementation. Hiddify does not state official 3.1 interop in its docs;
  that remains unknown.

### 8.15 TrustTunnel

Inbound and outbound type `trusttunnel`, no build tag, 296 lines of glue around
`xchacha20-poly1305/sing-trusttunnel`
([`inbound.go`](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/protocol/trusttunnel/inbound.go),
[`outbound.go`](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/protocol/trusttunnel/outbound.go)).

- **Design**: HTTP `CONNECT` streams inside HTTPS, over HTTP/2 on TLS or HTTP/3 on QUIC; one
  TLS/QUIC connection carries many streams; UDP uses a `CONNECT` to `_udp2` with framed packets;
  users are username/password; TLS is required for the outbound and for the inbound's UDP mode.
- **Lifecycle**: `health_check` sends a small request after 7 s idle; a network change drops
  connections and opens new ones; QUIC needs `with_quic`.
- **Performance**: no benchmark in the docs; HTTP/2 and HTTP/3 are a good fit for HTTPS-only
  networks.
- **Security**: standard TLS plus password authentication; the protocol is new but published
  by the TrustTunnel project (Apache-2.0). The Go library is GPL-3.0. Interop with the official
  Rust client/server was not tested here.
- **Priority**: P1. It fills the "HTTPS looks normal" slot, has both client and server, and the
  glue is small. Verify against the official implementation before shipping.

### 8.16 Sudoku

Inbound and outbound type `sudoku`, build tag `with_sudoku`, 455 lines of own code, ported
from sing-box-extended
([`outbound.go`](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/protocol/sudoku/outbound.go)).
Each byte is encoded as four 4×4 sudoku clues; the wire looks like text or low-entropy data
instead of ciphertext.

- **Design**: 288 grids selected by the key; four clue bytes per data byte, so upload grows at
  least 4×; AEAD ChaCha20-Poly1305, AES-128-GCM or `none`; X25519 handshake; 60-second clock
  skew limit; `prefer_entropy`, `prefer_ascii` or split up/down tables; custom tables; padding.
- **HTTP mask**: `legacy`, `stream`, `poll`, `ws` or `auto`; TLS is not built in, so a CDN or
  reverse proxy must terminate it for HTTPS. A `fallback` passes non-Sudoku connections to a
  real site.
- **Lifecycle**: handshake timeout 5 s (HTTP modes at least 15 s); multiplexing `off`, `auto`
  or `on`; the server accepts several private keys for one public key.
- **Security**: a custom protocol with custom crypto, not audited. `aead_method: none` defeats
  the point; `prefer_ascii` is obfuscation, not encryption.
- **Testing**: Hiddify says it is tested only between two Hiddify instances, not against the
  original Sudoku or mihomo.
- **Licence**: GPL-3.0 per the repository LICENSE (GitHub reports NOASSERTION).
- **Priority**: P2. A useful differentiator against randomness-based DPI, but the 4× overhead
  and the lack of interoperability testing make it a niche tool.

### 8.17 Mieru

Inbound and outbound type `mieru`, no build tag, 735 lines of glue around `enfein/mieru/v3`
([`outbound.go`](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/protocol/mieru/outbound.go),
[`inbound.go`](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/protocol/mieru/inbound.go)).

- **Design**: all bytes are encrypted and padded; no TLS, no plaintext handshake, no fixed
  header; TCP or UDP port bindings; multiplexing levels; `HANDSHAKE_STANDARD` or `NO_WAIT`
  (0-RTT); the key is derived from username, password and the current 2-minute time step.
- **Client/server**: both; the official `mita` server and this inbound are alternatives.
- **Lifecycle**: sessions and streams; no explicit health check; close is handled on both
  sides.
- **Security**: custom crypto, not audited; clock skew breaks the connection; no TLS means no
  certificate or SNI to block, but also no standard TLS analysis.
- **Performance**: no benchmark; padding and encryption add overhead.
- **Licence**: GPL-3.0.
- **Priority**: P2. A solid "random traffic" option with both sides, but it needs interop tests
  with `mita` and a benchmark before it earns a release slot.

## 9. Priority and adoption

### 9.1 Ranking

| Priority | Items | Recommended action |
|---|---|---|
| **P1** | DNSTT, TrustTunnel, sing-tun, ray2sing, AmneziaWG, selected engine fixes | Port or depend now; these close concrete gaps and fit the GPL codebase |
| **P2** | Mieru, Psiphon, Bond, Sudoku, MasterDNS, Soar | Prototype after P1; each needs one decisive test (interop, benchmark or product fit) |
| **P3** | XDRIVE, GooseRelay, Call Tunnel, Router Mode/core | Defer; niche, slow, account-dependent or licence-encumbered |
| **P4** | Hiddify app | Reference only; non-commercial licence and out of scope |

### 9.2 Suggested sequence

1. **DNSTT upgrade.** Keep the local original-compatible wire format. Port the resolver
   pretest, scores, smart pool, DoH/DoT with uTLS, and reconnect policy. This is the largest
   resilience gain for users behind DNS-only networks, and the local code is already there.
2. **sing-tun fixes.** Port the leftover cleanup, address-conflict handling, UDP ephemeral
   ports and system-accept recovery. These are small, testable and reduce field failures.
3. **TrustTunnel.** Add the outbound and inbound through `sing-trusttunnel`, then test against
   the official Rust endpoint. It is the best "HTTPS-only network" candidate.
4. **AmneziaWG parity.** Add the missing 3.1 knobs, MTU discovery, fallback addresses and peer
   names; keep the existing official-interop test as the gate.
5. **ray2sing parsers.** Reuse the CC0 parser code for the link schemes the product needs;
   avoid linking the whole engine into a small converter.
6. **Engine CPU work.** Adaptive processors, paced reads and read-ahead are independent of the
   protocol roadmap and have the clearest measured payoff. Port them one at a time with the
   existing benchmark harness.
7. **Monitoring and fallback.** Add delay/exit-IP monitoring and a first-success fallback group
   if the product needs automatic server choice; this is more valuable than another transport.
8. **Then reassess** Mieru, Psiphon, Bond, Sudoku, MasterDNS and Soar against real user
   networks. Do not add all six at once.

### 9.3 What to avoid

- **Call Tunnel** as a product feature. It is untested end to end, depends on accounts on
  third-party call services, and the link is the key. The operational and terms risk is larger
  than the benefit.
- **XDRIVE** as a daily transport. It is a last resort by design; the open liveness issue and
  the storage quota make it unsuitable for always-on routers.
- **Hiddify core/app code** in a commercial product. The non-commercial clause is explicit.
- **Wholesale engine merge.** Hiddify's tree is a broad fork on a pre-release line. Port
  individual, tested changes into the local fork's boundary instead.

### 9.4 Decision

Keep the reviewed baseline at upstream `v1.14.4` plus the local fork (`fe24739a`). Do not move
the baseline to Hiddify. Port P1 items behind the existing registries and dialer, record each
decision in the [transport guide](transports.md), and re-run the fork-boundary check. Revisit P2
only with an interoperability test or a network measurement that the local baseline lacks.

## 10. Licence notes

| Component | Licence at pin | Reuse note |
|---|---|---|
| sing-box-vpnctl | GPL-3.0-or-later plus naming clause | Inherits the [upstream wording](../../LICENSE) |
| hiddify-sing-box | GPL-3.0-or-later plus naming clause | The pinned engine uses the same licence text as this fork |
| hiddify-core | GPL-3.0 + Section 7 terms, including non-commercial | Not usable in a commercial product without permission |
| hiddify-app | GPL-3.0 + non-commercial, naming, source and share-alike terms | Not usable in a commercial product without permission |
| ray2sing | CC0-1.0 | Code is public domain; a binary linking sing-box is GPL |
| sing-tun | GPL-3.0-or-later | Compatible with this fork |
| DNSTT / VayDNS | CC0-1.0 | Compatible |
| MasterDnsVPN | MIT | Compatible |
| Soar | Apache-2.0 | Compatible |
| GooseRelayVPN | MIT | Compatible |
| TrustTunnel | Apache-2.0 (protocol), GPL-3.0 (Go library) | Compatible; check both |
| Psiphon | GPL-3.0 | Compatible |
| AmneziaWG | MIT | Compatible |
| Sudoku | GPL-3.0 | Compatible |
| Mieru | GPL-3.0 | Compatible |
| Xray-core / XDRIVE | MPL-2.0 | Compatible with GPL-3.0 in combination; keep file notices |

The Hiddify core and app licence files expressly restrict commercial use. GitHub reports both as
`NOASSERTION`. The engine, the local fork and upstream retain the notice requiring prior consent
for use of the name or an implication of association. Recheck the licence of each specific source
file and dependency before importing it.

## 11. Coverage gaps

- **No execution.** Every performance statement is from documentation or a vendor benchmark.
  No protocol was run, no server was deployed, no throughput was measured.
- **No interoperability testing.** DNSTT/VayDNS, TrustTunnel, Mieru, Sudoku, AmneziaWG 3.1 and
  XDRIVE were not tested against their official or reference implementations. Hiddify itself
  says Call Tunnel is untested end to end and Sudoku is tested only between Hiddify instances.
- **Exact Hiddify base unknown.** The engine README says v1.15, but there is no pinned upstream
  base file; the branch is a fast-moving pre-release line. A future rebase can change the
  comparison.
- **No independent security review.** The custom protocols (Sudoku, Mieru, MasterDNS, the call
  tunnel, the Bond framing) have not been audited here. The security column is a design
  assessment, not a code audit.
- **Vendor benchmarks.** MasterDNS's 9× and Soar's "fastest" claims come from their projects.
  The engine CPU numbers are Hiddify's own, on macOS/Linux, and were not reproduced.
- **Router Mode not run.** The code and docs are clear, but the nftables/nfqueue behaviour,
  OpenWrt packaging and MikroTik script were not executed on hardware.
- **App not run.** Android/iOS manifests and the pubspec were read; store builds, Sentry
  configuration, per-app routing and the extension sandbox were not assessed at runtime.
- **Psiphon is dynamic.** Its server list, CDN edges and protocols change; a static read cannot
  predict today's behaviour on a given network.
- **Hiddify Manager** is listed for context, not assessed.
- **Adjacent Hiddify features outside the requested list** — WARP and WARP over MASQUE, mKCP,
  finalmask, the reverse Tunnel, the SSH inbound and VLESS post-quantum encryption — are
  mentioned only as context and are not assessed as candidates.
- **Stars are not quality.** The star counts in §5.1 are popularity signals at the pin, not
  evidence of correctness or security.

## 12. References

### Hiddify

- Engine: <https://github.com/hiddify/hiddify-sing-box/tree/4891a0c68ef27353dfaab3a35832239e60176e7f>
- Engine feature index: [docs/hiddify/index.md](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/docs/hiddify/index.md)
- Registry: [include/registry.go](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/include/registry.go)
- Build tags: [Makefile](https://github.com/hiddify/hiddify-sing-box/blob/4891a0c68ef27353dfaab3a35832239e60176e7f/Makefile)
- Core: <https://github.com/hiddify/hiddify-core/tree/4e5a9a357003d0a995360586702fd24c53042e3f>
- Router Mode: [docs/router-mode.md](https://github.com/hiddify/hiddify-core/blob/4e5a9a357003d0a995360586702fd24c53042e3f/docs/router-mode.md)
- App: <https://github.com/hiddify/hiddify-app/tree/66c17f83a55233ef4a1c80a4606df811227cc210>
- Converter: <https://github.com/hiddify/ray2sing/tree/a722bf1b184d7fcf10248dee45fc099d62646ca8>
- sing-tun fork: <https://github.com/hiddify/sing-tun/tree/fe80f0918269863a13628e53e4557b038882504f>

### Upstream projects

- sing-box v1.14.4: <https://github.com/SagerNet/sing-box/releases/tag/v1.14.4>
- sing-tun: <https://github.com/SagerNet/sing-tun/tree/2e93ae4ab5ab5f838a7a6014d1082edebda4f5cf>
- DNSTT: <https://www.bamsoftware.com/software/dnstt/>
- VayDNS: <https://github.com/net2share/vaydns/tree/a0ff70110d5e96686ab8f4c8e1c44cd09be07a75>
- MasterDnsVPN: <https://github.com/masterking32/MasterDnsVPN/tree/7198b45b86c7def2e226e41bcca3eca8ca2ea502>
- Soar: <https://github.com/getlantern/soar/tree/007179d4b40bd589d2b0240abffae33c43899839>
- GooseRelayVPN: <https://github.com/Kianmhz/GooseRelayVPN/tree/0ed803f635890cbc021f71d7c8746ca1d2f262b1>
- TrustTunnel: <https://github.com/TrustTunnel/TrustTunnel/tree/113cdc89559b9d2698e1477b3d0af89e95c0b7cf>
- sing-trusttunnel: <https://github.com/xchacha20-poly1305/sing-trusttunnel/tree/27f689c5105a7f20bc64d5865c49f481cb8ca68c>
- Psiphon: <https://github.com/Psiphon-Labs/psiphon-tunnel-core/tree/5dccaa36ae176ba974bcebc2124f172ecec51b08>
- AmneziaWG: <https://github.com/amnezia-vpn/amneziawg-go/tree/08fe3d33bbb116be09a6183d02609311b8b0c9e8>
- Mieru: <https://github.com/enfein/mieru/tree/9812207bb8aefc3dd4dcc6a5ce268bf5e28b6c54>
- Sudoku: <https://github.com/SUDOKU-ASCII/sudoku/tree/b1d1f0e767653d2c70e2ff814a01264c7dedf1f4>
- Xray-core: <https://github.com/XTLS/Xray-core/tree/b56ffef338c8096c7dbe78eaa76f1cb05a328d6f>
- XDRIVE liveness issue: <https://github.com/XTLS/Xray-core/issues/7208>

### Local

- [Architecture](../../ARCHITECTURE.md)
- [AWG options](awg.md)
- [XHTTP options](xhttp.md)
- [DNSTT](dnstt.md)
- [Builds and releases](build.md)
- [Fork boundary](../../release/FORK_OWNED)
- [Transport guide](transports.md)
- [Upstream inventory](upstreams.json)
- [Alternative transports and forks](transport-projects.md)
