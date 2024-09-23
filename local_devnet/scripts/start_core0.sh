#!/bin/bash

# This script starts core0

if [[ ! -f /opt/data/priv_validator_state.json ]]
then
    mkdir /opt/data
    cat <<EOF > /opt/data/priv_validator_state.json
{
  "height": "0",
  "round": 0,
  "step": 0
}
EOF
fi

/bin/celestia-appd start \
  --moniker core0 \
  --rpc.laddr tcp://0.0.0.0:26657 \
  --home /opt \
  --force-no-bbr
