# This Dockerfile performs a multi-stage build. BUILDER_IMAGE is the image used
# to compile the celestia-appd binary. RUNTIME_IMAGE is the image that will be
# returned with the final celestia-appd binary.
#
# Separating the builder and runtime image allows the runtime image to be
# considerably smaller because it doesn't need to have Golang installed.
# celestia-appd links musl dynamically, so the runtime's Alpine must be at least the builder's.
ARG BUILDER_IMAGE=docker.io/golang:1.26.10-alpine3.24
ARG RUNTIME_IMAGE=docker.io/alpine:3.24
ARG TARGETOS
ARG TARGETARCH
# the docker registry used for the embedded v3 binary.
ARG CELESTIA_APP_REPOSITORY=ghcr.io/celestiaorg/celestia-app-standalone
# NOTE: This version must be updated at the same time as the version in the
# Makefile.
ARG CELESTIA_VERSION_V3="v3.13.0"
ARG CELESTIA_VERSION_V4="v4.1.0"
ARG CELESTIA_VERSION_V5="v5.0.12"
ARG CELESTIA_VERSION_V6="v6.4.10"
ARG CELESTIA_VERSION_V7="v7.0.3-mocha"
ARG CELESTIA_VERSION_V8="v8.0.8"
ARG CELESTIA_VERSION_V9="v9.0.8"
ARG CELESTIA_VERSION_V10="v10.5.0"

# Stage 1: this base image contains already released v3 binaries which can be embedded in the multiplexer.
FROM ${CELESTIA_APP_REPOSITORY}:${CELESTIA_VERSION_V3} AS base-v3

# Stage 1b: this base image contains already released v4 binaries which can be embedded in the multiplexer.
FROM ${CELESTIA_APP_REPOSITORY}:${CELESTIA_VERSION_V4} AS base-v4

# Stage 1c: this base image contains already released v5 binaries which can be embedded in the multiplexer.
FROM ${CELESTIA_APP_REPOSITORY}:${CELESTIA_VERSION_V5} AS base-v5

# Stage 1d: this base image contains already released v6 binaries which can be embedded in the multiplexer.
FROM ${CELESTIA_APP_REPOSITORY}:${CELESTIA_VERSION_V6} AS base-v6

# Stage 1e: this base image contains already released v7 binaries which can be embedded in the multiplexer.
FROM ${CELESTIA_APP_REPOSITORY}:${CELESTIA_VERSION_V7} AS base-v7

# Stage 1f: this base image contains already released v8 binaries which can be embedded in the multiplexer.
FROM ${CELESTIA_APP_REPOSITORY}:${CELESTIA_VERSION_V8} AS base-v8

# Stage 1g: this base image contains already released v9 binaries which can be embedded in the multiplexer.
FROM ${CELESTIA_APP_REPOSITORY}:${CELESTIA_VERSION_V9} AS base-v9

# Stage 1h: this base image contains already released v10 binaries which can be embedded in the multiplexer.
FROM ${CELESTIA_APP_REPOSITORY}:${CELESTIA_VERSION_V10} AS base-v10

# xx cross-compiles the cgo build for the target platform without emulation.
FROM --platform=$BUILDPLATFORM docker.io/tonistiigi/xx:1.10.0@sha256:40fe21f44bb8cddf1eb1226db190d0a984118f62ee02a91b77d99448d28264a6 AS xx

# Stage 2: Build the celestia-appd binary inside a builder image that will be discarded later.
# Ignore hadolint rule because hadolint can't parse the variable.
# See https://github.com/hadolint/hadolint/issues/339
# hadolint ignore=DL3006
FROM --platform=$BUILDPLATFORM ${BUILDER_IMAGE} AS builder
# xx is a set of helper scripts meant to be copied over the root filesystem.
# hadolint ignore=DL3067
COPY --from=xx / /

ENV CGO_ENABLED=1
ENV GO111MODULE=on
# hadolint ignore=DL3018
RUN apk update && apk add --no-cache \
    clang \
    git \
    lld \
    make

WORKDIR /celestia-app

# cache go module dependencies
COPY go.mod go.sum ./
COPY docker/download-go-modules.sh /usr/local/bin/download-go-modules.sh
RUN sh /usr/local/bin/download-go-modules.sh

# copy source code after downloading modules (to leverage caching)
COPY . .

# Declare the target platform only after the steps that do not depend on it, so
# the builds for each platform share those layers instead of each storing a copy.
ARG TARGETPLATFORM
ARG TARGETOS
ARG TARGETARCH

# The multiplexer must be built with both TARGETOS and TARGETARCH build arguments
# as the location of the embedded binary is derived from these values.
RUN test -n "$TARGETOS" || (echo "TARGETOS is required but not set" && exit 1)
RUN test -n "$TARGETARCH" || (echo "TARGETARCH is required but not set" && exit 1)

# Compress the v3-v10 binaries from their base images for embedding. Bind
# mounts read them without copying each binary into a layer of its own, which
# kept every uncompressed binary on disk for the rest of the build.
RUN --mount=type=bind,from=base-v3,source=/bin/celestia-appd,target=/tmp/celestia-appd-v3 \
    --mount=type=bind,from=base-v4,source=/bin/celestia-appd,target=/tmp/celestia-appd-v4 \
    --mount=type=bind,from=base-v5,source=/bin/celestia-appd,target=/tmp/celestia-appd-v5 \
    --mount=type=bind,from=base-v6,source=/bin/celestia-appd,target=/tmp/celestia-appd-v6 \
    --mount=type=bind,from=base-v7,source=/bin/celestia-appd,target=/tmp/celestia-appd-v7 \
    --mount=type=bind,from=base-v8,source=/bin/celestia-appd,target=/tmp/celestia-appd-v8 \
    --mount=type=bind,from=base-v9,source=/bin/celestia-appd,target=/tmp/celestia-appd-v9 \
    --mount=type=bind,from=base-v10,source=/bin/celestia-appd,target=/tmp/celestia-appd-v10 \
    for v in 3 4 5 6 7 8 9 10; do \
        tar -cvzf "internal/embedding/celestia-app_${TARGETOS}_v${v}_${TARGETARCH}.tar.gz" "/tmp/celestia-appd-v${v}" || exit 1; \
    done

# The C toolchain for the target platform; linux-headers are needed for Ledger support.
# hadolint ignore=DL3018
RUN xx-apk add --no-cache gcc linux-headers musl-dev

# xx-go --wrap makes go cross-compile for TARGETPLATFORM with the target's C toolchain.
# Remove the Go build cache in the same layer so it is not kept for each platform.
RUN uname -a && \
    xx-go --wrap && \
    make -o mod build DOWNLOAD=false && \
    xx-verify build/celestia-appd && \
    go clean -cache

# Stage 3: Create a minimal image to run the celestia-appd binary
# docker/e2e.Dockerfile copies this stage for the CI e2e image; keep them in sync
# (checked by scripts/e2e_dockerfile_test.sh).
# Ignore hadolint rule because hadolint can't parse the variable.
# See https://github.com/hadolint/hadolint/issues/339
# hadolint ignore=DL3006
FROM ${RUNTIME_IMAGE} AS runtime
# Use UID 10,001 because UIDs below 10,000 are a security risk.
# Ref: https://github.com/hexops/dockerfile/blob/main/README.md#do-not-use-a-uid-below-10000
ARG UID=10001
ARG USER_NAME=celestia
ENV CELESTIA_APP_HOME=/home/${USER_NAME}/.celestia-app
# hadolint ignore=DL3018
RUN apk update && apk add --no-cache \
    bash \
    curl \
    jq \
    && adduser ${USER_NAME} \
    -D \
    -g ${USER_NAME} \
    -h ${CELESTIA_APP_HOME} \
    -s /sbin/nologin \
    -u ${UID}
# Copy the celestia-appd binary from the builder into the final image.
COPY --from=builder /celestia-app/build/celestia-appd /bin/celestia-appd
# Copy the entrypoint script into the final image.
COPY --chown=${USER_NAME}:${USER_NAME} docker/entrypoint.sh /opt/entrypoint.sh
# Set the user to celestia.
USER ${USER_NAME}
# Set the working directory to the home directory.
WORKDIR ${CELESTIA_APP_HOME}
# Expose ports:
# 1317 is the default API server port.
# 9090 is the default GRPC server port.
# 26656 is the default node p2p port.
# 26657 is the default RPC port.
# 26660 is the port used for Prometheus.
# 26661 is the port used for tracing.
EXPOSE 1317 9090 26656 26657 26660 26661
ENTRYPOINT [ "/bin/bash", "/opt/entrypoint.sh" ]
