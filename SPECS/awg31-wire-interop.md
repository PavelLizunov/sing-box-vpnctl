# Spec: AWG3 wire interoperability repair

## 1. Intent & Invariants
- Repair header protection, content padding and random trailers in sing-box-vpnctl's vendored AWG3.1 device against official Amnezia wire semantics.
- Preserve AWG2, VLESS, Hysteria2 and XHTTP compatibility; do not label downgraded AWG2 as AWG3.
- No production install until independent review, real transfer tests and CI pass. Scope approved explicitly in the vpnctl operational session.

## 2. Interface / Data Contract
- Preserve existing AWG3 JSON configuration fields; align encryption/decryption, packet lengths and padding with official implementation.
- Verify client and server agree on header protection, transport padding and handshake trailers; reject malformed/unauthenticated traffic.
- Publish a separately verified kernel release, then finish vpnctl integration in its own worktree.

## 3. Verification Checklist
- [ ] Tests reproduce current header-protection, padding and trailer failures before repair.
- [ ] AWG2 and AWG3 complete real data transfers; invalid keys are rejected.
- [ ] AWG3 features pass separately and together, with official implementation interoperability.
- [ ] Independent review, security review and CI pass; prior protocols regressions remain green.
- [ ] Back up production before scoped is-new update and verify five protocols plus SSH afterwards.
