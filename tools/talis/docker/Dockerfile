# Use the official Go builder image
FROM golang:1.24 AS builder

ARG LDFLAGS

# make sure /out exists
RUN mkdir -p /out

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN GOOS=${GOOS} GOARCH=${GOARCH} CGO_ENABLED=0 go build -tags="ledger" -ldflags="${LDFLAGS}" -o /out/txsim ./test/cmd/txsim \
 && GOOS=${GOOS} GOARCH=${GOARCH} CGO_ENABLED=0 GOOS=linux go build -tags="ledger" -ldflags="${LDFLAGS}" -o /out/celestia-appd ./cmd/celestia-appd 
