# Stage 1: Build latency-monitor binary
FROM --platform=$BUILDPLATFORM docker.io/golang:1.25.7-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

ENV CGO_ENABLED=0
ENV GO111MODULE=on
# hadolint ignore=DL3018
RUN apk update && apk add --no-cache \
    gcc \
    git \
    # linux-headers are needed for Ledger support
    linux-headers \
    make \
    musl-dev

COPY . /celestia-app
WORKDIR /celestia-app

RUN uname -a && \
    CGO_ENABLED=${CGO_ENABLED} GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -o /celestia-app/build/latency-monitor ./tools/latency-monitor

# Stage 2: Create minimal runtime image
FROM docker.io/alpine:3.22

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
    # Creates a user with $UID and $GID=$UID
    && adduser ${USER_NAME} \
    -D \
    -g ${USER_NAME} \
    -h ${CELESTIA_APP_HOME} \
    -s /sbin/nologin \
    -u ${UID}

# Copy in the latency-monitor binary
COPY --from=builder /celestia-app/build/latency-monitor /bin/latency-monitor

USER ${USER_NAME}

# Set the working directory to the home directory
WORKDIR ${CELESTIA_APP_HOME}

ENTRYPOINT [ "/bin/latency-monitor" ]
