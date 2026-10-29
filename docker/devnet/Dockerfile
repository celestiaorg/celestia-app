# syntax=docker/dockerfile:1
FROM golang:1.26.10-bookworm AS build
WORKDIR /src
ENV GOWORK=off CGO_ENABLED=0
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=unknown
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    make -o mod build-standalone build-fibre-server VERSION="$VERSION" COMMIT="$COMMIT" && \
    go build -o build/submit ./local_devnet/submit

FROM debian:bookworm-slim AS runtime
ARG VERSION=dev
ARG COMMIT=unknown
LABEL org.opencontainers.image.source="https://github.com/celestiaorg/celestia-app" \
      org.opencontainers.image.version="$VERSION" \
      org.opencontainers.image.revision="$COMMIT"
# hadolint ignore=DL3008
RUN apt-get update && apt-get install -y --no-install-recommends bash ca-certificates curl jq netcat-openbsd procps \
    && rm -rf /var/lib/apt/lists/*
COPY docker/devnet/credentials.sh docker/devnet/health.sh docker/devnet/provision.sh docker/devnet/validator.sh /opt/devnet/
ENV P2P_NETWORK=devnet

FROM runtime AS credentials
COPY --from=build /src/build/celestia-appd /usr/local/bin/
RUN bash /opt/devnet/credentials.sh

FROM runtime AS validator
COPY --from=build /src/build/celestia-appd /src/build/fibre /src/build/submit /usr/local/bin/
COPY --from=credentials /credentials /credentials
ENV CELESTIA_APP_HOME=/data/validator FIBRE_HOST=localhost:7980
EXPOSE 1317 9090 26657 7980
HEALTHCHECK --interval=2s --timeout=5s --start-period=240s --retries=3 \
    CMD ["bash", "/opt/devnet/health.sh"]
CMD ["bash", "/opt/devnet/validator.sh"]
