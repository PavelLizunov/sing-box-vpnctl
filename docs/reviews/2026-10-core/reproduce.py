#!/usr/bin/env python3
"""Reproduce review fixtures without changing tracked engine sources."""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import tempfile

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
REVIEWED = "9b6b9bbf3c05d17afd693e5e66424f60d728ce6c"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go", default="go", help="Go executable (1.26.8 used in the review)")
    parser.add_argument("--mode", choices=("baseline", "trim", "unwrap", "before-update"), default="baseline")
    args = parser.parse_args()
    go = shutil.which(args.go)
    if not go:
        parser.error("Go executable not found; install Go or supply --go")
    revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    print(f"Checkout: {revision}; original review: {REVIEWED}", flush=True)
    manifest = json.loads((HERE / "SHA256.json").read_text())
    for name, expected in manifest.items():
        actual = hashlib.sha256((HERE / name).read_bytes()).hexdigest()
        if actual != expected:
            parser.error(f"Evidence hash mismatch: {name}")
    replace = {
        str(ROOT / "submodules/wireguard-go/device/endpoint_resolver_test.go"):
            str(HERE / "review_awg_handshake_test.go.txt")
    }
    run = "^TestReviewAWG(Basic|RandomTrailers|HeaderProtection)$"
    if args.mode in ("trim", "unwrap"):
        fixture = "review_receive_trim.go.txt" if args.mode == "trim" else "review_receive_unwrap.go.txt"
        replace[str(ROOT / "submodules/wireguard-go/device/receive.go")] = str(HERE / fixture)
        run = "^TestReviewAWG" + ("RandomTrailers" if args.mode == "trim" else "HeaderProtection") + "$"
    elif args.mode == "before-update":
        for name in ("send.go", "receive.go"):
            replace[str(ROOT / "submodules/wireguard-go/device" / name)] = str(HERE / ("before-update-" + name + ".txt"))
    with tempfile.TemporaryDirectory(prefix="awg-review-") as temporary:
        overlay = Path(temporary) / "overlay.json"
        overlay.write_text(json.dumps({"Replace": replace}))
        command = [go, "test", "-count=3", "-overlay", str(overlay), "-run", run, "-v", "github.com/sagernet/wireguard-go/device"]
        result = subprocess.run(command, cwd=ROOT)
    print("Expected for the reviewed code: baseline/before-update fail only the optional AWG profiles; trim/unwrap pass.")
    print("Diagnostic overlays are not production-ready fixes.")
    return result.returncode


if __name__ == "__main__":
    raise SystemExit(main())
