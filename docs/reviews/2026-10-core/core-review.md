# Core build review — 2026-10-04

## Verdict
Reviewed commit: `9b6b9bbf3c05d17afd693e5e66424f60d728ce6c` on `task/stable-core-update-20261004`.
Stable sing-box ancestry, compilation and primary protocol tests pass. Do not release this revision with an unrestricted claim of working AmneziaWG 3.1: two optional AWG extensions fail real local handshakes. No production services, tracked source files, credentials, releases or Git history were changed during this review.

## Confirmed findings

### AWG-01 — high: random_trailers handshake packets rejected
Evidence basis: source_and_reproduced.
Sender appends trailers in embedded device/send.go:253 and :314. DeterminePacketTypeAndPadding recognizes oversized handshake packets when randomTrailers is true, but RoutineReceiveIncoming still enforces exact MessageInitiationSize/MessageResponseSize at receive.go:207-218. Consequently packets are discarded before MAC verification/handshake processing.

Reproduction: two real devices on loopback UDP, generated keys, userspace fake TUN, S1-S4=20, distinct H1-H4, random_trailers=true on both sides. Three repeated trials failed to converge endpoint/handshake within two seconds; the basic AWG profile passed. A temporary Go overlay that only trims initiation/response trailers before the exact-length guards restored all three handshakes. This is causal proof, not a production-ready repair: transport packets, cookies and authenticated packet lengths still need proper treatment.

### AWG-02 — high: header_protection_key does not unwrap inbound packets
Evidence basis: source_and_reproduced.
Sender encrypts the packet after its nonce at device/send.go:248-250 and :309-311. Receiver only partially derives an XOR hash for the type in DeterminePacketTypeAndPadding (receive.go:605-609, :624-628); it never fully decrypts inbound packet bytes before field extraction, MAC validation and handshake unmarshalling. Config acceptance therefore does not prove interoperability.

Reproduction: the same two-device loopback setup with S1-S4=20 and a shared synthetic header protection key. Three repeated trials failed. A temporary overlay that fully unwraps before classification and avoids the duplicate type hash restored all three handshakes. Overlay is a diagnostic prototype only: combined profiles, nonce placement, data packets and cookies require a complete implementation and regression tests.

### Origin/attribution
Both failing paths predate the stable upgrade. Substituting the pre-update `db2db8a6` send.go/receive.go with another diagnostic overlay reproduced both failures while the basic AWG profile continued to pass. No introduced cause for these two defects was found in the stable merge. Nevertheless, our prior claim of readiness was too broad: earlier tests covered IPC strings and default handshakes, not these AWG 3.1 profiles.

## Passed checks
- v1.14.2 is an ancestor; documented merge commit/working revision match.
- Actual `.github/workflows/release.yml` DEFAULT_TAGS build on Linux amd64, including with_naive_outbound and with_purego; version reports reviewed revision and AWG/XHTTP tags.
- Exact smoke-test JSON from `.github/workflows/ci.yml` passes compiled CLI check.
- Same exact release tags cross-build Linux arm64/armv7, Windows amd64/arm64, macOS amd64/arm64; all six commands exit 0.
- Uncached complete general test suite with broad default features and required -checklinkname=0 passes; TestUnshareNamespace explicitly skipped because privileged namespace access is unavailable.
- Embedded WireGuard device, AWG transport, XHTTP and option tests pass five race-enabled repetitions.
- Focused go vet passes; go mod verify passes.
- Real loopback basic AWG obfuscated handshake passes, including custom S1-S4 and H1-H4.

## Additional coverage observations
The actual release workflow DEFAULT_TAGS differs from release/DEFAULT_BUILD_TAGS_OTHERS. Prior checks used the latter; this review closes the CLI release-tag compilation gap, but does not package AAR/XCFramework.
The CI workflow smoke JSON exercises AWG 2.0 option parsing but has no AWG 3.1 runtime handshake coverage. Current device rekey tests exercise actual UAPI and threshold sampling; the wrapper's TestAwg31IpcLines still only verifies rendered strings.

Rejected hypothesis: full uint32 rekey range causes panic. A temporary overlay exercised 0-4294967295; the expected panic did not reproduce. No defect claimed for it.

## Evidence files
Published alongside this report, with no dependency on the original machine or temporary folders:
- `reproduce.py` generates checkout-relative Go overlays in a fresh temporary directory; `review_awg_handshake_test.go` contains the exact added loopback reproductions. It does not edit engine sources.
- `awg-handshake-Basic.log`, `awg-handshake-RandomTrailers.log`, `awg-handshake-HeaderProtection.log`.
- `random-trailers-trim-proof.log` and `review_receive_trim.go`: causal trailer experiment, not a production fix.
- `header-unwrap-proof.log` and `review_receive_unwrap.go`: causal header-protection experiment, not a production fix.
- `before-update-awg-proof.log`, `before-update-send.go`, `before-update-receive.go`: pre-existing-code attribution.
- `build-linux-arm64.log`, `build-linux-arm.log`, `build-windows-amd64.log`, `build-windows-arm64.log`, `build-darwin-amd64.log`, `build-darwin-arm64.log`. Empty successful compiler logs are retained; the return-code summary is in the Passed checks section.
- `tags.txt`, `ci-config.json`: actual Linux release build inputs. Compiled binaries are intentionally not published.
- `SHA256.json`: hashes of the published evidence. Original absolute-path overlays are replaced by the portable runner.

## Limits and recommendation
Local review, no new agents/campaign or independent judge. No real remote VPN server interoperability test, Android/iOS runtime, macOS socket execution, AAR/XCFramework packaging or loaded external Cronet runtime verification. Build success is not proof of runtime behavior on those systems.

Fix AWG-01/AWG-02 under a separate authorized implementation, add persistent loopback handshake plus payload/cookie tests for basic/trailers/header-protection/combined profiles, then rerun release builds and tests before publishing. Stable sing-box integration itself does not need reverting based on the observed evidence.
