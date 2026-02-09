# Build celestia-appd v6.4.10 from source with:
# - celestia-core PR #2779: fix PruneBlocks nil dereference (celestia-core#2772)
# - cosmos-sdk v0.51.9: remove evidence MaxAgeNumBlocks block retention constraint

FROM docker.io/golang:1.25.6-alpine AS builder

ARG VERSION=v6.4.10

# hadolint ignore=DL3018
RUN apk add --no-cache \
    gcc \
    git \
    linux-headers \
    make \
    musl-dev

WORKDIR /src
RUN git clone --branch ${VERSION} --depth 1 https://github.com/celestiaorg/celestia-app.git .

# Swap in the celestia-core fix for PruneBlocks nil dereference
# and cosmos-sdk v0.51.9 which removes the evidence MaxAgeNumBlocks
# constraint from GetBlockRetentionHeight
RUN go mod edit -replace github.com/cometbft/cometbft=github.com/rootulp/celestia-core@a37b4ac850a5e01920cf33bd0511ab073740048d \
    && go mod edit -replace github.com/cosmos/cosmos-sdk=github.com/celestiaorg/cosmos-sdk@v0.51.9 \
    && go mod tidy

# Static link so the musl-built binary runs on the Debian runtime stage
RUN CGO_LDFLAGS="-static" make build-standalone

FROM docker.io/debian:trixie-slim

ARG UID=10001
ARG USER_NAME=celestia

ENV CELESTIA_APP_HOME=/home/${USER_NAME}/.celestia-app

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates=20250419 \
    curl=8.14.1-2+deb13u2 \
    jq=1.7.1-6+deb13u1 \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd -g ${UID} ${USER_NAME} \
    && useradd -l -m -d ${CELESTIA_APP_HOME} -s /sbin/nologin -u ${UID} -g ${UID} ${USER_NAME}

COPY --from=builder /src/build/celestia-appd /bin/celestia-appd

COPY --chown=${USER_NAME}:${USER_NAME} entrypoint.sh /opt/entrypoint.sh
RUN chmod +x /opt/entrypoint.sh

USER ${USER_NAME}
WORKDIR ${CELESTIA_APP_HOME}

EXPOSE 1317 9090 26656 26657 26660 26661

ENTRYPOINT [ "/bin/bash", "/opt/entrypoint.sh" ]
