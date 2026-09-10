#!/bin/bash
# Usage: ./scripts/check-goreleaser-image.sh <version> <digest>
#
# Verifies that the goreleaser-cross Docker image exists for the given tag and
# that the tag still resolves to the pinned digest. goreleaser-cross does not
# publish an image for every Go patch version (e.g. it skipped v1.26.1), so
# bumping GOLANG_CROSS_VERSION to a non-existent tag makes the release job fail
# silently and ship no binaries. Comparing the digest also catches a retagged
# image. This check fails loudly at PR time instead.

set -euo pipefail

version=${1:?"usage: $0 <version> <digest>"}
digest=${2:?"usage: $0 <version> <digest>"}
repository="goreleaser/goreleaser-cross"
image="ghcr.io/${repository}:${version}"

echo "Checking that ${image} exists and resolves to ${digest}..."

# Retry on transient network failures so a blip doesn't fail the check.
curl_opts=(--retry 5 --retry-connrefused --retry-delay 5)

# GHCR requires a bearer token even for anonymous pulls of public images.
token=$(curl -fsSL "${curl_opts[@]}" "https://ghcr.io/token?scope=repository:${repository}:pull" |
    sed -E 's/.*"token":"([^"]+)".*/\1/')

headers=$(mktemp)
trap 'rm -f "${headers}"' EXIT

code=$(curl -sSL "${curl_opts[@]}" -o /dev/null -D "${headers}" -w "%{http_code}" \
    -H "Authorization: Bearer ${token}" \
    -H "Accept: application/vnd.oci.image.index.v1+json" \
    -H "Accept: application/vnd.docker.distribution.manifest.list.v2+json" \
    "https://ghcr.io/v2/${repository}/manifests/${version}")

if [ "${code}" = "404" ]; then
    echo "ERROR: ${image} does not exist."
    echo "goreleaser-cross does not publish an image for every Go patch version."
    echo "Pick a published tag from https://github.com/goreleaser/goreleaser-cross/pkgs/container/goreleaser-cross"
    echo "and update GOLANG_CROSS_VERSION in the Makefile."
    exit 1
fi

if [ "${code}" != "200" ]; then
    echo "ERROR: unexpected HTTP status ${code} while checking ${image}."
    echo "Could not determine whether the image exists; failing to be safe."
    exit 1
fi

actual=$(grep -i '^docker-content-digest:' "${headers}" | tail -n 1 | awk '{ print $2 }' | tr -d '\r')
if [ "${actual}" != "${digest}" ]; then
    echo "ERROR: ${image} resolves to ${actual}, not the pinned ${digest}."
    echo "If the tag was intentionally bumped, update GOLANG_CROSS_DIGEST in the Makefile."
    exit 1
fi

echo "OK: ${image} exists and resolves to ${digest}."
