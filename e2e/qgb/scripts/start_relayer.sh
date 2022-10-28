#!/bin/bash

# This script runs the QGB relayer with the ability to deploy a new QGB contract or
# pass one as an environment variable QGB_CONTRACT

# check if environment variables are set
if [[ -z "${EVM_CHAIN_ID}" || -z "${PRIVATE_KEY}" ]] || \
   [[ -z "${TENDERMINT_RPC}" || -z "${CELESTIA_GRPC}" ]] || \
   [[ -z "${EVM_ENDPOINT}" ]]
then
  echo "Environment not setup correctly. Please set:"
  echo "EVM_CHAIN_ID, PRIVATE_KEY, TENDERMINT_RPC, CELESTIA_GRPC, EVM_ENDPOINT variables"
  exit 1
fi

# install needed dependencies
apk add curl

# wait for the node to get up and running
while true
do
  height=$(/bin/celestia-appd query block 1 -n ${TENDERMINT_RPC} 2>/dev/null)
  if [[ -n ${height} ]] ; then
    break
  fi
  echo "Waiting for block 1 to be generated..."
  sleep 5s
done

# check whether to deploy a new contract or use an existing one
if [[ -z "${QGB_CONTRACT}" ]]
then
  export DEPLOY_NEW_CONTRACT=true
  export STARTING_NONCE=earliest
  # expects the script to be mounted to this directory
  /bin/bash /opt/deploy_qgb_contract.sh
fi

# get the address from the `qgb_address.txt` file
QGB_CONTRACT=$(cat /opt/qgb_address.txt)

/bin/celestia-appd relayer \
  -d=${PRIVATE_KEY} \
  -t=${TENDERMINT_RPC} \
  -c=${CELESTIA_GRPC} \
  -z=${EVM_CHAIN_ID} \
  -e=${EVM_ENDPOINT} \
  -a=${QGB_CONTRACT}
