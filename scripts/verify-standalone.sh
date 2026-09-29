#!/usr/bin/env bash
set -euo pipefail

BIN=${1:-}
if [[ -z "$BIN" || ! -x "$BIN" ]]; then
    echo "usage: $0 /path/to/hitvid" >&2
    exit 2
fi

file_output=$(file "$BIN")
printf '%s\n' "$file_output"
if ! grep -qi "statically linked" <<<"$file_output"; then
    echo "binary is not fully statically linked" >&2
    exit 1
fi

if readelf -d "$BIN" 2>/dev/null | grep -q "(NEEDED)"; then
    echo "binary still contains dynamic shared-library dependencies" >&2
    readelf -d "$BIN" >&2
    exit 1
fi

if command -v ldd >/dev/null 2>&1; then
    ldd_output=$(ldd "$BIN" 2>&1 || true)
    if grep -Eq "=>|ld-linux|linux-vdso" <<<"$ldd_output"; then
        echo "ldd reported runtime shared-library dependencies:" >&2
        printf '%s\n' "$ldd_output" >&2
        exit 1
    fi
fi

tmp_home=$(mktemp -d)
trap 'rm -rf "$tmp_home"' EXIT
env -i     HOME="$tmp_home"     TERM=xterm-256color     PATH=/nonexistent     "$BIN" -help >/dev/null

echo "Standalone verification passed: no runtime shared libraries or PATH tools are required."
