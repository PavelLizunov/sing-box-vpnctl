# sing-box-core Architecture Specification

## 1. Upstream Foundation
`sing-box-core` tracks the official release tags of **SagerNet/sing-box** (`https://github.com/SagerNet/sing-box.git`), starting with **v1.14.0** (commit `0b8995879f29a9b98ee027bc17b75e101445b238`).

### Core Branches
- `upstream-release`: Clean tracking branch of official `SagerNet/sing-box` release tags.
- `main`: Canonical production branch with AmneziaWG (2.0 & 3.1) and XHTTP integration.
- `release/*`: Tagged release cuts.

---

## 2. Downstream Patch Layer

### A. WireGuard / AmneziaWG Obfuscation (`with_awg`)
Upstream sing-box uses `golang.zx2c4.com/wireguard` and `sagernet/wireguard-go`.
In `sing-box-core`, the WireGuard outbound routes through `wireguard-go-awg`:
- Obfuscated packet framing in `device/send.go` and `device/receive.go`.
- UAPI handshake parameter parser for `Jc`, `Jmin`, `Jmax`, `S1`, `S2`, `H1`, `H2`, `H3`, `H4`.
- AmneziaWG 3.1 extensions:
  - `RandomTrailers`: Appends random entropy bytes to data packets.
  - `DisableCookie`: Disables cookie reply packets on handshake.

### B. Xray HTTP Transport (`with_xhttp`)
Ported from official XHTTP specifications to enable next-generation CDN/proxy multiplexing over HTTP/2 and HTTP/3.

### C. Clash API Integrity
`sing-box-core` guarantees `with_clash_api` support on all targets, including Android `libbox.aar`. The endpoint `/traffic`, `/connections`, and `/proxies` are retained and authenticated via secret token.

---

## 3. Release Artifacts & Checksums
Every GitHub Actions release publishes:
1. `sing-box-windows-amd64.zip` + `.sha256`
2. `sing-box-windows-arm64.zip` + `.sha256`
3. `sing-box-linux-amd64.tar.gz` + `.sha256`
4. `sing-box-linux-arm64.tar.gz` + `.sha256`
5. `sing-box-darwin-universal.zip` + `.sha256`
6. `libbox-android.aar` + `.sha256`
