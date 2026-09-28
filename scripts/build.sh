#!/usr/bin/env bash
set -eu

# Determine the arch/os combos we're building for
XC_ARCH=${XC_ARCH:-"386 amd64 arm"}
XC_OS=${XC_OS:-linux darwin windows freebsd openbsd solaris}

# Delete the old dir
echo "Removing old directory..."
rm -rf bin pkg
mkdir -p bin pkg

# Build statically linked binaries
export CGO_ENABLED=0

# Set module download mode to readonly to not implicitly update go.mod
export GOFLAGS="-mod=readonly"

# Download dependencies
go mod download

echo "Building..."
for os in ${XC_OS}; do
    for arch in ${XC_ARCH}; do
        case "${os}/${arch}" in
            darwin/arm|darwin/386)
                continue
                ;;
        esac

        output="pkg/${os}_${arch}/datasplice"
        if [ "${os}" = "windows" ]; then
            output="${output}.exe"
        fi

        mkdir -p "$(dirname "${output}")"
        GOOS="${os}" GOARCH="${arch}" go build -o "${output}" .
    done
done