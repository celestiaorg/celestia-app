#!/bin/bash

# This script creates the necessary files before starting celestia-appd

# only create the priv_validator_state.json if it doesn't exist and the command is start
if [[ $1 == "start" && ! -f ${CELESTIA_APP_HOME}/data/priv_validator_state.json ]]
then
    mkdir -p ${CELESTIA_APP_HOME}/data
    cat <<EOF > ${CELESTIA_APP_HOME}/data/priv_validator_state.json
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

exec /bin/celestia-appd $@
