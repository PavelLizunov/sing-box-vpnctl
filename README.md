# sing-box-core

<p align="center">
  <strong>Next-generation universal proxy platform with native AmneziaWG (2.0 & 3.1) and XHTTP</strong><br>
  The official core engine powering <a href="https://github.com/PavelLizunov/VPNRouter">VPNRouter</a> across Windows, macOS, Linux, and Android.
</p>

---

## 🎯 Overview

`sing-box-core` is a production-grade, hardened fork of **[SagerNet/sing-box](https://github.com/SagerNet/sing-box)** (v1.14+ stable) purpose-built to provide:
1. **First-Class AmneziaWG Support**: Native WireGuard obfuscation supporting both **AmneziaWG 2.0** (`Jc`, `Jmin`, `Jmax`, `S1`, `S2`, `H1-H4`) and **AmneziaWG 3.1** (`RandomTrailers`, `DisableCookie`, `header_protection_key`, `content_padding_addition`).
2. **XHTTP Transport**: Native support for Splice / Xray HTTP transport (`with_xhttp`).
3. **Guaranteed Clash API Compatibility**: Unlike other forks that strip `experimental.clash_api`, `sing-box-core` preserves authenticated HTTP Clash API endpoints across all platforms, including mobile Android (`libbox.aar`).
4. **Independent Supply Chain**: Fully decoupled from third-party forks (`Leadaxe/sing-box-lx`), building reproducible signed binaries and AAR packages directly from our verified GitHub Actions CI.

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
                       │           sing-box-core              │
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
     │ • Magic Headers (H1-H4)│                      │ • Content Padding Add  │
     └────────────────────────┘                      └────────────────────────┘
```

> **Note on Compatibility**: AmneziaWG 2.0 and 3.1 are distinct protocol revisions. `sing-box-core` automatically negotiates or configures the requested mode based on configuration fields, failing closed when server requirements conflict.

---

## 🚀 Building from Source

### Prerequisites
- **Go**: `1.25` or `1.26`
- **Git**: `>= 2.30`
- **CGO / Cross-compiler**: Required for Android gomobile (`libbox.aar`)

### Desktop CLI Build (Windows / Linux / macOS)
```bash
# Clone the repository
git clone https://github.com/PavelLizunov/sing-box-core.git
cd sing-box-core

# Build standard release binary
go build -trimpath \
  -tags "with_gvisor,with_quic,with_dhcp,with_wireguard,with_utls,with_clash_api,with_naive_outbound,with_purego,badlinkname,tfogo_checklinkname0,with_xhttp,with_awg" \
  -ldflags "-s -w -checklinkname=0 -X github.com/sagernet/sing-box/constant.Version=1.14.0-vr.1" \
  -o sing-box ./cmd/sing-box
```

---

## 🗺️ Roadmap

- [ ] **Phase 1**: Initial upstream SagerNet `v1.14.0` baseline setup.
- [ ] **Phase 2**: WireGuard-go AWG engine integration (`with_awg`).
- [ ] **Phase 3**: AmneziaWG 3.1 features port (`RandomTrailers`, `DisableCookie`).
- [ ] **Phase 4**: GitHub Actions CI workflow for multi-platform binary compilation and release publishing.
- [ ] **Phase 5**: VPNRouter integration and automated verification on `WINBRAT`.

---

## 📄 License
Licensed under GPLv3 or proprietary dual-license in accordance with upstream SagerNet sing-box specifications.
