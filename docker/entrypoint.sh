#!/bin/bash

# This script prepares the necessary state files before starting the celestia-appd application.

# Ensure CELESTIA_APP_HOME is set for safety, or assign a default path if required.
if [ -z "$CELESTIA_APP_HOME" ]; then
    echo "ERROR: The CELESTIA_APP_HOME environment variable is not set." >&2
    exit 1
fi

# The state file path.
PRIV_VAL_STATE_FILE="${CELESTIA_APP_HOME}/data/priv_validator_state.json"

# Check if the first argument is "start" AND the state file does not exist.
if [[ "$1" == "start" && ! -f "$PRIV_VAL_STATE_FILE" ]]
then
    echo "INFO: Creating initial '$PRIV_VAL_STATE_FILE' file as it does not exist."

    # Create the data directory if it doesn't already exist.
    mkdir -p "${CELESTIA_APP_HOME}/data"

    # Create the initial priv_validator_state.json with height 0, round 0, step 0.
    cat <<EOF > "$PRIV_VAL_STATE_FILE"
{
  "height": "0",
  "round": 0,
  "step": 0
}
EOF
else
    # Log if the condition is not met (e.g., if it's not 'start' or the file already exists).
    echo "INFO: Skipping '$PRIV_VAL_STATE_FILE' creation (either file exists or command is not 'start')."
fi

# Inform the user about the command being executed.
echo "INFO: Executing command: /bin/celestia-appd $@"
echo "----------------------------------------------"

# Execute celestia-appd with all passed arguments.
# Using 'exec' replaces the current shell process with the application, saving memory.
# Using "$@" ensures that all arguments are passed correctly, preserving spaces.
exec /bin/celestia-appd "$@"
