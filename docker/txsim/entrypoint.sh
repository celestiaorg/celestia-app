#!/bin/bash
set -Ee -o pipefail

echo "Starting txsim with command:"
echo "/bin/txsim $*"
echo ""

exec /bin/txsim "$@"
