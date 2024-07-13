#!/bin/bash
#
# $1 - The version number to use for the build. If not provided, the current git
#     commit hash will be used.
#

set -euo pipefail

app="$(basename "$PWD")"

version="${1:-dev}"

env VERSION="$version" go generate -x ./...

rm -rf bin
mkdir -p bin
pushd bin
trap popd EXIT

gobuild() {
  mkdir -p "$1/$2"
  pushd "$1/$2"
  env GOOS="$1" GOARCH="$2" go build ../../..
  case "$1" in
    "windows")
      zip ../../"${app}_${1}_${2}_${version}".zip ./*.exe
      ;;
    "linux")
      tar -c --zstd --numeric-owner \
        -f ../../"${app}_${1}_${2}_${version}".tar.zst .
      ;;
    *)
      tar -c --numeric-owner \
        -f ../../"${app}_${1}_${2}_${version}".tar.bz2 .
      ;;
  esac
  rm ./*
  popd
  rmdir "${1:?}/${2:?}" "${1:?}"
}

export CGO_ENABLED=0

# Binaries for all platforms
gobuild linux amd64
gobuild linux arm64
gobuild darwin amd64
gobuild darwin arm64
gobuild windows amd64
gobuild windows arm64

sha1sum ./*.tar.* ./*.zip >sha1sums.txt
