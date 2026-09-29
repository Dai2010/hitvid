#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
DIST_DIR=${HITVID_DIST_DIR:-"$ROOT_DIR/dist"}
OUTPUT=${HITVID_OUTPUT:-"$DIST_DIR/hitvid-linux-amd64"}

if [[ "$(uname -s)" != "Linux" ]]; then
    echo "standalone release build currently supports Linux only" >&2
    exit 1
fi
case "$(uname -m)" in
    x86_64|amd64) ;;
    *)
        echo "standalone release build currently supports amd64 only" >&2
        exit 1
        ;;
esac

for tool in go cc file readelf; do
    command -v "$tool" >/dev/null || {
        echo "required standalone build tool missing: $tool" >&2
        exit 1
    }
done

bash "$ROOT_DIR/scripts/build-native.sh"
mkdir -p "$(dirname "$OUTPUT")"

(
    cd "$ROOT_DIR"
    CGO_ENABLED=1 go test -tags native ./...
    CGO_ENABLED=1 go build         -tags native         -trimpath         -ldflags='-s -w -linkmode external -extldflags "-static"'         -o "$OUTPUT" .
)

chmod 0755 "$OUTPUT"
bash "$ROOT_DIR/scripts/verify-standalone.sh" "$OUTPUT"
echo "Built standalone hitvid: $OUTPUT"
