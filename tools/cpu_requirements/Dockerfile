# Build stage
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make gcc musl-dev linux-headers

# Set working directory
WORKDIR /app

# Copy go module files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy the entire project (needed for local dependencies)
COPY . .

# Build the cpu_requirements tool
RUN go build -o /cpu_requirements ./tools/cpu_requirements

# Runtime stage
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache ca-certificates

# Copy the binary from builder
COPY --from=builder /cpu_requirements /usr/local/bin/cpu_requirements

# Set the entrypoint to run the benchmarking tool
ENTRYPOINT ["/usr/local/bin/cpu_requirements"]
