#!/bin/sh

# Starts a single node with its privval gRPC endpoint on a non-loopback IP
# behind mutual TLS, then starts fibre connecting to it with a client cert.
#
# Setup (once, from the celestia-app root; don't commit the go.mod change):
#   go mod edit -replace github.com/cometbft/cometbft=../celestia-core
#   make build-standalone && make build-fibre-server
#
# Run:
#   ./scripts/fibre-privval-tls-test.sh                  # MODE=mtls: mTLS on the LAN IP
#   MODE=plaintext ./scripts/fibre-privval-tls-test.sh   # plaintext on 127.0.0.1
#   MODE=insecure ./scripts/fibre-privval-tls-test.sh    # plaintext on the LAN IP, both override flags on
#
# FIBRES=2 starts a second fibre (own home, port 7981, own client cert) against the same node.
# SUBMIT=off skips the blob submission at the end.
#
# Expect in the fibre output: transport=mtls (or plaintext), then "serving gRPC",
# then "Successfully submitted Fibre blob!" once per fibre. That proves the full
# path: fibre signs the payment promise via privval and the tx lands on chain.
# Node logs: /tmp/fibre-tls/app.log. Certs: /tmp/fibre-tls/certs.
#
# Signing request over mTLS, in another terminal while the script runs:
#   CERTS=/tmp/fibre-tls/certs
#   GOGO=$(go list -m -f '{{.Dir}}' github.com/cosmos/gogoproto)
#   grpcurl -import-path ../celestia-core/proto -import-path $GOGO -proto tendermint/privval/types.proto -cacert $CERTS/ca.crt -cert $CERTS/client.crt -key $CERTS/client.key -d '{"chain_id":"test","unique_id":"t","raw_bytes":"aGVsbG8="}' $(ipconfig getifaddr en0):26669 tendermint.privval.PrivValidatorAPI/SignRawBytes
# Drop -cert and -key to see the node reject the request.
#
# Env overrides: APPD, FIBRE, NODE_IP, WORK_DIR (wiped on every run).

set -o errexit
set -o nounset

APPD="${APPD:-./build/celestia-appd}"
FIBRE="${FIBRE:-./build/fibre}"
MODE="${MODE:-mtls}"
FIBRES="${FIBRES:-1}"
SUBMIT="${SUBMIT:-on}"
case "${MODE}" in
  mtls|insecure) NODE_IP="${NODE_IP:-$(ipconfig getifaddr en0 2>/dev/null || hostname -I 2>/dev/null | awk '{print $1}')}" ;;
  plaintext) NODE_IP="127.0.0.1" ;;
  *) echo "MODE must be mtls, plaintext or insecure"; exit 1 ;;
esac
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
FIBRE2_PID=""

cleanup() {
  [ -n "${FIBRE2_PID}" ] && kill "${FIBRE2_PID}" 2>/dev/null || true
  [ -n "${FIBRE_PID}" ] && kill "${FIBRE_PID}" 2>/dev/null || true
  [ -n "${APP_PID}" ] && kill "${APP_PID}" 2>/dev/null || true
  wait 2>/dev/null || true
}
trap cleanup INT TERM EXIT

rm -rf "${WORK_DIR}"
mkdir -p "${WORK_DIR}"

echo "work dir:     ${WORK_DIR}"
echo "mode:         ${MODE}"
echo "fibres:       ${FIBRES}"
echo "privval addr: ${PRIVVAL_ADDR}"
echo ""

if [ "${MODE}" = "mtls" ]; then
echo "--> generating certs (${CERTS})"
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
openssl req -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes \
  -keyout client2.key -out client2.csr -subj "/CN=privval-client-2" 2>/dev/null
openssl x509 -req -in client2.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out client2.crt -days 1 -extfile client.ext 2>/dev/null
cd - >/dev/null
fi

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

sed -i.bak "s|^priv_validator_grpc_laddr = .*|priv_validator_grpc_laddr = \"${PRIVVAL_ADDR}\"|" "${CONFIG}"
if [ "${MODE}" = "insecure" ]; then
echo "--> configuring privval gRPC exposed without TLS (allow_insecure)"
sed -i.bak "s|^priv_validator_grpc_allow_insecure = .*|priv_validator_grpc_allow_insecure = true|" "${CONFIG}"
fi
if [ "${MODE}" = "mtls" ]; then
echo "--> configuring privval gRPC with mTLS"
sed -i.bak "s|^priv_validator_grpc_cert_file = .*|priv_validator_grpc_cert_file = \"${CERTS}/server.crt\"|" "${CONFIG}"
sed -i.bak "s|^priv_validator_grpc_key_file = .*|priv_validator_grpc_key_file = \"${CERTS}/server.key\"|" "${CONFIG}"
sed -i.bak "s|^priv_validator_grpc_client_ca_file = .*|priv_validator_grpc_client_ca_file = \"${CERTS}/ca.crt\"|" "${CONFIG}"
fi

echo "--> starting celestia-appd (logs: ${WORK_DIR}/app.log)"
"${APPD}" start --home "${APP_HOME}" --grpc.enable --delayed-precommit-timeout 1s >"${WORK_DIR}/app.log" 2>&1 &
APP_PID=$!
i=0
until grep -aq 'finalized block' "${WORK_DIR}/app.log"; do
  if ! kill -0 "${APP_PID}" 2>/dev/null; then
    echo "celestia-appd failed to start:"
    grep '^Error:' "${WORK_DIR}/app.log" || tail -5 "${WORK_DIR}/app.log"
    APP_PID=""
    exit 1
  fi
  i=$((i + 1))
  [ "$i" -ge 60 ] && { echo "celestia-appd did not produce a block within 60s (see ${WORK_DIR}/app.log)"; exit 1; }
  sleep 1
done
echo "--> node privval gRPC:"
grep -a 'privval gRPC' "${WORK_DIR}/app.log" | sed 's/\x1b\[[0-9;]*m//g; s/^/    /'

echo "--> starting fibre"
case "${MODE}" in
  mtls)
    "${FIBRE}" start \
      --home "${FIBRE_HOME}" \
      --signer-grpc-address "${PRIVVAL_ADDR}" \
      --signer-grpc-ca-file "${CERTS}/ca.crt" \
      --signer-grpc-cert-file "${CERTS}/client.crt" \
      --signer-grpc-key-file "${CERTS}/client.key" &
    ;;
  plaintext)
    "${FIBRE}" start \
      --home "${FIBRE_HOME}" \
      --signer-grpc-address "${PRIVVAL_ADDR}" &
    ;;
  insecure)
    "${FIBRE}" start \
      --home "${FIBRE_HOME}" \
      --signer-grpc-address "${PRIVVAL_ADDR}" \
      --signer-grpc-allow-insecure &
    ;;
esac
FIBRE_PID=$!

if [ "${FIBRES}" = "2" ]; then
  sleep 3
  echo "--> starting second fibre"
  case "${MODE}" in
    mtls)
      "${FIBRE}" start \
        --home "${FIBRE_HOME}2" \
        --server-listen-address 127.0.0.1:7981 \
        --signer-grpc-address "${PRIVVAL_ADDR}" \
        --signer-grpc-ca-file "${CERTS}/ca.crt" \
        --signer-grpc-cert-file "${CERTS}/client2.crt" \
        --signer-grpc-key-file "${CERTS}/client2.key" &
      ;;
    plaintext)
      "${FIBRE}" start \
        --home "${FIBRE_HOME}2" \
        --server-listen-address 127.0.0.1:7981 \
        --signer-grpc-address "${PRIVVAL_ADDR}" &
      ;;
    insecure)
      "${FIBRE}" start \
        --home "${FIBRE_HOME}2" \
        --server-listen-address 127.0.0.1:7981 \
        --signer-grpc-address "${PRIVVAL_ADDR}" \
        --signer-grpc-allow-insecure &
      ;;
  esac
  FIBRE2_PID=$!
fi

# tx <args...>: send a tx from the validator key and print its code.
tx() {
  "${APPD}" tx "$@" --from "${KEY_NAME}" --keyring-backend test --home "${APP_HOME}" \
    --chain-id "${CHAIN_ID}" --fees 5000utia --yes -o json | grep -o '"code":[0-9]*'
  sleep 3
}

# submit_blob <fibre host>: register the host for the validator and upload a blob through it.
submit_blob() {
  echo "--> registering fibre host $1 and submitting a blob"
  tx valaddr set-host "$1"
  go run ./tools/submit-fibre-blob --home "${APP_HOME}" --grpc-addr localhost:9090
}

if [ "${SUBMIT}" = "on" ]; then
  sleep 3
  echo "--> depositing to escrow"
  tx fibre deposit-to-escrow 1000000000utia
  submit_blob 127.0.0.1:7980
  [ "${FIBRES}" = "2" ] && submit_blob 127.0.0.1:7981
fi

wait "${APP_PID}"
