# stage 1 Generate celestia-appd Binary
FROM golang:1.18-alpine as builder
RUN apk update && apk --no-cache add gcc=latest musl-dev=latest
COPY . /celestia-app
WORKDIR /celestia-app
RUN make build

# stage 2
FROM alpine:3.16
RUN apk update && apk --no-cache add bash=latest

COPY --from=builder /celestia-app/build/celestia-appd /bin/celestia-appd
COPY  docker/entrypoint.sh /opt/entrypoint.sh

# p2p, rpc and prometheus port
EXPOSE 26656 26657 1317 9090

ENV CELESTIA_HOME /opt

ENTRYPOINT [ "/bin/bash", "/opt/entrypoint.sh" ]
