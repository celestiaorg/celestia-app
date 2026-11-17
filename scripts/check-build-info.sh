#!/bin/sh
# Usage: ./scripts/check-build-info.sh <binary> [go.mod]
#
# Fails unless <binary> was built the same way as the release binaries: with
# -trimpath and with the exact Go version in the go directive of go.mod. Both
# settings decide the file paths and standard library strings compiled into the
# binary, so every way of building celestia-app must use the same values.

set -eu

binary=${1:?"usage: $0 <binary> [go.mod]"}
gomod=${2:-go.mod}

want_go="go$(sed -n 's/^go //p' "${gomod}")"
info=$(go version -m "${binary}")
got_go=$(printf '%s\n' "${info}" | sed -n '1s/^.*: //p')

status=0
if [ "${got_go}" != "${want_go}" ]; then
    echo "ERROR: ${binary} was built with ${got_go}, but ${gomod} requires ${want_go}."
    echo "Build with the Makefile, which sets GOTOOLCHAIN=${want_go}."
    status=1
fi
if ! printf '%s\n' "${info}" | grep -Eq '^[[:space:]]+build[[:space:]]+-trimpath=true$'; then
    echo "ERROR: ${binary} was not built with -trimpath."
    status=1
fi
if [ "${status}" -eq 0 ]; then
    echo "OK: ${binary} was built with ${got_go} and -trimpath."
fi
exit "${status}"
