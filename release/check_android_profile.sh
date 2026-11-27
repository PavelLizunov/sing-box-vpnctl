#!/usr/bin/env bash
set -euo pipefail

test "$#" -gt 0
binary=$(mktemp)
trap 'rm -f -- "$binary"' EXIT

for aar in "$@"; do
    unzip -tq "$aar"
    entries=$(unzip -Z1 "$aar")
    if grep -qi cronet <<< "$entries"; then
        echo "Cronet was packaged in $aar" >&2
        exit 1
    fi
    for abi in arm64-v8a armeabi-v7a x86 x86_64; do
        unzip -p "$aar" "jni/$abi/libbox.so" > "$binary"
        build_info=$(go version -m "$binary")
        tags=$(sed -n 's/^[[:space:]]*build[[:space:]]*-tags=//p' <<< "$build_info")
        for feature in with_turnrelay with_trusttunnel with_mieru; do
            if ! grep -qw "$feature" <<< "$tags"; then
                echo "$aar ($abi) is missing $feature" >&2
                exit 1
            fi
        done
        if grep -Eq 'cronet-go|with_naive_outbound|with_purego' <<< "$build_info"; then
            echo "Cronet was linked in $aar ($abi)" >&2
            exit 1
        fi
    done
done
