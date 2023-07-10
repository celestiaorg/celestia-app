#!/bin/bash

if [ "$1" = 'create-key' ]; then
  echo "Creating a new keyring-test for the txsim"
  exec /bin/celestia-appd keys add sim --keyring-backend test --home /home/celestia
fi

# TODO: This is a temporary solution to get the txsim working
# Please define your own entrypoint.sh when running a txsim's dockerimage
txsim --help

# example of running a txsim on robusta chain
# txsim --key-path /home/celestia \
#  --rpc-endpoints http://consensus-validator-robusta-rc6.celestia-robusta.com:26657,http://consensus-full-robusta-rc6.celestia-robusta.com:26657 \
#  --grpc-endpoints consensus-validator-robusta-rc6.celestia-robusta.com:9090 --poll-time 10s --blob 10 --seed 100 --send 10 --stake 2
