# stage 1 Generate celestia-appd Binary
FROM docker.io/golang:1.18.10-alpine3.17 as builder
# hadolint ignore=DL3018
RUN apk update && apk --no-cache add \
    gcc \
    git \
    make \
    musl-dev
COPY . /celestia-app
WORKDIR /celestia-app
RUN make build

# stage 2
FROM docker.io/alpine:3.17.2

# Read here why UID 10001: https://github.com/hexops/dockerfile/blob/main/README.md#do-not-use-a-uid-below-10000
ARG UID=10001
ARG USER_NAME=celestia

ENV CELESTIA_HOME=/opt/${USER_NAME}

# hadolint ignore=DL3018
RUN apk update && apk --no-cache add \
    bash

COPY --from=builder /celestia-app/build/celestia-appd /bin/celestia-appd
COPY  docker/entrypoint.sh /opt/entrypoint.sh

# Creates a user with $UID and $GID=$UID
RUN adduser ${USER_NAME} \
    -D \
    -g "celestia" \
    -h ${CELESTIA_HOME} \
    -s /sbin/nologin \
    -u ${UID}

USER ${USER_NAME}

# p2p, rpc and prometheus port
EXPOSE 26656 26657 1317 9090

ENTRYPOINT [ "/bin/bash", "/opt/entrypoint.sh" ]
