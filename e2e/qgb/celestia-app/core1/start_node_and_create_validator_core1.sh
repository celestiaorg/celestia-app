# create necessary structure
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

{
  # wait for the node to get up and running
  while true
  do
    status_code=$(curl --write-out '%{http_code}' --silent --output /dev/null localhost:26657/status)
    if [[ "$status_code" -e 200 ]] ; then
      break
    fi
    echo "Waiting for node to be up..."
    sleep 5s
  done

  # create validator
  bash /opt/create_core1_validator.sh
} &

# start node
celestia-appd start \
--home /opt \
--moniker core1 \
--p2p.persistent_peers 7b5ef9ef378a3907be7591863a566634d191b58b@core0:26656 \
--rpc.laddr tcp://0.0.0.0:26657
