#!/bin/sh
set -eu
for target in darwin/arm64 windows/amd64 linux/amd64; do
  os=${target%/*}
  arch=${target#*/}
  output="build/$os-$arch/inferyard-studio"
  if [ "$os" = windows ]; then output="$output.exe"; fi
  mkdir -p "build/$os-$arch"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -tags release -o "$output" .
  printf '%s: go build exit=0\n' "$target"
done
python3 scripts/package.py
