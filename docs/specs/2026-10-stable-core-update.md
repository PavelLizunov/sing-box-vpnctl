# Stable core update — 2026-10-04

## Outcome and authorization
User approved updating the stable sing-box base and maintaining embedded AWG/XHTTP compatibility. Implement and verify on a dedicated task branch; commit/push to origin. No deployments, restarts, published release tags, changes to credentials, or changes to unrelated `.dsh/` content.

## Baseline and targets
- Fork baseline: db2db8a6, v1.14.0-vpnctl.3; ancestor official v1.14.0 (0b8995879f29a9b98ee027bc17b75e101445b238).
- Rechecked upstream tags: stable sing-box v1.14.2 = af6e64c3b69e6132ebaee0e1a3d24e93903f6709. v1.15 alpha excluded.
- AWG reference: official amneziawg-go v3.1.20260828; kernel v3.1.20260906 and tools v3.1.20260828. Preserve sing-box-specific APIs and socket patches rather than replacing the vendored module.
- Xray reference: GitHub latest non-prerelease v26.3.27; v26.9.30 is prerelease, excluded as an automatic update. Preserve current richer native XHTTP port; review compatible fixes without downgrading it or importing the entire Xray engine.

## Invariants and scope
Merge stable upstream preserving ancestry and fork release tags, AWG/XHTTP build registration, DNS behavior, Clash user metadata, V2Ray API, mobile tags and local wireguard replacement. Resolve only task-owned conflicts. Preserve AWG defaults and cookie protection. Repair advertised `rekey_after_time` end-to-end using the official semantics, not just IPC string generation. Keep unsupported HTTP/3 and downloadSettings explicit rather than claiming full Xray equivalence. Update README/architecture and record provenance and limits.

## Verification
Check merged ancestry/diff and go.mod replacement. Run focused AWG and XHTTP tests, race tests where supported, general Go tests, full release-tag CLI build/check/version, and cross-platform compile checks as feasible. Verify actual UAPI acceptance/rejection/defaults for rekey config and packet/handshake regressions. No live external VPN credentials available: record lack of real server/device E2E. Go is not in PATH or common toolchain locations; acquire a task-local verified official toolchain if needed. Do not substitute worker claims for lead acceptance.

## Material unknowns
Merge conflicts/dependency API changes, official AWG timing range semantics and effect on existing timers, available Go/module network downloads, cross-platform SDK availability, origin push permissions. No remote worker use currently planned. All inspection/implementation is local.

## Progress
- Inspection: no tracked user changes; existing untracked `.dsh/` excluded.
- Latest sing-box stable target rechecked through git ls-remote.
- Stable v1.14.2 merged without conflicts; local wireguard replacement preserved.
- Applied complete SagerNet wireguard-go baseline 8bd032a91a3076bb09cb6103861343d973c5e289 -> v0.0.7 (7aa7121e681cc35748e7f0001c33dc1e2a8d3312) diff. Preserved AWG clearReserved handling on macOS receive path.
- Implemented official AWG v3.1 rekey_after_time range parsing/get/default/sample and send-path threshold; added UAPI and concurrent-update tests.
- Focused protocol tests and embedded device/socket/XHTTP race checks passed with Go 1.27.1.
- Initial broad test attempt failed: missing linkname compiler flag and privileged TestUnshareNamespace cannot run in this host. Re-running with Go 1.26.8 matching upstream release CI, -checklinkname=0 and explicitly skipping only TestUnshareNamespace. No privileged workaround attempted.
- Official Go downloads verified with SHA256; toolchains/build outputs are under task-specific /tmp directories, not tracked.
- Go 1.26.8 broad core suite passed with linkname flag and only privileged namespace test excluded. Focused race tests and go vet passed; embedded AWG tests include actual UAPI set/get/default/range/rejection.
- Linux amd64 complete DEFAULT_BUILD_TAGS_OTHERS binary built and version checked. Windows amd64, macOS arm64 and Linux arm64 CLI cross-compilation passed with AWG/XHTTP/QUIC/uTLS/API tags; no execution on those target operating systems.
- macOS cross-compile caught an initial adaptation typo; fixed receive header clearing to use reservedForEndpoint lookup under its lock, retaining AWG magic bytes. Recompiled successfully.
- Synthetic AWG configuration checked with the full binary; no real secrets used. Initial nested amnezia test fixture corrected to flat endpoint options per the existing schema.
- Whitespace warnings in upstream .github/go_ios_cpu_features.patch are expected unified-diff context lines; file preserved byte-for-byte from upstream.
- Final synthetic AWG and XHTTP CLI config checks passed. Full DEFAULT_BUILD_TAGS_OTHERS test suite passed with the documented namespace exclusion; go mod verify passed.
- Android arm64 and Apple darwin arm64 experimental/libbox package compilation passed; no AAR/XCFramework packaging or device execution performed.
- Independent Gemini reviews completed. XHTTP stable-reference review found no required wire/default correction, so no transport feature rewrite/downgrade performed. WireGuard review confirmed the stable API updates and identified a Darwin zero-read spin introduced by the connected-socket path.
- Backported upstream SagerNet commit 6731c7387c811275155632546b518c261a54f274 (2026-09-29): report ErrRebindRequired on zero receive count. This four-line correctness fix is newer than latest stable v0.0.7 but does not move the dependency to an unstable branch or add features. Latest stable tag rechecked: v0.0.7.
- Tested merge checkpoint 85b22ee5 pushed to origin/task/stable-core-update-20261004. Final Darwin CLI cross-build and embedded engine/XHTTP/WireGuard race rechecks passed after review fix. Task-owned diff passes whitespace checks.
- Acceptance: stable base integrated, preserved fork features, AWG rekey accepted/applied/tested, XHTTP stable compatibility retained, README/architecture updated. No production/device/live-server E2E performed; privileged namespace test, AAR/XCFramework packaging and target OS runtime checks remain outside this local verification.
- Final verified follow-up committed/pushed on the dedicated task branch; main remains unchanged. No release tag or deployment performed.
