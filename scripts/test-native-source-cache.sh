#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

export BUILD_DIR="$TMP_DIR/build"
export HITVID_BUILD_NATIVE_LIBS=0
# shellcheck source=build-native.sh
source "$ROOT_DIR/scripts/build-native.sh"

mkdir -p "$SRC_DIR" "$TMP_DIR/archive/cache-probe"
printf 'trusted\n' > "$TMP_DIR/archive/cache-probe/payload.txt"
tar -cJf "$SRC_DIR/cache-probe.tar.xz" -C "$TMP_DIR/archive" cache-probe
expected=$(sha256sum "$SRC_DIR/cache-probe.tar.xz" | awk '{print $1}')

mkdir -p "$SRC_DIR/cache-probe"
printf 'tampered\n' > "$SRC_DIR/cache-probe/payload.txt"

source_dir=$(fetch_source "cache-probe" "https://invalid.example/cache-probe.tar.xz" "$expected")
grep -qx 'trusted' "$source_dir/payload.txt"

mkdir -p "$PREFIX/lib"
printf 'tampered\n' > "$PREFIX/lib/libavcodec.a"
prepare_prefix
test ! -e "$PREFIX/lib/libavcodec.a"

echo "Native source cache re-extraction and prefix reset checks passed"
