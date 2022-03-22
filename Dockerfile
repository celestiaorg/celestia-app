# stage 1 Generate celestia-appd Binary
FROM golang:1.17-alpine as builder
RUN apk update && apk --no-cache add make gcc git musl-dev
COPY . /opt
WORKDIR /opt
RUN make build

# stage 2
FROM alpine
RUN apk update && apk --no-cache add curl jq bash

COPY --from=builder /opt/build/celestia-appd /opt/celestia-appd
COPY docker/priv_validator_state.json /opt/data/priv_validator_state.json
WORKDIR /opt

# p2p, rpc and prometheus port
EXPOSE 26656 26657 1317 9090

# This allows us to always set the --home directory using an env
# var while still capturing all arguments passed at runtime
CMD [ "/bin/bash", "-c", "exec ./celestia-appd \
            --home ${CELESTIA_HOME_DIR} \
            \"${@}\"", "--" ]
# Default command to run if no arguments are passed
#CMD ["--help"]
