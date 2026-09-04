#!/usr/bin/env python3
import subprocess
import re
import sys
import os

def run_bench():
    # Bundle current repo and run benchmark on mac-worker
    os.system("git bundle create /tmp/sing-box-bench.bundle HEAD >/dev/null 2>&1")
    os.system("scp -q -o BatchMode=yes /tmp/sing-box-bench.bundle mac-worker:/tmp/sing-box-bench.bundle")
    
    cmd = """
    set -euo pipefail
    export PATH=/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin
    WORK=/tmp/perf-bench
    rm -rf "$WORK"
    git clone -q /tmp/sing-box-bench.bundle "$WORK"
    cd "$WORK"

    # 1. Quality gate
    go test -tags with_xhttp ./transport/v2rayxhttp/... -count=1 >/dev/null
    go test -tags with_awg ./transport/wireguard/... -count=1 >/dev/null

    # 2. Benchmarks
    B1=$(go test -tags with_xhttp -bench='BenchmarkApplyXPaddingLegacy|BenchmarkApplyXPaddingHeader|BenchmarkGenerateTokenishPadding' -benchmem -run=^$ ./transport/v2rayxhttp/ -count=1)
    B2=$(go test -tags with_awg -bench='BenchmarkAwgIpcLinesAWG2|BenchmarkAwgIpcLinesAWG31|BenchmarkMasqueCPSQUIC' -benchmem -run=^$ ./transport/wireguard/ -count=1)
    B3=$(cd submodules/wireguard-go && go test -bench='BenchmarkDeterminePacketTypeAndPadding|BenchmarkHeaderProtectionCipher|BenchmarkRandomTrailer' -benchmem -run=^$ ./device/ -count=1)

    echo "$B1"
    echo "$B2"
    echo "$B3"
    rm -rf "$WORK" /tmp/sing-box-bench.bundle
    """
    
    res = subprocess.run(["ssh", "-o", "BatchMode=yes", "mac-worker", cmd], capture_output=True, text=True)
    if res.returncode != 0:
        print("BENCHMARK_ERROR:", res.stderr, file=sys.stderr)
        sys.exit(1)
        
    output = res.stdout
    ns_values = []
    bench_results = {}
    for line in output.splitlines():
        # Match e.g.: BenchmarkApplyXPaddingLegacy-10 12618409 94.91 ns/op 272 B/op 3 allocs/op
        m = re.match(r'^(Benchmark\w+)-\d+\s+\d+\s+([\d\.]+)\s+ns/op', line.strip())
        if m:
            name = m.group(1)
            ns = float(m.group(2))
            bench_results[name] = ns
            ns_values.append(ns)
            
    if not ns_values:
        print("NO_BENCHMARKS_PARSED", file=sys.stderr)
        sys.exit(1)

    # Print individual results
    for k, v in sorted(bench_results.items()):
        print(f"{k}: {v:.2f} ns/op", file=sys.stderr)
        
    # Aggregate metric: total ns across all benchmarks
    total_ns = sum(ns_values)
    print(f"{total_ns:.2f}")

if __name__ == "__main__":
    run_bench()
