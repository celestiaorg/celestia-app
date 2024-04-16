# stage 1 Generate celestia-appd Binary
FROM docker.io/golang:1.22.0-alpine3.19 as builder
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
RUN make build

# stage 2
FROM docker.io/alpine:3.18.2

# Read here why UID 10001: https://github.com/hexops/dockerfile/blob/main/README.md#do-not-use-a-uid-below-10000
ARG UID=10001
ARG USER_NAME=celestia

ENV CELESTIA_HOME=/home/${USER_NAME}

# hadolint ignore=DL3018
RUN apk update && apk add --no-cache \
    bash \
    # Creates a user with $UID and $GID=$UID
    && adduser ${USER_NAME} \
    -D \
    -g ${USER_NAME} \
    -h ${CELESTIA_HOME} \
    -s /sbin/nologin \
    -u ${UID}

# Copy in the binary
COPY --from=builder /celestia-app/build/celestia-appd /bin/celestia-appd

COPY --chown=${USER_NAME}:${USER_NAME} docker/entrypoint.sh /opt/entrypoint.sh

USER ${USER_NAME}

# p2p, rpc and prometheus port
EXPOSE 26656 26657 1317 9090

=======
# Set the working directory to the home directory.
WORKDIR ${CELESTIA_HOME}
# Expose ports:
# 1317 is the default API server port.
# 9090 is the default GRPC server port.
# 26656 is the default node p2p port.
# 26657 is the default RPC port.
# 26660 is the port used for Prometheus.
# 26661 is the port used for tracing.
EXPOSE 1317 9090 26656 26657 26660 26661
ENTRYPOINT [ "/bin/bash", "/opt/entrypoint.sh" ]
