#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
DIST_DIR=${HITVID_DIST_DIR:-"$ROOT_DIR/dist"}
VERSION=${HITVID_VERSION:-0.0.0+git.${GITHUB_SHA:-local}}
ARCH=${HITVID_ARCH:-$(dpkg --print-architecture)}

case "$ARCH" in
    amd64) GOARCH=amd64 ;;
    arm64) GOARCH=arm64 ;;
    armhf) GOARCH=arm ;;
    i386) GOARCH=386 ;;
    ppc64el) GOARCH=ppc64le ;;
    s390x) GOARCH=s390x ;;
    riscv64) GOARCH=riscv64 ;;
    *)
        echo "unsupported Debian architecture: $ARCH" >&2
        exit 1
        ;;
esac

if ! dpkg --compare-versions "$VERSION" ge 0; then
    echo "invalid Debian package version: $VERSION" >&2
    exit 1
fi

command -v go >/dev/null || { echo "required tool missing: go" >&2; exit 1; }
command -v dpkg-deb >/dev/null || { echo "required tool missing: dpkg-deb" >&2; exit 1; }

build_root=$(mktemp -d "${TMPDIR:-/tmp}/hitvid-deb.XXXXXX")
trap 'rm -rf "$build_root"' EXIT
package_root="$build_root/package"
mkdir -p "$package_root/DEBIAN" "$package_root/usr/bin" "$package_root/usr/share/doc/hitvid"

echo "Running Go tests"
(
    cd "$ROOT_DIR"
    if [[ "$GOARCH" == "$(go env GOARCH)" ]]; then
        CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH" go test ./...
    else
        echo "Skipping tests for cross-compiled architecture: $ARCH"
    fi
    CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH" go build \
        -trimpath -ldflags='-s -w' -o "$package_root/usr/bin/hitvid" .
)

install -m 0644 "$ROOT_DIR/README.md" "$package_root/usr/share/doc/hitvid/README.md"
install -m 0644 "$ROOT_DIR/LICENSE" "$package_root/usr/share/doc/hitvid/LICENSE"
cat > "$package_root/DEBIAN/control" <<EOF
Package: hitvid
Version: $VERSION
Section: video
Priority: optional
Architecture: $ARCH
Maintainer: Hitmux <support@hitmux.org>
Depends: ffmpeg, chafa
Homepage: https://hitmux.org
Description: high-performance terminal video player
 hitvid plays video files directly in a terminal using FFmpeg and Chafa.
 .
 This package contains the compatibility backend and requires the ffmpeg and
 chafa command line programs at runtime.
EOF

mkdir -p "$DIST_DIR"
output="$DIST_DIR/hitvid_${VERSION}_${ARCH}.deb"
dpkg-deb --build --root-owner-group "$package_root" "$output" >/dev/null
echo "Built $output"
