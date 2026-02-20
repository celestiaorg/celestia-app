# Download pre-built celestia-appd from the v6.4.10-mocha release.
# https://github.com/celestiaorg/celestia-app/releases/tag/v6.4.10-mocha
#
# Uses the multiplexer binary (not standalone) to reproduce
# https://github.com/celestiaorg/celestia-app/issues/6601

FROM docker.io/debian:trixie-slim

ARG VERSION=v6.4.10-mocha
ARG UID=10001
ARG USER_NAME=celestia

ENV CELESTIA_APP_HOME=/home/${USER_NAME}/.celestia-app

# hadolint ignore=DL3008
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    jq \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd -g ${UID} ${USER_NAME} \
    && useradd -l -m -d ${CELESTIA_APP_HOME} -s /sbin/nologin -u ${UID} -g ${UID} ${USER_NAME}

# Download the pre-built binary. Map Docker's TARGETARCH to the release naming.
ARG TARGETARCH
RUN ARCH=$([ "${TARGETARCH}" = "amd64" ] && echo "x86_64" || echo "arm64") \
    && curl -sL "https://github.com/celestiaorg/celestia-app/releases/download/${VERSION}/celestia-app_Linux_${ARCH}.tar.gz" \
    | tar -xz -C /bin celestia-appd

COPY --chown=${USER_NAME}:${USER_NAME} entrypoint.sh /opt/entrypoint.sh
RUN chmod +x /opt/entrypoint.sh

USER ${USER_NAME}
WORKDIR ${CELESTIA_APP_HOME}

EXPOSE 1317 9090 26656 26657 26660 26661

ENTRYPOINT [ "/bin/bash", "/opt/entrypoint.sh" ]
