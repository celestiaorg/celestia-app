# This Dockerfile performs a multi-stage build. 
# It compiles the celestia-appd binary in a large builder image and 
# copies the resulting binary into a minimal runtime image.

# --- Build Arguments (Variables accessible during the build process) ---
ARG BUILDER_IMAGE=docker.io/golang:1.24.6-alpine
ARG RUNTIME_IMAGE=docker.io/alpine:3.22
ARG TARGETOS
ARG TARGETARCH
# Configuration arguments to override defaults within the application (passed to 'make' or entrypoint).
ARG MAX_SQUARE_SIZE
ARG UPGRADE_HEIGHT_DELAY

# ==============================================================================
# Stage 1: builder (Compiles the Go Binary)
# ==============================================================================
# Use a specific platform architecture and base image for compilation.
# hadolint ignore=DL3006
FROM --platform=$BUILDPLATFORM ${BUILDER_IMAGE} AS builder
ARG TARGETOS
ARG TARGETARCH

# Set environment variables for static compilation.
ENV CGO_ENABLED=0
ENV GO111MODULE=on

# Install necessary build tools and headers (linux-headers for Ledger support).
# hadolint ignore=DL3018
RUN apk update && apk add --no-cache \
	gcc \
	git \
	linux-headers \
	make \
	musl-dev
WORKDIR /celestia-app

# Optimize layer caching: Download module dependencies first.
COPY go.mod go.sum ./
RUN go mod download

# Copy source code and build the static binary.
COPY . .

# Build the binary using cross-compilation variables.
RUN uname -a && \
	CGO_ENABLED=${CGO_ENABLED} GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
	make build-standalone

# ==============================================================================
# Stage 2: runtime (Minimal Image for Execution)
# ==============================================================================
# Use a minimal base image.
# hadolint ignore=DL3006
FROM ${RUNTIME_IMAGE} AS runtime

# --- Security Configuration ---
# Use a high UID (10001) for security best practices (non-root user).
ARG UID=10001
ARG USER_NAME=celestia
ENV CELESTIA_APP_HOME=/home/${USER_NAME}/.celestia-app

# Install runtime utilities (bash, curl, jq) and create a dedicated, non-root user.
# hadolint ignore=DL3018
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

# --- Copy Artifacts ---
# Copy the compiled binary from the 'builder' stage.
COPY --from=builder /celestia-app/build/celestia-appd /bin/celestia-appd
# Copy the entrypoint script, setting ownership to the non-root user.
COPY --chown=${USER_NAME}:${USER_NAME} docker/entrypoint.sh /opt/entrypoint.sh

# Switch to the dedicated, non-root user for execution.
USER ${USER_NAME}
# Set the working directory to the application's home directory.
WORKDIR ${CELESTIA_APP_HOME}

# --- Networking and Execution ---
# Expose standard ports for Cosmos/Celestia services.
# 1317: API server
# 9090: GRPC server
# 26656: P2P port
# 26657: RPC port
# 26660: Prometheus metrics
# 26661: Tracing
EXPOSE 1317 9090 26656 26657 26660 26661

# Define the command to run when the container starts.
ENTRYPOINT [ "/bin/bash", "/opt/entrypoint.sh" ]
