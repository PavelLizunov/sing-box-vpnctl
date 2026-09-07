# vpnctl.5 security execution evidence (in progress)

Baseline: origin/main and tag v1.14.0-vpnctl.4 = 91068e67ea244f4cdfea991f33cf3dfa2a259511. Remote inspected 2026-09-07; .5 free. Task branch fix/security-vpnctl5, isolated worktree /var/lib/dsh/Project/sing-box-security-vpnctl5. Shared sing-box-core untouched (preexisting untracked .dsh/).

Worker: linux-worker, tester, /home/tester/sing-box-security-vpnctl5; Go /home/tester/.local/opt/awg31-go1.27.1/go/bin/go = go1.27.1 linux/amd64. Preflight /home/tester and /tmp 58GiB available. No cleanup, deployments, service restarts, benchmarks, or published changes.

Every snapshot below committed locally, transferred via git bundle, fetched and checked out detached by exact SHA on dedicated worker clone. No mutable source synchronization.

| Fix | RED commit and observed failure | GREEN commit and observed result |
|---|---|---|
| CPS packet bounds | a1b6f261, device TestCPSPacketBounds: exit1; r/rc/rd/dz -1,65508,1073741824 accepted; oversized chain accepted | 05607483, same test exit0 |
| UAPI string injection | 0d6c795e, TestAwgRejectLineInjection: exit1; CR/LF/CRLF accepted for I1-I5/CPA/rekey | 70dc6817, same test exit0 |
| Raw header CRLF | 7ac70bd1, TestMagicHeaderRejectCRLF: exit1; trailing/leading/internal CRLF and JSON accepted | 0dc1d139, same test and UAPI tests exit0 |
| Session randomness / entropy | 096e1d5c, TestSessionAlphabetUniqueEntropy and TestSessionRandomSource: exit1 duplicate entropy and missing crypto/rand | d8c555d8, same tests exit0 |
| Stream-up status | 7bc83593, TestStreamUpRejectUploadStatus: exit1 Write still blocked after 1s | f023468b, -race TestStreamUp and TestSplitConnTerminalRaces exit0 |

Exact commands (relative to worker root; GO is absolute path above):

- CPS: `(cd submodules/wireguard-go && $GO test ./device -run TestCPSPacketBounds -count=1)`.
- UAPI: `$GO test -tags with_awg ./transport/wireguard -run TestAwgRejectLineInjection -count=1`.
- Header: `$GO test -tags with_awg ./option ./transport/wireguard -run 'TestMagicHeaderRejectCRLF|TestAwgRejectLineInjection' -count=1`.
- Session: `$GO test ./transport/v2rayxhttp -run 'TestSessionAlphabetUniqueEntropy|TestSessionRandomSource' -count=1`.
- Upload RED: `$GO test ./transport/v2rayxhttp -run TestStreamUpRejectUploadStatus -count=1`.
- Upload GREEN: `$GO test -race ./transport/v2rayxhttp -run 'TestStreamUp|TestSplitConnTerminalRaces' -count=1`.

Security interpretation: numeric/chain allocation hazards and UAPI line injection source verified directly. Session RNG change is hardening, not proof of predictable production IDs or session takeover. No unsafe large allocation was needed by the regressions.

## Review repairs and candidate verification

- Valid masquerade values with trailing CR/LF: RED 62a50d1c; GREEN f21c1735. Raw string validation now precedes sugar normalization.
- Failed split connection retained deadline timers: RED f21c1735; GREEN 3c75c518 (full XHTTP race suite exit0). Both timers stopped, later rearming stopped too.
- Later expired read deadline masked terminal upload error: RED 32d341b5; GREEN 4aa39602. Repeated Read preserves first terminal error.
- Release policy Python tests: RED 1f64d5ab (3 failures), GREEN c06b2aee (3 pass). Actions verified via git ls-remote upstream; nttld/setup-ndk annotated v1 uses peeled commit ed92fe6cadad69be94a966a7ee3271275e62f779. Native attest v4 contract fetched from upstream pinned action.yml.
- 8512213a device `go test -race ./device -count=1` exit0 (3.614s). Device source unchanged since.
- 4aa3960205ac91346f670caf6ad019736b4928b3 option/AWG/XHTTP `go test -race -tags with_awg ... -count=1` exit0 (1.017/1.266/2.156s).
- Full production-tag suite initially failed: omitted required `-ldflags=-checklinkname=0` broke experimental/libbox and boxdd test linking; corrected command resolves that. Independently, TestUnshareNamespace fails `operation not permitted` on this unprivileged worker. No privilege or host configuration changes made.
- Corrected `$GO test -ldflags=-checklinkname=0 -tags "$TAGS" -skip TestUnshareNamespace ./...` at 4aa39602 exit0. This is NOT an unqualified full-suite pass: the namespace test remains untested in this worker environment.
- CGO_ENABLED=0 production-tag build at 4aa39602 exit0. Binary version output: 1.14.0-vpnctl.5, Go1.27.1 linux/amd64, all release tags, Revision4aa3960205ac91346f670caf6ad019736b4928b3, CGO disabled.
- Parent independently reviewed 91068e67..8512213a and deltas through 4aa39602. Initial reviewer findings repaired; parent accepted source and conditional evidence with explicit namespace boundary, pending final feature-tag race check and CI.

Remaining: final formatting/evidence commit, push branch/PR and green CI, parent pre-tag acceptance, release matrix publication, cryptographic verification of published provenance/revision/hashes/version. This is not a completion report.
