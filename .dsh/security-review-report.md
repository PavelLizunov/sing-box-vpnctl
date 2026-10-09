# Differential Security Review

**Repository:** `/var/lib/dsh/Project/sing-box-awg31`
**Range:** `db2db8a6..d1862694` (2 commits)
**HEAD:** `d186269410bec6dd7e2d2cf8a06c09f9cddf1605`
**Commits:**
- `d1862694` fix(wireguard): redact rejected UAPI values from logs
- `cae50a53` fix(awg): align v3 framing with official wire semantics

**Reviewed files (in scope):**
- `submodules/wireguard-go/device/uapi.go` (+72 lines changed)
- `transport/wireguard/endpoint.go` (+1/−1 line changed)
- `transport/wireguard/awg_secret_redaction_test.go` (new, 191 lines)

**Adjacent files consulted (read-only context):**
- `transport/wireguard/device_awg.go` (awgIpcLines, pre-existing validation)
- `submodules/wireguard-go/device/magic-header.go` (magicHeader parsing)
- `submodules/wireguard-go/device/send.go` (awgPacketSize)
- `submodules/wireguard-go/device/noise-protocol.go` (message size constants)
- `submodules/wireguard-go/device/logger.go` (Logger struct)

**Reviewer:** Automated differential security review
**Date:** 2025-07-17

---

## Executive Summary

This range fixes a **secret-leakage vulnerability** where WireGuard private keys, pre-shared keys, and header-protection keys could appear in logs and error messages on configuration rejection. It also adds **staged UAPI validation** for framing parameters (paddings/headers/header-protection key), preventing partial application of inconsistent configurations and adding new bounds checks. A comprehensive regression test (`awg_secret_redaction_test.go`) covers the redaction invariants.

The changes are **security-positive** overall. One residual observation and one pre-existing note are recorded below.

---

## Findings

### F-1: Secret leakage in endpoint.go error path (FIXED)

| Field | Value |
|-------|-------|
| **Severity** | HIGH (was a confirmed vulnerability, now fixed) |
| **Status** | Confirmed Fixed |
| **File** | `transport/wireguard/endpoint.go:222` |

**Before (baseline `db2db8a6`):**
```go
// endpoint.go:222 (old)
return E.Cause(err, "setup wireguard: \n", ipcConf.String())
```
The full IPC configuration string — containing `private_key=<hex>`, `preshared_key=<hex>`, and `header_protection_key=<hex>` — was included verbatim in the returned error. This error propagates up the sing-box call stack to logging and potentially to API/UI callers.

**After (head `d1862694`):**
```go
// endpoint.go:222 (new)
return E.New("setup wireguard: rejected device configuration")
```
The error is now a static string. No secret material is included.

**Evidence:** `git blame` confirms the old line was introduced in `5e7fd7ad7` ("Fix lint errors") with the `ipcConf.String()` call, and was present in all subsequent commits until this fix.

**Prerequisites for exploitation (pre-fix):** An attacker needed access to log output (log files, monitoring systems, error APIs) of a sing-box instance where WireGuard configuration was rejected at runtime. The secrets were the device's own private key and all peer PSKs — full compromise of all tunnel sessions.

---

### F-2: Secret leakage in UAPI deferred error logger (FIXED)

| Field | Value |
|-------|-------|
| **Severity** | HIGH (was a confirmed vulnerability, now fixed) |
| **Status** | Confirmed Fixed |
| **File** | `submodules/wireguard-go/device/uapi.go:211-216` |

**Before:**
```go
device.log.Errorf("%v", err)
```
The deferred function logged the full error, which could contain parser errors echoing rejected values (e.g., `"failed to parse line \"private_key=<hex>\""` from line 241, or `"failed to set private_key: <wrapped error>"` from line 289).

**After:**
```go
device.log.Errorf("UAPI configuration rejected")
```
Static message only. The underlying `err` is still returned to the caller but no longer logged within the device.

**Note on error format strings:** Several `ipcErrorf` calls still embed user-supplied values in their error text (e.g., line 241 `"failed to parse line %q"`, line 504 `"invalid UAPI device key: %v"`). These are safe because:
1. The deferred logger no longer logs the error text
2. The caller (`endpoint.go:222`) now returns a static error, discarding the wrapped IPC error

---

### F-3: Residual secret echo in IpcHandle error logging (INFORMATIONAL)

| Field | Value |
|-------|-------|
| **Severity** | LOW |
| **Status** | Reasoned Hypothesis (not exploitable in current integration) |
| **File** | `submodules/wireguard-go/device/uapi.go:721` |

```go
// uapi.go:721
device.log.Errorf("%v", status)
```

The `IpcHandle` method (UAPI Unix socket protocol handler) still logs the full `IPCError` including its wrapped error text via `Errorf("%v", status)`. The `IPCError.Error()` method formats as `"IPC error %d: %v"` with the wrapped error, which could contain parser errors that echo rejected values.

**Mitigating factor:** `IpcHandle` is **not called anywhere** in the sing-box codebase. The only call path from sing-box is `endpoint.go` → `wgDevice.IpcSet()` → `IpcSetOperation()`, which bypasses `IpcHandle` entirely. `IpcHandle` exists for upstream wireguard-go's UAPI socket protocol, which requires local Unix socket access (root-equivalent privilege).

**Prerequisites:** An attacker would need (1) `IpcHandle` to be connected to a socket (currently impossible in sing-box), (2) the ability to send malformed UAPI commands through that socket, and (3) access to device logs. Even if `IpcHandle` were used, the UAPI socket itself requires local administrator access.

**Recommendation:** If `IpcHandle` is ever activated, apply the same static-message pattern from `IpcSetOperation`.

---

### F-4: Staged configuration validation (NEW — security improvement)

| Field | Value |
|-------|-------|
| **Severity** | N/A (positive finding) |
| **Status** | Confirmed Improvement |
| **File** | `submodules/wireguard-go/device/uapi.go:219-223, 244-251, 730-802` |

**Change:** Padding values (`s1`-`s4`), headers (`h1`-`h4`), and `header_protection_key` are now staged in an `ipcSetDevice` struct during parsing and applied atomically via `mergeWithDevice()`.

**New validation in `mergeWithDevice` (lines 746-802):**
1. **Packet size bounds** (line 752-755): Each `S<n>` prefix is validated against `awgPacketSize(prefix, coreSize, 0)` to prevent oversized packets that could exceed UDP payload limits
2. **Header-protection minimum** (line 756-758): When header protection is enabled, each prefix must be ≥ `HeaderCipherNonceSize` (12 bytes) to ensure the nonce space is available
3. **Full-width header range** (line 778): Rejects `uint64(end)-uint64(start)+1 == 1<<32` (the full uint32 range), which would make message type discrimination impossible
4. **Nil header check** (line 778, 782-783): Prevents nil pointer dereference during overlap checking
5. **Header overlap** (line 788-789): Pre-existing check, now better protected against nil pointers

**Early merge at `public_key` transition** (lines 244-251): When transitioning from device config to peer config, `mergeWithDevice` is called first, ensuring framing state is installed before `handlePostConfig()` can start peer traffic (addressing a TOCTOU race with `peer.Start()`).

**Scanner error ordering** (lines 272-274): `scanner.Err()` is now checked before `mergeWithDevice`, preventing application of partially-read configuration on I/O errors.

---

### F-5: Pre-existing partial atomicity of device state writes (INFORMATIONAL)

| Field | Value |
|-------|-------|
| **Severity** | LOW |
| **Status** | Reasoned Hypothesis (pre-existing, not introduced by this diff) |
| **File** | `submodules/wireguard-go/device/uapi.go:338-501` |

The following device fields are still written directly during UAPI parsing, before `mergeWithDevice` validation:
- `device.junk.{count,min,max}` (lines 338, 349, 360)
- `device.ipackets[0..4]` (lines 439-467)
- `device.randomTrailers` (line 475)
- `device.disableCookies` (line 483)
- `device.contentPaddingAddition` (line 501)

If UAPI parsing fails after these writes (e.g., during `mergeWithDevice`), these values remain applied while the staged paddings/headers/headerProtectionKey are rolled back (never applied). This is a pre-existing atomicity gap, not introduced by this diff.

**Impact:** In the sing-box integration, `IpcSet` is called once during `Start()`. A failure results in `wgDevice.Close()` (endpoint.go:221), so the partially-modified device is immediately destroyed. No real-world impact exists in the current call path. The atomicity gap would only matter if `IpcSet` were called for live reconfiguration (not currently done in sing-box).

---

### F-6: Test coverage assessment

| Field | Value |
|-------|-------|
| **Severity** | N/A (informational) |
| **Status** | Positive |
| **File** | `transport/wireguard/awg_secret_redaction_test.go` |

**Test design strengths:**
1. Uses synthetic, distinct byte patterns (`0xa5`, `0xb6`, `0xc7`, `0xd8`) that are unambiguously identifiable
2. Checks all three secret types: private key, PSK, header-protection key
3. Checks four encodings per secret: base64, raw base64, hex lowercase, hex uppercase
4. Verifies three output surfaces: returned error, debug logs, error logs
5. Verifies the full IPC blob is not present in any output
6. Two test cases: oversized prefix (fails `awgPacketSize`) and malformed padding (fails `content_padding_addition` parsing)
7. Uses a no-network dialer to prevent real socket operations
8. Build-gated with `with_awg && with_gvisor` tags

**Test limitation:** The test only covers the `endpoint.go` → `IpcSet` path. The `IpcHandle` path (F-3) is not tested, but as noted, it's not called in the integration.

---

## Pre-Delivery Quality Checklist

- [x] Exact diff/commit range confirmed: `db2db8a6..d1862694` (2 commits)
- [x] History checked: `git blame` traced old leaking lines to `5e7fd7ad7` and `320d8d929`
- [x] `git log -S` confirmed `ipcConf.String()` introduced in `5e7fd7ad` and removed in `cae50a53`
- [x] Callers mapped: `endpoint.go:219` → `IpcSet` → `IpcSetOperation` (only live path); `IpcHandle` is dead code
- [x] Logging checked for secret leaks: deferred func now static; endpoint error now static; `IpcHandle` residual noted
- [x] Attack scenarios specify prerequisites: log access required; `IpcHandle` requires local socket
- [x] No live network, external services, or production credentials used
- [x] Review report written to `.dsh/security-review-report.md` (this file)

## Coverage Limits

1. **Unexamined files:** `submodules/wireguard-go/device/awg31_wire_test.go` (435 lines, new) and `submodules/wireguard-go/device/awg31_transfer_test.go` (294 lines, new) were not reviewed in depth — they test wire framing correctness, not security boundaries
2. **Out-of-scope files in range:** `submodules/wireguard-go/device/{device,noise-protocol,peer,receive,send}.go`, `.github/workflows/ci.yml`, `SPECS/awg31-wire-interop.md` — wire protocol changes are outside the security review scope
3. **Build-gated test:** `awg_secret_redaction_test.go` requires `with_awg && with_gvisor` build tags — cannot be executed in this environment without the full build toolchain and dependencies
4. **`IpcGetOperation` path:** Outputs private keys and PSKs to callers via `IpcGet()` — this is the standard WireGuard UAPI behavior (read the current config), not a bug, but `IpcGet` is not called from sing-box transport
5. **Memory residency:** The `e.ipcConf` field (with private key in hex) persists in `Endpoint` struct memory until `Close()`. This is inherent to the WireGuard userspace design; the private key must be in process memory during operation
