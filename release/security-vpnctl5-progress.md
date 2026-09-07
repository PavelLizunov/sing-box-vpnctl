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

Remaining: format and strengthen edge tests, full AWG/device/XHTTP/race/full-tag tests and builds, release workflow pin/tag/no-clobber/attestation hardening, README and release notes, independent security review + parent evidence acceptance, push CI and release matrix, cryptographic verification of published provenance/revision/hashes/version. This is not a completion report.
