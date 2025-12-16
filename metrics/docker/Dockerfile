FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the metrics-server binary
RUN CGO_ENABLED=0 GOOS=linux go build -o /metrics-server ./metrics/cmd/metrics-server

# Final stage
FROM alpine:3.19

RUN apk --no-cache add ca-certificates

# Create non-root user and data directory
RUN adduser -D -u 10001 metrics && \
    mkdir -p /data && \
    chown metrics:metrics /data
USER metrics

COPY --from=builder /metrics-server /usr/local/bin/metrics-server

EXPOSE 9900

ENTRYPOINT ["metrics-server"]
CMD ["--port", "9900", "--targets-file", "/data/targets.json"]
