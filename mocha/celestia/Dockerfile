# Download the v6.4.10 pre-built binary from GitHub releases.
FROM docker.io/debian:trixie-slim

ARG UID=10001
ARG USER_NAME=celestia
ARG VERSION=v6.4.10
ARG TARGETARCH

ENV CELESTIA_APP_HOME=/home/${USER_NAME}/.celestia-app

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates=20250419 \
    curl=8.14.1-2+deb13u2 \
    jq=1.7.1-6+deb13u1 \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd -g ${UID} ${USER_NAME} \
    && useradd -l -m -d ${CELESTIA_APP_HOME} -s /sbin/nologin -u ${UID} -g ${UID} ${USER_NAME}

SHELL ["/bin/bash", "-o", "pipefail", "-c"]
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
