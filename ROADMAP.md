# sing-box-core Delivery Roadmap

## Phase 1: Baseline Upstream Sync (sing-box 1.14.0)
- [x] Add remote upstream: `https://github.com/SagerNet/sing-box.git`.
- [x] Fetch and checkout official release tag `v1.14.0`.
- [x] Verify clean baseline Go 1.25/1.26 compilation.

## Phase 2: AmneziaWG 2.0 & XHTTP Integration
- [x] Import `wireguard-go-awg` subsystem based on SagerNet 1.14.0 with 16-parameter obfuscation.
- [x] Wire `with_awg` build tag into sing-box `transport/wireguard` and options.
- [x] Import modular XHTTP transport (`with_xhttp`) with circuit breaker and H2 connection pooling.
- [x] Integrate 4 critical stability patches in-tree (H4 clear gate, OOB nil-guard, WSAENOBUFS retry, WARP reserved guard).
- [x] Verify clean compilation and config validation on Go 1.26.

## Phase 3: AmneziaWG 3.1 Extensions
- [ ] Port `RandomTrailers` implementation from `amnezia-vpn/amneziawg-go` (commit `1f50ad73`).
- [ ] Port `DisableCookie` implementation.
- [ ] Update config schema for AWG 3.1 parameters.

## Phase 4: Cross-Platform CI / Release Automation
- [ ] Create `.github/workflows/release.yml`.
- [ ] Build matrix for Windows, Linux, macOS, and Android (`libbox.aar`).
- [ ] Automate `.sha256` checksum generation and release publishing.

## Phase 5: VPNRouter End-to-End Migration
- [ ] Update `VPNRouter/tools/build-singbox-lx.ps1` and build scripts to pull from `PavelLizunov/sing-box-core`.
- [ ] Run full E2E verification on `WINBRAT` test VM (`100.115.182.0`).
- [ ] Deprecate `Leadaxe/sing-box-lx` dependency.
