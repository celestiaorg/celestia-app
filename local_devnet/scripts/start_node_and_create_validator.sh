#!/bin/bash

# This script starts a Celestia-app, creates a validator with the provided parameters, then
# keeps running it validating blocks.

# check if environment variables are set
if [[ -z "${CELESTIA_HOME}" || -z "${MONIKER}" || -z "${AMOUNT}" ]]
then
  echo "Environment not setup correctly. Please set: CELESTIA_HOME, MONIKER, AMOUNT variables"
  exit 1
fi

# create necessary structure if doesn't exist
if [[ ! -f ${CELESTIA_HOME}/data/priv_validator_state.json ]]
then
    mkdir "${CELESTIA_HOME}"/data
    cat <<EOF > ${CELESTIA_HOME}/data/priv_validator_state.json
{
  "height": "0",
  "round": 0,
  "step": 0
}
EOF
fi

{
  # wait for the node to get up and running
  while true
  do
    status_code=$(curl --write-out '%{http_code}' --silent --output /dev/null localhost:26657/status)
    if [[ "${status_code}" -eq 200 ]] ; then
      break
    fi
    echo "Waiting for node to be up..."
    sleep 2s
  done

  VAL_ADDRESS=$(celestia-appd keys show "${MONIKER}" --keyring-backend test --bech=val --home /opt -a)
  # keep retrying to create a validator
  while true
  do
    # create validator
    celestia-appd tx staking create-validator \
      --amount="${AMOUNT}" \
      --pubkey="$(celestia-appd tendermint show-validator --home "${CELESTIA_HOME}")" \
      --moniker="${MONIKER}" \
      --chain-id="local_devnet" \
      --commission-rate=0.1 \
      --commission-max-rate=0.2 \
      --commission-max-change-rate=0.01 \
      --min-self-delegation=1000000 \
      --from="${MONIKER}" \
      --keyring-backend=test \
      --home="${CELESTIA_HOME}" \
      --broadcast-mode=block \
      --fees="300000utia" \
      --yes
    output=$(celestia-appd query staking validator "${VAL_ADDRESS}" 2>/dev/null)
    if [[ -n "${output}" ]] ; then
      break
    fi
    echo "trying to create validator..."
    sleep 1s
  done

#  sleep 20s
#  txsim --blob-sizes 2008 --key-mnemonic "ladder south movie meat because before flame blade electric height impose learn file dose shine inmate pioneer chest gun leopard tell vessel hint raccoon"  --grpc-endpoint localhost:9090  --blob 50
} &

# start node
celestia-appd start \
--home="${CELESTIA_HOME}" \
--moniker="${MONIKER}" \
--p2p.persistent_peers="e3c592c0c2ad4b05cef3791456b0d6dd4da72ed2@core0:26656,c7a982ec9ef3af4f0846cb30e439cd70d961ce6e@core1:26656,6570631840e8efb9dc5da90574403a6b27418504@core2:26656,a30ec55c1df749da3f77abbcfc511ba298350609@core3:26656" \
--rpc.laddr=tcp://0.0.0.0:26657 --force-no-bbr --log_level info --v2-upgrade-height 10
