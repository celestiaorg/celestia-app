# stage 1 Generate celestia-appd Binary
FROM --platform=$BUILDPLATFORM golang:1.18 as builder
ARG TARGETOS TARGETARCH

# hadolint ignore=DL3018
RUN apt-get update && apt-get install make gcc
COPY . /celestia-app
WORKDIR /celestia-app
RUN env GOOS=$TARGETOS GOARCH=$TARGETARCH LEDGER_ENABLED=false make build

# stage 2
FROM debian
# hadolint ignore=DL3018
RUN apt-get update && apt-get install bash

COPY --from=builder /celestia-app/build/celestia-appd /bin/celestia-appd
COPY  docker/entrypoint.sh /opt/entrypoint.sh

# p2p, rpc and prometheus port
EXPOSE 26656 26657 1317 9090

ENV CELESTIA_HOME /opt

ENTRYPOINT [ "/bin/bash", "/opt/entrypoint.sh" ]