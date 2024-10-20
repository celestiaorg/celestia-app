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

#{
#  sleep 5s
#  /bin/celestia-appd tx bank send core0 celestia1kkthrhfut4s7weqkzd0g3667r53nrjkc77lxk6 10000000000000utia --keyring-backend test --keyring-dir /opt --fees 15000utia --chain-id local_devnet --yes
#  sleep 5s
#  /bin/celestia-appd tx bank send core0 celestia13ncclces2u87526c8ravmz4d524w0vr7yljxyk 10000000000000utia --keyring-backend test --keyring-dir /opt --fees 15000utia --chain-id local_devnet --yes
#  sleep 5s
#  txsim --blob-sizes 2008 --key-mnemonic "arch extra share tag great resource family owner shrug nominee crunch bulk cart twenty ripple bunker sugar carbon unable visual fold leave lyrics soda"  --grpc-endpoint localhost:9090  --blob 50
#}&

/bin/celestia-appd start \
  --moniker core0 \
  --rpc.laddr tcp://0.0.0.0:26657 \
  --p2p.persistent_peers="c7a982ec9ef3af4f0846cb30e439cd70d961ce6e@core1:26656,6570631840e8efb9dc5da90574403a6b27418504@core2:26656,a30ec55c1df749da3f77abbcfc511ba298350609@core3:26656" \
  --home /opt --force-no-bbr --log_level info --v2-upgrade-height 10
