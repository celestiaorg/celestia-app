FROM --platform=$BUILDPLATFORM docker.io/golang:1.25.7-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

ENV CGO_ENABLED=0
ENV GO111MODULE=on
RUN apk update && apk add --no-cache git

COPY . /celestia-app
WORKDIR /celestia-app

RUN uname -a && \
    CGO_ENABLED=${CGO_ENABLED} GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -o /celestia-app/build/latency-monitor ./tools/latency-monitor

FROM docker.io/alpine:3.22

ARG UID=10001
ARG USER_NAME=celestia

ENV CELESTIA_APP_HOME=/home/${USER_NAME}/.celestia-app

RUN apk update && apk add --no-cache \
    bash \
    curl \
    jq \
    && adduser ${USER_NAME} \
    -D \
    -g ${USER_NAME} \
    -h ${CELESTIA_APP_HOME} \
    -s /sbin/nologin \
    -u ${UID}

COPY --from=builder /celestia-app/build/latency-monitor /bin/latency-monitor

USER ${USER_NAME}

WORKDIR ${CELESTIA_APP_HOME}

ENTRYPOINT [ "/bin/latency-monitor" ]
