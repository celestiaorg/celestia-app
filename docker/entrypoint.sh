#!/bin/bash

# This script creates the necessary files before starting Celestia-appd

# only create the priv_validator_state.json if it doesn't exist and the command is start
if [[ $1 == "start" && ! -f ${CELESTIA_HOME}/data/priv_validator_state.json ]]
then
    mkdir -p ${CELESTIA_HOME}/data
    cat <<EOF > ${CELESTIA_HOME}/data/priv_validator_state.json
{
  "height": "0",
  "round": 0,
  "step": 0
}
EOF
fi

echo "Starting celestia-appd with command:"
echo "/bin/celestia-appd $@"
echo ""

/bin/celestia-appd $@
