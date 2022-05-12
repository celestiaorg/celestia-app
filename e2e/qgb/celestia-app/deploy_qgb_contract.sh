#!/bin/bash

# This script deploys the QGB contract and outputs the address in `qgb_address.txt` file

# check if environment variables are set
if [[ -z "${EVM_CHAIN_ID}" || -z "${PRIVATE_KEY}" ]] || \
   [[ -z "${TENDERMINT_RPC}" || -z "${CELESTIA_GRPC}" ]] || \
   [[ -z "${EVM_CHAIN_ID}" || -z "${EVM_ENDPOINT}"]]
then
  echo "Environment not setup correctly. Please set:"
  echo "EVM_CHAIN_ID, PRIVATE_KEY, TENDERMINT_RPC, CELESTIA_GRPC, EVM_CHAIN_ID, EVM_ENDPOINT variables"
  exit -1
fi

/bin/celestia-appd deploy \
  -x ${EVM_CHAIN_ID} \
  -d ${PRIVATE_KEY} \
  --rpc.laddr ${TENDERMINT_RPC} \
  -c ${CELESTIA_GRPC} \
  -z ${EVM_CHAIN_ID} \
  -e ${EVM_ENDPOINT} > /opt/output

cat /opt/output | tail -n 1 | cut -d\  -f 4 > /opt/qgb_address.txt
