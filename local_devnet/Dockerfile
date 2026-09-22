# syntax=docker/dockerfile:1
FROM golang:1.26.6-bookworm AS build
WORKDIR /src
ENV GOWORK=off GOMAXPROCS=4
COPY go.mod go.sum ./
COPY third_party/reedsolomon ./third_party/reedsolomon
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -p 4 -tags=ledger -o /out/celestia-appd ./cmd/celestia-appd && \
    go build -p 4 -o /out/fibre ./fibre/cmd && \
    go build -p 4 -o /out/submit ./local_devnet/submit

FROM debian:bookworm-slim
# hadolint ignore=DL3008
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl jq netcat-openbsd \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/ /usr/local/bin/
COPY local_devnet/*.sh /opt/devnet/
WORKDIR /opt/devnet
