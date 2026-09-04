# sing-box-vpnctl

<p align="center">
  <strong>Hardened universal proxy platform with native AmneziaWG (2.0 & 3.1), XHTTP, and Anti-Censorship DNS</strong><br>
  The official core engine powering both <a href="https://github.com/PavelLizunov/VPNRouter">VPNRouter</a> (clients: Windows, macOS, Linux, Android) and <a href="https://github.com/PavelLizunov/vpnctl">vpnctl</a> (server nodes and control plane).
</p>

---

## 🎯 Overview

`sing-box-vpnctl` is a production-grade, hardened distribution of **[SagerNet/sing-box](https://github.com/SagerNet/sing-box)** (tracking official stable releases starting from v1.14+) purpose-built to provide:

1. **First-Class AmneziaWG Support**: Native WireGuard obfuscation supporting both **AmneziaWG 2.0** (`Jc`, `Jmin`, `Jmax`, `S1`–`S4`, `H1`–`H4`, `I1`–`I5` CPS, masquerading) and **AmneziaWG 3.1** (`RandomTrailers`, `DisableCookie`, `header_protection_key`, `content_padding_addition`).
2. **XHTTP Transport (SplitHTTP)**: Native support for Xray-compatible HTTP transport (`with_xhttp`), featuring:
   - Modes: `auto` (auto-stream-one with Reality, otherwise packet-up), `packet-up`, `stream-up`, `stream-one`.
   - **Circuit Breaker (SPEC 076)**: stops 100% CPU spinning when CDNs reset upload streams by backing off and retiring failed connections.
   - **Graceful GOAWAY Retries**: replayable request bodies survive HTTP/2 GOAWAY frames without dropping sessions.
   - **Connection Pool Hygiene (SPEC 059)**: leak-free `openUsage` tracking under concurrent load.
3. **Censorship-Resistant DNS Subsystem**:
   - **Anti-ECH / SVCB Type 65 Filter**: prevents TSPU/DPI TCP resets caused by encrypted ClientHello on Cloudflare-fronted domains by selectively rejecting or filtering HTTPS/SVCB DNS records.
   - **Zero-Leak Tunnel DNS (Detour)**: enforces complete DNS encapsulation inside proxy tunnels, keeping physical WAN free of inspectable DNS queries.
   - **Fast FakeIP Resolver**: immediate synthetic IP mapping (198.18.0.0/15) avoiding DNS blocking altogether.
   - **Optimistic Caching & Parallel Queries**: sing-box 1.14 optimistic cache with parallel resolver racing.
4. **Guaranteed Clash API Compatibility**: Preserves authenticated HTTP Clash API endpoints across all platforms, including mobile Android (`libbox.aar`), avoiding proprietary gRPC locks.
5. **Independent Supply Chain**: Fully decoupled from third-party forks (`Leadaxe/sing-box-lx`), building reproducible signed binaries and AAR packages directly from our verified GitHub Actions CI.

---

## 📦 Cross-Platform Build Matrix

| Platform | Target Architecture | Binary / Artifact | Build Tags |
| :--- | :--- | :--- | :--- |
| **Windows** | `amd64`, `arm64` | `sing-box.exe` | `with_gvisor,with_quic,with_dhcp,with_wireguard,with_utls,with_clash_api,with_naive_outbound,with_purego,with_xhttp,with_awg` |
| **Linux** | `amd64`, `arm64`, `armv7` | `sing-box` | `with_gvisor,with_quic,with_dhcp,with_wireguard,with_utls,with_clash_api,with_naive_outbound,with_purego,with_xhttp,with_awg` |
| **macOS** | Universal (`amd64` + `arm64`) | `sing-box` | `with_gvisor,with_quic,with_dhcp,with_wireguard,with_utls,with_clash_api,with_naive_outbound,with_purego,with_xhttp,with_awg` |
| **Android** | `arm64-v8a`, `armeabi-v7a`, `x86_64` | `libbox.aar` | gomobile bundle with `with_clash_api,with_awg,with_xhttp` |

---

## 🛡️ AmneziaWG Capabilities (2.0 vs 3.1)

```
                       ┌──────────────────────────────────────┐
                       │           sing-box-vpnctl            │
                       └──────────────────┬───────────────────┘
                                          │
                  ┌───────────────────────┴───────────────────────┐
                  ▼                                               ▼
     ┌────────────────────────┐                      ┌────────────────────────┐
     │     AmneziaWG 2.0      │                      │     AmneziaWG 3.1      │
     ├────────────────────────┤                      ├────────────────────────┤
     │ • Junk Packets (Jc)    │                      │ • All 2.0 Obfuscation  │
     │ • Junk Range (Jmin/max)│                      │ • Random Trailers      │
     │ • Init Padding (S1)    │                      │ • Disable Cookie Reply │
     │ • Resp Padding (S2)    │                      │ • Header Protection    │
     │ • Transport Junk (S4)  │                      │ • Content Padding Add  │
     │ • Magic Headers (H1-H4)│                      │ • Rekey After Time     │
     │ • CPS Packets (I1-I5)  │                      └────────────────────────┘
     └────────────────────────┘
```

---

## 🌐 Anti-Censorship DNS Recipes

### Mitigating Russian TSPU ECH Drops (HTTPS/SVCB Type 65)
To prevent DPI boxes from dropping TLS connections when browsers attempt ECH:
```json
{
  "dns": {
    "rules": [
      {
        "query_type": ["HTTPS", "SVCB"],
        "action": "reject"
      }
    ]
  }
}
```

### Strict Tunnel DNS (No WAN Leaks)
```json
{
  "dns": {
    "servers": [
      {
        "tag": "remote-dns",
        "address": "https://1.1.1.1/dns-query",
        "detour": "proxy-out"
      }
    ]
  }
}
```

---

## 🚀 Building from Source

### Prerequisites
- **Go**: `1.25` or `1.26`
- **Git**: `>= 2.30`
- **CGO / Cross-compiler**: Required for Android gomobile (`libbox.aar`)

### Desktop CLI Build (Windows / Linux / macOS)
```bash
# Clone the repository
git clone https://github.com/PavelLizunov/sing-box-vpnctl.git
cd sing-box-vpnctl

# Build standard release binary
go build -trimpath \
  -tags "with_gvisor,with_quic,with_dhcp,with_wireguard,with_utls,with_clash_api,with_naive_outbound,with_purego,badlinkname,tfogo_checklinkname0,with_xhttp,with_awg" \
  -ldflags "-s -w -checklinkname=0 -X github.com/sagernet/sing-box/constant.Version=1.14.0-vpnctl.1" \
  -o sing-box ./cmd/sing-box
```

---

## 🗺️ Roadmap

- [x] **Phase 1**: Baseline Upstream Sync (sing-box `v1.14.0` official stable).
- [x] **Phase 2**: WireGuard-go AWG 2.0 & XHTTP integration (`with_awg`, `with_xhttp`).
- [ ] **Phase 3**: AmneziaWG 3.1 features port (`RandomTrailers`, `DisableCookie`, `header_protection_key`, `content_padding_addition`).
- [ ] **Phase 4**: GitHub Actions CI workflow for multi-platform binary compilation and release publishing.
- [ ] **Phase 5**: VPNRouter and vpnctl integration and end-to-end verification.

---

## 📄 License
Licensed under GPLv3 in accordance with upstream SagerNet sing-box specifications.
