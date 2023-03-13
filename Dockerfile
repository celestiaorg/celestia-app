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
# hadolint ignore=DL3018
RUN apk update && apk --no-cache add \
    bash

COPY --from=builder /celestia-app/build/celestia-appd /bin/celestia-appd
COPY  docker/entrypoint.sh /opt/entrypoint.sh

# p2p, rpc and prometheus port
EXPOSE 26656 26657 1317 9090

ENV CELESTIA_HOME /opt

RUN adduser -D -u 1000 celestia \
    && chown -R celestia:celestia /opt

USER celestia

ENTRYPOINT [ "/bin/bash", "/opt/entrypoint.sh" ]
