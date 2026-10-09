# AWG31 validation status

User explicitly approved kernel-fix scope. Dedicated worktree /var/lib/dsh/Project/sing-box-awg31 branch fix/awg31-wire-interop baseline db2db8a637f4ee5c60a6b460d473b20f950f1827. No production changes or release.

## Round 9 successor / security and CI

Current reviewed local kernel successor **d186269410bec6dd7e2d2cf8a06c09f9cddf1605**. Reachable raw UAPI parser error log disclosed malformed-field HP marker despite static returned Endpoint error; parent confirmed tests-first baseline RED bash-92 (only malformed_padding_echo, exact error log secret assertion; no secrets printed), then static log fix independently reviewed SHA06dd8d7d2ffc21fca4c7a634f3d233e134238b4176007df7505d417035d27e97 no important/critical. Exact successor worker /home/tester/awg31-fixed-d1862694 passes transport -race (including real Start secret regression)1.857s and nested device -race3.404s bash-93. Full-tag rebuilt binary SHA **3f8fac378fe6389840f856c04dec69e75ec98edf6d830a0226bcd48d67f2ad43** bash-95; use this for remaining tests.

cae50a53 bounded fuzz15s2workers PASS795137execs bash-88. Root ./... bash-91 ONLY failure TestUnshareNamespace operationnotpermitted unprivileged worker, all other shown packages pass. Exact successor netns test compiled then ran sudo unshare fresh namespace bash-95 PASS0.01s. No host network touched.

Official native config tools v3.1.20260812 exact Git ee0f0a9aa34ff0a0da4b3433b9512781cfe02843 built on worker; actual binary name src/wg not src/awg. /home/tester/awg31-tools-ee0f0a9/src/wg SHA5eb2eca206cd7e2afbe21b02b6def2347b003f3a7d4cf5e622cd56b21b6d379a, banner correct and tracked diff clean. Ready for actual rendered .conf import test.

Parent bash-96 running git push -u origin fix/awg31-wire-interop then gh pr create (main) to trigger required CI; collect result, discover run and gh run watch --exit-status. origin verified github.com/PavelLizunov/sing-box-vpnctl.git; never upstream. No release/deploy yet.

## Round 8 authoritative green checkpoint

Local kernel commit **cae50a530015471dc40070ac1837ccfcd594b495**, branch fix/awg31-wire-interop, NOT pushed/released. Independent corrected full-diff review SHA2905bf9dd3cc853635c37945b7853c87ff51c43317c6913499ea527a84cc50f9 closed important+both minor findings, no remaining critical/important visible. Immutable bundle fetched into worker /home/tester/awg31-fixed-cae50a53 detached exact SHA; no mutable source synchronization.

Parent bash-79 nested full device suite PASS2.190s. Parent bash-80 nested full device -race PASS3.611s, root AWG transport PASS0.033s, full production-tag sing-box build PASS. Binary /home/tester/awg31-fixed-cae50a53/sing-box-test SHA256 **eb404fc54ea716b749038de400a0c2236486d3f97906c0c68e9ad9d463f52e42** (test version1.14.0-vpnctl.4-test, not release).

Parent bash-81 official interop matrix **16/16 PASS, exit0** in fresh root NET namespace. Both roles official server/client for AWG2, HP S12, HP S>12, CPA1, trailers, combined, native-conf-equivalent combined. Every positive exact HTTP+UDP, live processes+native handshake. WrongHP both roles require independent positive baseline then fresh mismatch gives TCP+UDP timeouts, handshake false, daemons alive. Official binary digest e43a20d5ad28ce2f0ff21f387591d4ba8e3e71fa778e69e1342a56a6bf4e7141 pinned b5928ef. Script scripts/awg_official_interop.py is current reviewed-by-author AST-checked, external official process UAPI, not actual .conf parser. No production users/data touched.

Remaining: actual vpnctl renderer-generated file import & dual-stack path/private/public-node management isolation, byte mutation tests, UAPI/log-secret regression, fuzz/root full regression/platform CI/new release/pin/deploy. Historical ownership sections below superseded: all device workers stopped/frozen, no ongoing edits.

## Reference

Official amnezia-vpn/amneziawg-go tag v3.1.20260828 resolves to b5928efb6ca19f0153958460c3d141f04abc5c2e (lightweight tag, no signature verification claimed). Reference cloned /tmp/awg31-official-research. Narrow device fix assigned; do not replace whole upstream device because custom allocator, queue barriers and sing-box injection paths must survive.

## Executed baseline

linux-worker exact Git checkout /home/tester/awg31-validation at db2db8a. Task-local checksum-verified Go1.27.1 /home/tester/.local/opt/awg31-go1.27.1/go/bin/go. Existing nested module `go test -count=1 -timeout=120s ./device` PASS (0.107s), parent managed bash-48 exit0. These preexisting tests do NOT prove AWG3; new frozen regression tests need same baseline run before repaired revision.

Direct worker DNS for go.dev failed; no system network reconfiguration. Documented ai-egress HTTP proxy bounded IP echo succeeds. Public source/dependency fetch commands use ephemeral per-command proxy only. Toolchain SHA256 verified on control-plane and worker: 63d339f0da5ab53635a56f2490a7984dfe12dfcff22ad749f63edaf590168445, official go.dev JSON response. Worker 65G free before builds.

## In-progress source/test owners

- Device implementation + awg31_wire_test.go: worker 54f87677-c249-4f09-97e5-76ccc88b4cb2; scoped wire repair, HP prefix validation/capacity/keepalive handling as required.
- Independent device_test real transfer table: worker 192553a2-3fca-4f1d-9372-c0f8c2278d04, only device/awg31_transfer_test.go. No implementation source consultation.
- Parent transport/wireguard/endpoint.go removed secret-bearing IPC dump and nested error from IpcSet failure return. Must verify actual error/log confidentiality before acceptance.
- Parent .github/workflows/ci.yml now explicitly runs nested device tests and race tests, plus root transport integration tests; removed unsupported rekey_after_time from smoke config. Not run CI yet.

## Independent regression baseline (bash-51)

Frozen device/awg31_transfer_test.go checksum c71b2f76dede4fb6b7139ec7961c2e2526b3aee4a256e8e85b613e7a540b97e5 copied identically to released baseline. Compilation and -list passed. Execution exit1 after 62.052s: AWG2/TUN passes; AWG2/InputPacket times out (fixture/API versus actual allocation bug under investigation, do not claim full AWG2 matrix green); HP, CPA1, trailers and combined TUN timeout, correct HP key control timeout; wrong-key negative observations pass but are not sufficient alone. Test writer investigating InputPacket baseline while implementation worker repairs known wire differences. No raw synthetic secrets in emitted diagnostics.

## Strict runtime fixture and official reference (round 5)

Parent scripts/awg_runtime_probe.py replaces exploratory fixture with mandatory two config checks, owned process cleanup, exact HTTP payload, no raw diagnostics/keys, and aggregate nonzero failures. AST syntax passes. bash-55 in fresh worker net namespace against released binary SHA256 973e453dc835ec07b53e950c97eb956fedb436434619e178c5dace0568cde0f6: wireguard and awg2 PASS; hp/padding/trailers check0 but curl28, processes alive; aggregate exit1 as intended. New wrong-key/combined cases exist but not yet tested on fixed binary.

Test-writer baseline source investigation says InputPacket fixture correctly follows API. Baseline allocation excludes S4 and copy shifts before extending destination length, truncating AEAD tag for IP47/49 packets. Preserve vectors; do not force alignment to hide defect. Dependency allocator semantics remain to verify.

bash-57 built official amnezia-vpn/amneziawg-go at exact b5928efb6ca19f0153958460c3d141f04abc5c2e on worker /home/tester/awg31-official-b5928e/awg31-reference, exit0. Banner says 0.0.20250522 despite pinned 3.1 tag: use commit/build provenance, not banner claim. Disposable net namespace native TUN create/link succeeds. Independent script for official bidirectional TCP/UDP interop in progress (worker 16ac84bc-aa0c-4c0a-ae77-f4ed364af409), no interop verdict yet.

## Tests-first wire baseline (round 6)

Parent bash-61 applied saved /tmp/awg31-tests-first.patch to exact released db2db8a worker checkout (only tests added). Compilation succeeded; wire length assertions fail; UAPI accepts HP without nonce-sized prefixes; real loopback fixture then reproduces runtime panic at baseline send.go:882 slicing [:132] with cap128. Exit1 as expected. This confirms an actual preexisting outbound buffer failure, not solely a test timeout hypothesis. No fixed-source success claimed yet. Device author preparing frozen narrow repair for independent review.

Official reference build digest e43a20d5ad28ce2f0ff21f387591d4ba8e3e71fa778e69e1342a56a6bf4e7141; go version -m confirms v3.1.20260828 module, vcs.revision=b5928efb6ca19f0153958460c3d141f04abc5c2e, vcs.modified=false, despite stale --version banner.

## Round 7 review / official baseline

Parent bash-66 official-process AWG2 control PASS BOTH roles: sing-box client -> official server and official client -> sing-box server; exact HTTP and UDP bytes, native handshake true, daemons alive, aggregate exit0. Baseline sing SHA973e453d..., official digest e43a20d5...; no repaired AWG3 claim.

Independent core diff review d9b90bfd against frozen SHA7d7e0ca8bac9ee638f6fb024e7be3493ded532514595f32fd329e8f1e97e97f0: no critical, important mixed-generation H/S/HP handshake framing under concurrent UAPI; minor RX window omits AEAD tag, negative crypto tests skip prepare path. Assigned corrective worker67d9a78a (send/receive/noise-protocol/uapi/wire unit tests) and will review final delta. Parent gofmt extracted from verified Go archive formatted snapshot; do not install SDK on control-plane. Original worker's queued duplicate-fixture cleanup changed ONLY awg31_wire_test.go afterward; correction worker told reread and own file exclusively. Historical /tmp/awg31-device-repair.patch test section now stale. Original author interrupted; use current source after corrective final handoff.

## Required next gates

1. Freeze new tests and reproduce baseline failures at exact baseline commit without mutating shared source checkout.
2. Independent diff review before task commit; commit enables immutable worker checkout; build/test fixes there with recorded SHA.
3. Full positive/negative IPv4/IPv6 data transfer (all AWG features separately/together), malformed and wrong-key cases, race tests; upstream native-client interoperability in both directions.
4. Harden runtime fixture: aggregate failures nonzero, no raw key-bearing logs, isolate network; local self-to-self alone insufficient.
5. Review/security, root tests and production-tag build, CI, verified versioned release before vpnctl pin and any is-new deployment.
