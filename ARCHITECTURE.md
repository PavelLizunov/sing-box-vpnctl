# sing-box-vpnctl Architecture Specification

## 1. Foundation & Maintenance Model
`sing-box-vpnctl` is a hardened, additive distribution tracking official release tags of **SagerNet/sing-box** (`https://github.com/SagerNet/sing-box.git`), starting with **v1.14.0** (commit `0b8995879f29a9b98ee027bc17b75e101445b238`).

### Design Philosophy
- **Clean Baseline Tracking**: Maintain strict ancestry from official upstream tags. No divergence or rewriting of upstream interfaces.
- **Additive Extension Layer**: All custom features (XHTTP, AmneziaWG, Anti-Censorship DNS) attach cleanly behind modular interfaces and build tags.
- **Zero Build-Time Monkey Patching**: In-tree submodules contain all stability patches so clean `go build` works out of the box on all operating systems.

---

## 2. Core Subsystems

### A. WireGuard / AmneziaWG Engine (`with_awg`)
The in-tree `submodules/wireguard-go` is rebased on `sagernet/wireguard-go@8bd032a`, preserving all sing-box 1.14.0 internal features (`InputPackets`, `SetSinglePeerMode`, `SetEgressProvider`, `SetIOActivityFuncs`, `AllowedIPs.LookupFromPacket`).
- **AmneziaWG 2.0**: Handshake and packet framing obfuscation (`Jc`, `Jmin`, `Jmax`, `S1`–`S4`, `H1`–`H4`, `I1`–`I5` CPS, domain masquerade).
- **AmneziaWG 3.1 Extensions**: `RandomTrailers`, `DisableCookie`, `header_protection_key`, `content_padding_addition`, `rekey_after_time`.
- **In-Tree Stability & Performance Patches**:
  - H4 reserved-byte receive-clear gate in both standard socket and Windows WinRing RIO paths (prevents packet misclassification in AWG transport).
  - Fast-path data packet classification in `DeterminePacketTypeAndPadding` (1.5 ns/op, zero allocs).
  - Lock-free ChaCha20 header protection cipher (`atomic.Pointer`).
  - Zero-alloc slice expansions in outbound packet queues.
  - OOB nil-guard (`golang/go#77875`).
  - Send retry on `WSAENOBUFS` (10055 on Windows).
  - `ClientBind` WARP reserved byte preservation for AWG magic headers.

### B. XHTTP Transport Layer (`with_xhttp`)
Modular client transport implemented in `transport/v2rayxhttp` and registered via `transport/v2ray/registry.go`:
- **Modes**: `auto`, `packet-up`, `stream-up`, `stream-one`.
- **Performance**: Pre-allocated padding string slices (zero heap allocations) and direct URL path formatting without intermediate map encoding.
- **Race-Free Lifecycle**: Two-phase `watchDialContext` to prevent false cancellations when streams are established concurrently with dial context expiration.
- **Circuit Breaker (SPEC 076)**: Prevents CPU spinning upon hostile CDN resets.
- **Graceful GOAWAY Retries**: Replayable request bodies for transparent session survival.
- **Pool Hygiene (SPEC 059)**: Accurate tracking of pooled connections under load.

### C. Censorship-Resistant DNS Subsystem
- **Anti-ECH Protection**: Filtering `query_type: ["HTTPS", "SVCB"]` to mitigate ISP/TSPU RST resets on Cloudflare domains.
- **Strict Tunnel DNS**: Decoupling DNS resolution from the local network via proxy detours.
- **FakeIP Engine**: Instant synthetic IP allocation (198.18.0.0/15) bypassing local DNS filtering.

### D. Clash API Integrity
Authenticated HTTP Clash API (`experimental.clash_api`) is preserved across all targets, including Android `libbox.aar`.

---

## 3. Release Artifacts & Checksums
Every GitHub Actions release publishes:
1. `sing-box-windows-amd64.zip` + `.sha256`
2. `sing-box-windows-arm64.zip` + `.sha256`
3. `sing-box-linux-amd64.tar.gz` + `.sha256`
4. `sing-box-linux-arm64.tar.gz` + `.sha256`
5. `sing-box-linux-armv7.tar.gz` + `.sha256`
6. `sing-box-darwin-universal.zip` + `.sha256`
7. `libbox.aar` (`libbox-${VERSION}.aar`) + `.sha256`
8. `libbox-legacy.aar` + `.sha256`
9. `SHA256SUMS` manifest containing all asset hashes
