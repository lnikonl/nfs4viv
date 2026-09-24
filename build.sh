#!/usr/bin/env bash
# Builds nfs4viv for Linux and Windows and packs release archives that bundle
# the README and the license. Fallback for systems without make.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

BIN_DIR="bin"
DIST_DIR="dist"
VERSION="${VERSION:-dev}"
LDFLAGS="-X gitlab.babun.tv/nfs4/nfs4-tools/nfs4viv/appinfo.Version=$VERSION"

rm -rf "$BIN_DIR" "$DIST_DIR"
mkdir -p "$BIN_DIR" "$DIST_DIR"

echo "building nfs4viv (linux/amd64)"
go build -ldflags "$LDFLAGS" -o "$BIN_DIR/nfs4viv" ./cmd/nfs4viv

echo "building nfs4viv.exe (windows/amd64)"
GOOS=windows GOARCH=amd64 go build -ldflags "$LDFLAGS" -o "$BIN_DIR/nfs4viv.exe" ./cmd/nfs4viv

stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT

for target in linux windows; do
	binary="nfs4viv"
	extension="tar.gz"
	if [ "$target" = windows ]; then
		binary="nfs4viv.exe"
		extension="zip"
	fi
	mkdir -p "$stage/$target"
	cp "$BIN_DIR/$binary" README.md LICENSE "$stage/$target/"
	archive="$DIST_DIR/nfs4viv-$VERSION-$target-amd64.$extension"
	echo "packing $(basename "$archive")"
	go run ./cmd/packdist "$archive" "$stage/$target/$binary" "$stage/$target/README.md" "$stage/$target/LICENSE"
done

echo "done: $BIN_DIR/ and $DIST_DIR/"
