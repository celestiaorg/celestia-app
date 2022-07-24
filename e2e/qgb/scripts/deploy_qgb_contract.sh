#!/bin/bash

# This script deploys the QGB contract and outputs the address to stdout.

# check whether to deploy a new contract or no need
if [[ "${DEPLOY_NEW_CONTRACT}" != "true" ]]
then
  echo "no need to deploy a new QGB contract. exiting..."
  exit 0
fi

# check if environment variables are set
if [[ -z "${EVM_CHAIN_ID}" || -z "${PRIVATE_KEY}" ]] || \
   [[ -z "${TENDERMINT_RPC}" || -z "${CELESTIA_GRPC}" ]] || \
   [[ -z "${EVM_ENDPOINT}" || -z "${STARTING_NONCE}" ]]
then
  echo "Environment not setup correctly. Please set:"
  echo "EVM_CHAIN_ID, PRIVATE_KEY, TENDERMINT_RPC, CELESTIA_GRPC, EVM_ENDPOINT, STARTING_NONCE variables"
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

# wait for the ethereum node to start
while true
do
    status_code=$(curl --write-out '%{http_code}' --silent --output /dev/null \
                      --location --request POST ${EVM_ENDPOINT} \
                      --header 'Content-Type: application/json' \
                      --data-raw "{
                  	    \"jsonrpc\":\"2.0\",
                  	    \"method\":\"eth_blockNumber\",
                  	    \"params\":[],
                  	    \"id\":${EVM_CHAIN_ID}}")
    if [[ "${status_code}" -eq 200 ]] ; then
      break
    fi
    echo "Waiting for ethereum node to be up..."
    sleep 2s
done

echo "deploying QGB contract..."

/bin/celestia-appd deploy \
  -x ${EVM_CHAIN_ID} \
  -d ${PRIVATE_KEY} \
  -t ${TENDERMINT_RPC} \
  -c ${CELESTIA_GRPC} \
  -z ${EVM_CHAIN_ID} \
  -n ${STARTING_NONCE} \
  -e ${EVM_ENDPOINT} > /opt/output

echo $(cat /opt/output)

cat /opt/output | cut -f 2 -d = | cut -f 1 -d\ > /opt/qgb_address.txt
