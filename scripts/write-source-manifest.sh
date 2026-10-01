#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
DIST_DIR=${HITVID_DIST_DIR:-"$ROOT_DIR/dist"}
SRC_DIR=${BUILD_DIR:-"$ROOT_DIR/native/build"}/src
mkdir -p "$DIST_DIR"

{
    echo "hitvid source manifest"
    echo "repository: ${GITHUB_REPOSITORY:-local}"
    echo "revision: ${GITHUB_SHA:-$(git -C "$ROOT_DIR" rev-parse HEAD 2>/dev/null || echo unknown)}"
    echo
    for archive in "$SRC_DIR"/*.tar.xz; do
        [[ -f "$archive" ]] || continue
        sha256sum "$archive"
    done
} > "$DIST_DIR/SOURCE-MANIFEST.txt"
