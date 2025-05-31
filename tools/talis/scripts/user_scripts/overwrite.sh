#!/bin/bash

# stop the existing network
source kill_sessions.sh txsim
source kill_sessions.sh app
source kill_sessions.sh traces
source kill_port.sh

# regenerate the payload after compiling the latest version of the generation code.
CHAIN_ID=$1
# cd ./cmd/remote
# go install
# cd ../..

remote payload -p ./payload -v ./payload/validators.json -c $CHAIN_ID

# send the payload to the droplets, this also grabs the latest locally compiled
# version of app and txsim 
source send_payload.sh

# send the signal to restart the network
source remote_trigger.sh app debug_install.sh