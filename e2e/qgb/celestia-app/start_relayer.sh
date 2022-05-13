#!/bin/bash

# This script runs the QGB relayer with the ability to inject any QGB contract
# address via overwriting the content of the `qgb_address.txt` file.

# check if environment variables are set
if [[ -z "${EVM_CHAIN_ID}" || -z "${PRIVATE_KEY}" ]] || \
   [[ -z "${TENDERMINT_RPC}" || -z "${CELESTIA_GRPC}" ]] || \
   [[ -z "${EVM_ENDPOINT}" ]]
then
  echo "Environment not setup correctly. Please set:"
  echo "EVM_CHAIN_ID, PRIVATE_KEY, TENDERMINT_RPC, CELESTIA_GRPC, EVM_ENDPOINT variables"
  exit -1
fi

# wait for the node to get up and running
while true
do
  status_code=$(curl --write-out '%{http_code}' --silent --output /dev/null core0:26657/status) # TODO don't use a hardcoded address
  if [[ "$status_code" -eq 200 ]] ; then
    break
  fi
  echo "Waiting for node to be up..."
  sleep 5s
done

# check whether to deploy a new contract or use an existing one
if [[ "${DEPLOY_NEW_CONTRACT}" == "true" ]]
then
  /bin/bash /opt/deploy_qgb_contract.sh
fi

# get the address from the `qgb_address.txt` file
# the reason for this is to allow testing against a wrong QGB contract, a faulty one, and
# a contract that is not up to date.
QGB_CONTRACT=$(cat /opt/qgb_address.txt)

/bin/celestia-appd relayer \
  -d ${PRIVATE_KEY} \
  -t ${TENDERMINT_RPC} \
  -c ${CELESTIA_GRPC} \
  -z ${EVM_CHAIN_ID} \
  -e ${EVM_ENDPOINT} \
  -a ${QGB_CONTRACT}
