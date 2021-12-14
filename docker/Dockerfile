FROM golang:1.17-alpine AS builder
RUN apk update && \
    apk upgrade && \
    apk --no-cache add make \
    git libc-dev bash gcc linux-headers eudev-dev python3

ENV APP /celestia-app

# Add source files
WORKDIR ${APP}
COPY . .

RUN make build

FROM alpine

RUN apk add bash
ENV APP /celestia-app
WORKDIR ${APP}

# Copy in the binary
COPY --from=builder ${APP}/build/celestia-appd .

EXPOSE 26656 26657 1317 9090

COPY docker/priv_validator_state.json ${APP}/data/priv_validator_state.json

# This allows us to always set the --home directory using an env 
# var while still capturing all argumenmts passed at runtime
ENTRYPOINT [ "/bin/bash", "-c", "exec ./celestia-appd \
            --home ${APP} \
            \"${@}\"", "--" ]
# Default command to run if no arguments are passed
CMD ["--help"] 

