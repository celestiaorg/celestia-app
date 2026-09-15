#!/bin/sh

# Starts a single node with the privval gRPC endpoint on a non-loopback IP
# behind mutual TLS, then starts fibre connecting to it with a client cert.
#
# Requires ./build/celestia-appd built against a celestia-core with privval
# gRPC TLS support and ./build/fibre. Override with APPD, FIBRE, NODE_IP and
# WORK_DIR. The work dir (default /tmp/fibre-tls) is wiped on every run.

set -o errexit
set -o nounset

APPD="${APPD:-./build/celestia-appd}"
FIBRE="${FIBRE:-./build/fibre}"
NODE_IP="${NODE_IP:-$(ipconfig getifaddr en0 2>/dev/null || hostname -I 2>/dev/null | awk '{print $1}')}"
PRIVVAL_ADDR="${NODE_IP}:26669"
CHAIN_ID="test"
KEY_NAME="validator"

WORK_DIR="${WORK_DIR:-/tmp/fibre-tls}"
APP_HOME="${WORK_DIR}/app"
FIBRE_HOME="${WORK_DIR}/fibre"
CERTS="${WORK_DIR}/certs"
CONFIG="${APP_HOME}/config/config.toml"
APP_PID=""
FIBRE_PID=""

cleanup() {
  [ -n "${FIBRE_PID}" ] && kill "${FIBRE_PID}" 2>/dev/null || true
  [ -n "${APP_PID}" ] && kill "${APP_PID}" 2>/dev/null || true
  wait 2>/dev/null || true
}
trap cleanup INT TERM EXIT

rm -rf "${WORK_DIR}"
mkdir -p "${WORK_DIR}"

echo "work dir:     ${WORK_DIR}"
echo "certs:        ${CERTS}"
echo "privval addr: ${PRIVVAL_ADDR}"
echo ""

echo "--> generating certs"
mkdir -p "${CERTS}"
cd "${CERTS}"
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes \
  -keyout ca.key -out ca.crt -days 1 -subj "/CN=privval-ca" 2>/dev/null
openssl req -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes \
  -keyout server.key -out server.csr -subj "/CN=privval-server" 2>/dev/null
printf 'subjectAltName=IP:%s\nextendedKeyUsage=serverAuth\n' "${NODE_IP}" >server.ext
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out server.crt -days 1 -extfile server.ext 2>/dev/null
openssl req -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes \
  -keyout client.key -out client.csr -subj "/CN=privval-client" 2>/dev/null
printf 'extendedKeyUsage=clientAuth\n' >client.ext
openssl x509 -req -in client.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out client.crt -days 1 -extfile client.ext 2>/dev/null
cd - >/dev/null

echo "--> initializing node"
"${APPD}" init "${CHAIN_ID}" --chain-id "${CHAIN_ID}" --home "${APP_HOME}" >/dev/null 2>&1
"${APPD}" keys add "${KEY_NAME}" --keyring-backend test --home "${APP_HOME}" >/dev/null 2>&1
"${APPD}" genesis add-genesis-account \
  "$("${APPD}" keys show "${KEY_NAME}" -a --keyring-backend test --home "${APP_HOME}")" \
  1000000000000000utia --home "${APP_HOME}" >/dev/null
"${APPD}" genesis gentx "${KEY_NAME}" 5000000000utia --fees 5000utia --keyring-backend test \
  --chain-id "${CHAIN_ID}" --home "${APP_HOME}" >/dev/null 2>&1
"${APPD}" genesis collect-gentxs --home "${APP_HOME}" >/dev/null 2>&1
sed -i.bak '/^\[grpc\]/{n;s#enable = false#enable = true#;}' "${APP_HOME}/config/app.toml"

echo "--> configuring privval gRPC with mTLS"
sed -i.bak "s|^priv_validator_grpc_laddr = .*|priv_validator_grpc_laddr = \"${PRIVVAL_ADDR}\"|" "${CONFIG}"
sed -i.bak "s|^priv_validator_grpc_cert_file = .*|priv_validator_grpc_cert_file = \"${CERTS}/server.crt\"|" "${CONFIG}"
sed -i.bak "s|^priv_validator_grpc_key_file = .*|priv_validator_grpc_key_file = \"${CERTS}/server.key\"|" "${CONFIG}"
sed -i.bak "s|^priv_validator_grpc_client_ca_file = .*|priv_validator_grpc_client_ca_file = \"${CERTS}/ca.crt\"|" "${CONFIG}"

echo "--> starting celestia-appd (logs: ${WORK_DIR}/app.log)"
"${APPD}" start --home "${APP_HOME}" --grpc.enable --delayed-precommit-timeout 1s >"${WORK_DIR}/app.log" 2>&1 &
APP_PID=$!
sleep 5
if ! kill -0 "${APP_PID}" 2>/dev/null; then
  echo "celestia-appd failed to start:"
  grep '^Error:' "${WORK_DIR}/app.log" || tail -5 "${WORK_DIR}/app.log"
  APP_PID=""
  exit 1
fi

echo "--> starting fibre"
"${FIBRE}" start \
  --home "${FIBRE_HOME}" \
  --signer-grpc-address "${PRIVVAL_ADDR}" \
  --signer-grpc-ca-file "${CERTS}/ca.crt" \
  --signer-grpc-cert-file "${CERTS}/client.crt" \
  --signer-grpc-key-file "${CERTS}/client.key" &
FIBRE_PID=$!

wait "${APP_PID}"
