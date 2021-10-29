FROM golang:1.17-alpine AS builder
RUN apk update && \
    apk upgrade && \
    apk --no-cache add make \
    git libc-dev bash gcc linux-headers eudev-dev python3

# Add source files
WORKDIR /go/src/github.com/celestiaorg/celestia-app
COPY . .

RUN make install

FROM golang:1.17-alpine

ENV APP /capp
ENV CHAIN_ID "test"
ENV NODE_NAME "user1"
ENV KEY_TYPE "test"
ENV COINS_TYPE "10000000000stake,100000000000samoleans"

RUN apk update && \
    apk upgrade && \
    apk --no-cache add curl jq bash && \
    addgroup cappuser && \
    adduser -S -G cappuser cappuser -h "$APP"

WORKDIR $APP

# Run the container with cappuser by default
USER cappuser

WORKDIR $APP

COPY --from=builder /go/bin/celestia-appd /usr/bin/celestia-appd

EXPOSE 26656 26657 1317 9090
ADD /docker/entry.sh /capp/entry.sh
USER root
RUN chmod +x /capp/entry.sh
USER cappuser
ENTRYPOINT [ "/capp/entry.sh" ]
