#!/bin/bash

# This script creates the necessary files before starting Celestia-appd

# only create the priv_validator_state.json if it doesn't exist and the command is start
if [[ $1 == "start" && ! -f ${CELESTIA_HOME}/data/priv_validator_state.json ]]
then
    mkdir ${CELESTIA_HOME}/data # it is alright if it fails, the script will continue executing
    cat <<EOF > ${CELESTIA_HOME}/data/priv_validator_state.json
{
  "height": "0",
  "round": 0,
  "step": 0
}
EOF
fi

/bin/celestia-appd --home ${CELESTIA_HOME} $@
