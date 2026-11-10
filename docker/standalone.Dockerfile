# This Dockerfile performs a multi-stage build. BUILDER_IMAGE is the image used
# to compile the celestia-appd binary. RUNTIME_IMAGE is the image that will be
# returned with the final celestia-appd binary.
#
# Separating the builder and runtime image allows the runtime image to be
# considerably smaller because it doesn't need to have Golang installed.
ARG BUILDER_IMAGE=docker.io/golang:1.26.10-alpine3.24
ARG RUNTIME_IMAGE=docker.io/alpine:3.24

# xx cross-compiles the cgo build for the target platform without emulation.
FROM --platform=$BUILDPLATFORM docker.io/tonistiigi/xx:1.10.0@sha256:40fe21f44bb8cddf1eb1226db190d0a984118f62ee02a91b77d99448d28264a6 AS xx

# Stage 1: Build the celestia-appd binary inside a builder image that will be discarded later.
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

ARG TARGETPLATFORM
# The C toolchain for the target platform; linux-headers are needed for Ledger support.
# hadolint ignore=DL3018
RUN xx-apk add --no-cache gcc linux-headers musl-dev

# xx-go --wrap cross-compiles for TARGETPLATFORM with the target's C toolchain. STATIC=true links
# musl in, so the binary runs outside this image; the grep fails a build without cgo.
# hadolint ignore=DL4006
RUN uname -a && \
    xx-go --wrap && \
    make -o mod build-standalone STATIC=true && \
    xx-verify --static build/celestia-appd && \
    go version -m build/celestia-appd | grep -q CGO_ENABLED=1

# Stage 2: Create a minimal image to run the celestia-appd binary
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
