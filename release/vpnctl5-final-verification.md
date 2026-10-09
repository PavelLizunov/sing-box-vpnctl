# v1.14.0-vpnctl.5 — release verification

## Delivered

- Release: https://github.com/PavelLizunov/sing-box-vpnctl/releases/tag/v1.14.0-vpnctl.5
- PR: https://github.com/PavelLizunov/sing-box-vpnctl/pull/2 (merged with exact-head guard).
- Reviewed head: `873ba4c0086f91ed5d674b308f459696de35e1ca`.
- Released merge/tag revision: `8688eab3c51d07ff89a124ce247044409b9f93fd`; merge tree equals reviewed head.
- Baseline: `91068e67ea244f4cdfea991f33cf3dfa2a259511` / v1.14.0-vpnctl.4.
- Published at 2026-09-07T20:22:18Z. No assets overwritten, tags moved, production deployment, service restart, benchmark, or host privilege change.

## Implementation and review

CPS numeric and aggregate packet bounds, AWG raw CR/LF rejection with secret-safe errors, crypto/rand XHTTP IDs with deduplicated alphabets, synchronized stream-up rejection cleanup, and exact-revision SHA-pinned immutable attested release workflow implemented. Regression tests preceded fixes in separate committed RED/GREEN snapshots; details are in [security-vpnctl5-progress.md](security-vpnctl5-progress.md).

Independent source reviewer and parent reviewed the exact diff and corrections. Reviewer-found masquerade normalization bypass, retained/rearmed timers, and later-deadline error masking were reproduced and repaired. No remaining High/Medium source findings were reported; review is scoped, not a universal security guarantee. Skills used: homelab, sdd (preapproved Micro-Spec), ponytail, security-review, change-verification, repository-readme.

## Executed verification

- Worker exact reviewed head: device race, feature-tag option/AWG/XHTTP race checks, production-tag test suite, production build/version.
- **Boundary:** TestUnshareNamespace fails operation-not-permitted under the worker account. The final production-tag suite explicitly skipped only that test; not an unqualified full-suite pass. Required `-ldflags=-checklinkname=0` used in corrected full-tag run.
- Final worker log: `/tmp/sing-box-security-final-verification.log` (155 lines), exact reviewed SHA and `FINAL_EXIT=0`.
- PR CI: https://github.com/PavelLizunov/sing-box-vpnctl/actions/runs/34157723996 — success.
- Main CI: https://github.com/PavelLizunov/sing-box-vpnctl/actions/runs/34158014310 — success.
- Release matrix: https://github.com/PavelLizunov/sing-box-vpnctl/actions/runs/34158234961 — success, all seven build jobs, native attestation and publish.
- Workflow Python assertions 3/3 and task diff whitespace check passed.

## Published assets and provenance

All **23 uploaded assets** downloaded: 11 payloads, 11 individual checksum files, SHA256SUMS. Payloads cover Windows amd64/arm64, Linux amd64/arm64/armv7, macOS universal ZIP/tar.gz, Android current/legacy AAR with versioned and stable-name copies.

All 11 payloads passed both aggregate and individual SHA-256 checks. All23 uploaded files independently passed native GitHub attestation verification with:

```sh
gh attestation verify FILE --repo PavelLizunov/sing-box-vpnctl \
  --source-digest 8688eab3c51d07ff89a124ce247044409b9f93fd \
  --source-ref refs/tags/v1.14.0-vpnctl.5 \
  --signer-workflow PavelLizunov/sing-box-vpnctl/.github/workflows/release.yml \
  --deny-self-hosted-runners --format json
```

Verifier: isolated GitHub CLI2.100.0, upstream archive SHA256 verified before use, no global tool replacement. Certificate identity confirms source digest/ref, release workflow, GitHub-hosted runner, and run34158234961 attempt1. Attestations bind packaged artifacts, not platform code-signing or reproducible-build claims.

Evidence:
- `/tmp/sing-box-security-published-verification.log` —23 VERIFIED lines + FINAL_VERIFICATION_EXIT=0.
- `/tmp/sing-box-vpnctl5-published/checksums.log` —22 checksum passes.
- `/tmp/sing-box-vpnctl5-published/attestations/` —23 verification JSON files plus per-file logs.
- `/tmp/sing-box-vpnctl5-published/assets/SHA256SUMS` — published hashes.
- `/tmp/sing-box-vpnctl5-published/version.txt` — published Linux amd64 version invocation.
- `/tmp/sing-box-security-release-34158234961.log` — complete matrix log.

Published Linux amd64 output: version1.14.0-vpnctl.5, Go1.26.7, revision8688eab3c51d07ff89a124ce247044409b9f93fd, CGO disabled, all release feature tags including with_awg, with_xhttp, with_v2ray_api. Linux amd64 archive SHA256: `5f98eacc95b9ed9c1d53592605d812d9d2cb8c496fa95723cfb7c5f9447fe46e`.

## Limits and workspace safety

Only the downloaded Linux amd64 executable was run, with `version`; Windows/macOS/Android/non-amd64 artifacts were built, hash-checked, and provenance-verified, not executed on each target platform. No new live Xray interoperability or production traffic test. Shared `/var/lib/dsh/Project/sing-box-core` remains untouched with its preexisting untracked `.dsh/`; all edits made in dedicated task worktree. This post-release report is a local evidence artifact and is not part of the already immutable release tag.
