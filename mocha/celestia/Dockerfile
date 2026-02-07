# Download the v6.4.10 pre-built binary from GitHub releases.
FROM docker.io/debian:trixie-slim

ARG UID=10001
ARG USER_NAME=celestia
ARG VERSION=v6.4.10
ARG TARGETARCH

ENV CELESTIA_APP_HOME=/home/${USER_NAME}/.celestia-app

# hadolint ignore=DL3008
RUN apt-get update && apt-get install -y --no-install-recommends \
    bash \
    curl \
    jq \
    sed \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd -g ${UID} ${USER_NAME} \
    && useradd -m -d ${CELESTIA_APP_HOME} -s /sbin/nologin -u ${UID} -g ${UID} ${USER_NAME}

# Map Docker's TARGETARCH (amd64/arm64) to release asset names (x86_64/arm64)
RUN ARCH="${TARGETARCH}"; \
    if [ "${ARCH}" = "amd64" ]; then ARCH="x86_64"; fi; \
    curl -fsSL "https://github.com/celestiaorg/celestia-app/releases/download/${VERSION}/celestia-app-standalone_Linux_${ARCH}.tar.gz" \
    | tar -xz -C /bin celestia-appd

COPY --chown=${USER_NAME}:${USER_NAME} entrypoint.sh /opt/entrypoint.sh
RUN chmod +x /opt/entrypoint.sh

USER ${USER_NAME}
WORKDIR ${CELESTIA_APP_HOME}

EXPOSE 1317 9090 26656 26657 26660 26661

ENTRYPOINT [ "/bin/bash", "/opt/entrypoint.sh" ]
