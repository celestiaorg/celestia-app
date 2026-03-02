# Build celestia-appd from source (standalone, from the main branch).

FROM docker.io/golang:1.25-bookworm AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN make build-standalone

# ---

FROM docker.io/debian:trixie-slim

ARG UID=10001
ARG USER_NAME=celestia

ENV CELESTIA_APP_HOME=/home/${USER_NAME}/.celestia-app

# hadolint ignore=DL3008
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    jq \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd -g ${UID} ${USER_NAME} \
    && useradd -l -m -d ${CELESTIA_APP_HOME} -s /sbin/nologin -u ${UID} -g ${UID} ${USER_NAME}

COPY --from=builder /src/build/celestia-appd /bin/celestia-appd
COPY --chown=${USER_NAME}:${USER_NAME} docker/arabica/celestia/entrypoint.sh /opt/entrypoint.sh
RUN chmod +x /opt/entrypoint.sh

USER ${USER_NAME}
WORKDIR ${CELESTIA_APP_HOME}

EXPOSE 1317 9090 26656 26657 26660 26661

ENTRYPOINT [ "/bin/bash", "/opt/entrypoint.sh" ]
