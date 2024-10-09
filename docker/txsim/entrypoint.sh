#!/bin/bash

echo "Starting txsim with command:"
echo "/bin/txsim $@"
echo ""

exec /bin/txsim $@
