# Builds, releases and verifying a download

## Build tags

Release binaries use the upstream tag set plus three tags of the fork:

- `with_awg`: AmneziaWG options for the WireGuard endpoint.
- `with_xhttp`: the XHTTP client transport.
- `with_v2ray_api`: the V2Ray stats API.

The exact list is `DEFAULT_TAGS` in `.github/workflows/release.yml`. The mobile library uses the list in `cmd/internal/build_libbox/main.go`.

## Building from source

Go 1.26.10 or newer. The fork's CI, verification and release workflows pin 1.26.10 so a cached older 1.26 toolchain cannot reintroduce GO-2026-6809.

```bash
git clone \
  https://github.com/PavelLizunov/sing-box-vpnctl.git
cd sing-box-vpnctl
TAGS="$(grep -oP 'DEFAULT_TAGS: "\K[^"]+' \
  .github/workflows/release.yml)"
go build -trimpath -tags "$TAGS" \
  -ldflags "-s -w -checklinkname=0" \
  -o sing-box ./cmd/sing-box
```

`-checklinkname=0` is required: the tree uses linkname in the same way upstream does.

## What a release contains

- Linux: `tar.gz` archives for amd64, arm64 and armv7.
- Windows: `zip` archives for amd64 and arm64.
- macOS: one universal binary, as `tar.gz` and `zip`.
- Android: `libbox.aar` (Android 7.0, API 24, and newer) and `libbox-legacy.aar` (Android 5.0, API 21, without the naive outbound).
- `SHA256SUMS` and a `.sha256` file next to every asset.

Releases are built by GitHub Actions from the exact tagged commit. The workflow refuses to replace an existing release.

## Verifying a download

Download the asset and `SHA256SUMS` from the same release:

```bash
sha256sum --check --ignore-missing SHA256SUMS
gh attestation verify ./ASSET_FILE \
  --repo PavelLizunov/sing-box-vpnctl
```

The attestation ties the file to the release workflow run. Compare the source commit it reports with the commit of the release tag.

## What is checked before a release

See the Checks section of [ARCHITECTURE.md](../../ARCHITECTURE.md). Verify includes synthetic TCP/UDP interop with official AmneziaWG `v3.1.20260828` in an isolated Linux namespace. This is not a native `.conf` import test. Real Android devices, physical suspend/resume and production reverse-proxy paths still need separate testing. Xray interoperability is a candidate acceptance check, not part of the checked-in Verify matrix.

A build with `CGO_ENABLED=0` and the release `with_purego` tag can still require the platform's dynamic loader and native libraries; do not assume it is a fully static executable.
