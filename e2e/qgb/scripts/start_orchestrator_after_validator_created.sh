#!/bin/bash

# This script waits  for the validator to be created before starting the orchestrator

# check if environment variables are set
if [[ -z "${MONIKER}" || -z "${PRIVATE_KEY}" ]] || \
   [[ -z "${TENDERMINT_RPC}" || -z "${CELESTIA_GRPC}" ]]
then
  echo "Environment not setup correctly. Please set:"
  echo "MONIKER, PRIVATE_KEY, TENDERMINT_RPC, CELESTIA_GRPC variables"
  exit 1
fi

# install needed dependencies
apk add curl

# wait for the validator to be created before starting the orchestrator
VAL_ADDRESS=$(celestia-appd keys show ${MONIKER} --keyring-backend test --bech=val --home /opt -a)
while true
do
  # verify that the node is listening on gRPC
  nc -z -w5 $(echo $CELESTIA_GRPC | cut -d : -f 1) $(echo $CELESTIA_GRPC | cut -d : -f 2)
  result=$?
  if [ "${result}" != "0" ]; then
    echo "Waiting for node gRPC to be available ..."
    sleep 5s
    continue
  fi

  # verify if RPC is running and the validator was created
  output=$(celestia-appd query staking validator ${VAL_ADDRESS} --node $TENDERMINT_RPC 2>/dev/null)
  if [[ -n "${output}" ]] ; then
    break
  fi
  echo "Waiting for validator to be created..."
  sleep 5s
done

/bin/celestia-appd orchestrator \
  -p=/opt \
  -x=qgb-e2e \
  -d=${PRIVATE_KEY} \
  --keyring-account=${MONIKER} \
  -t=${TENDERMINT_RPC} \
  -c=${CELESTIA_GRPC}
