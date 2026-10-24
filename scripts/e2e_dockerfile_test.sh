#!/usr/bin/env bash
set -euo pipefail

# The CI e2e image (docker/e2e.Dockerfile) must stay the runtime stage of the
# release image (docker/multiplexer.Dockerfile), and its embedded binaries come
# from the Makefile versions, so those must match the Dockerfile's too.
cd "$(dirname "${BASH_SOURCE[0]}")/.."

# Prints the runtime stage without comments and the line that copies the binary.
runtime_stage() {
    grep -E '^ARG RUNTIME_IMAGE=' "$1"
    sed -n '/^FROM \${RUNTIME_IMAGE} AS runtime$/,$p' "$1" \
        | grep -vE '^#|^COPY .* /bin/celestia-appd$'
}

if ! diff <(runtime_stage docker/multiplexer.Dockerfile) <(runtime_stage docker/e2e.Dockerfile); then
    echo "ERROR: docker/e2e.Dockerfile has drifted from the runtime stage of docker/multiplexer.Dockerfile"
    exit 1
fi

makefile_versions=$(sed -nE 's/^CELESTIA_V([0-9]+)_VERSION := (.+)$/\1 \2/p' Makefile)
dockerfile_versions=$(sed -nE 's/^ARG CELESTIA_VERSION_V([0-9]+)="(.+)"$/\1 \2/p' docker/multiplexer.Dockerfile)
if [ -z "$makefile_versions" ] || [ "$makefile_versions" != "$dockerfile_versions" ]; then
    echo "ERROR: embedded binary versions differ between Makefile and docker/multiplexer.Dockerfile"
    diff <(echo "$makefile_versions") <(echo "$dockerfile_versions") || true
    exit 1
fi
echo "e2e Dockerfile and embedded binary versions are in sync"
