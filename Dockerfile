# stage 1 Generate celestia-appd Binary
FROM golang:1.17-alpine as builder
RUN apk update && apk --no-cache add make gcc git musl-dev
COPY . /celestia-app
WORKDIR /celestia-app
RUN make build

# stage 2
FROM alpine
RUN apk update && apk --no-cache add bash

COPY --from=builder /celestia-app/build/celestia-appd /opt
COPY docker/priv_validator_state.json /opt/data
WORKDIR /opt

# p2p, rpc and prometheus port
EXPOSE 26656 26657 1317 9090

# This allows us to always set the --home directory using an env
# var while still capturing all arguments passed at runtime
ENTRYPOINT [ "/bin/bash", "-c", "exec ./celestia-appd \
            --home /opt \
            \"${@}\"", "--" ]
# Default command to run if no arguments are passed
CMD ["--help"]
