# Architecture

sing-box-vpnctl is upstream [sing-box](https://github.com/SagerNet/sing-box) plus a layer of its own. The upstream part is taken as it is and replaced with every core update; the layer is the only thing this repository maintains.

## The boundary

- `release/FORK_UPSTREAM_BASE` names the upstream commit the tree is based on.
- `release/FORK_OWNED` lists every path that may differ from that commit, in three groups:
  - **owned**: files the fork added;
  - **derived**: the embedded WireGuard engine in `submodules/wireguard-go`, a copy of SagerNet wireguard-go with the AmneziaWG device logic. It takes bug fixes only, so that the next port can be compared with its upstream line by line;
  - **hooks**: upstream files that carry a few fork lines, each with the largest size it may have.
- `release/fork_boundary_test.py` fails CI when any other file differs from upstream or a hook grows. A fix that seems to belong in an upstream file goes into an owned file and is called through a hook.

## Where the layer lives

AmneziaWG:

- `option/wireguard_awg.go`: the options and their JSON names.
- `transport/wireguard/device_awg.go`: validation and translation of the options into device configuration lines. Limits that protect the device (junk sizes, key format, prefix sizes with header protection, rekey range) are enforced here.
- `transport/wireguard/*_awg.go`: signature packets and protocol masquerade (QUIC Initial, DNS, SIP, STUN).
- `submodules/wireguard-go/device`: the wire format (prefixes, magic headers, header protection, trailers, content padding). It follows the official amneziawg-go wire format; `go.mod` replaces the wireguard-go module with this directory.

XHTTP:

- `option/v2ray_xhttp.go`: the options.
- `transport/v2rayxhttp`: the client (three modes, connection pool with limits and a failure breaker, deadlines).
- `transport/v2ray/registry.go` and `include/v2rayxhttp.go`: registration behind the `with_xhttp` build tag.

Hooks in upstream files are one or a few lines each: the option structs embed the fork options, the endpoint passes them on, the transport switch asks the registry, the Clash API adds `user`, the build tag lists gain `with_awg`, `with_xhttp` and `with_v2ray_api`.

## Rules for the fork's own code

- Go files of the layer carry no comments, except a single line above a statement that starts with `Invariant:`, `Race:`, `Quirk:` or `Protocol:` and states a reason the code cannot show. CI rejects other new comments (`release/hygiene/`).
- Dated plans, reviews and logs are not kept in the tree.

## Checks

- `ci.yml`, on every pull request: the fork boundary, the comment rule, tests of the embedded device and bind (also with the race detector), AWG transport tests, security regressions, a build with the release tags and a configuration check.
- `verify.yml`, on demand or by pushing a `verify/*` branch: the whole upstream test suite, the embedded tests on macOS and Windows, cross-builds for the six release targets, the Android library, a vulnerability report.
- `release.yml`, on a tag: builds the exact tagged commit, publishes checksums and build attestations, never replaces an existing release.

## Taking a new upstream version

1. Merge the upstream tag into a branch cut from current `main`, and update `release/FORK_UPSTREAM_BASE` in the same change.
2. Port the changes of SagerNet wireguard-go between the old and the new version into `submodules/wireguard-go`, as separate commits.
3. Run `verify.yml` on the branch. The boundary test shows every upstream file that still differs.
4. Check on a test setup that AmneziaWG and XHTTP still pass traffic before tagging a release.

## History

Dated reviews, task specs and progress logs stay readable in git at the last commit that held them:

```bash
git ls-tree -r --name-only 4a5352eb470c92aa8569ceeac4b8d674dfcc980e docs/reviews docs/specs SPECS
git show 4a5352eb470c92aa8569ceeac4b8d674dfcc980e:docs/reviews/2026-10-core/core-review.md
git show 4a5352eb470c92aa8569ceeac4b8d674dfcc980e:ROADMAP.md
```
