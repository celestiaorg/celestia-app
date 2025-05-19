#!/bin/sh

# This script reproduces the error in https://github.com/celestiaorg/celestia-app/issues/4832.

# Stop script execution if an error is encountered
set -o errexit
# Stop script execution if an undefined variable is used
set -o nounset

# Create a new user without home directory
sudo useradd --no-create-home newuser

# Create a directory owned by root
sudo mkdir -p /tmp/celestia-test
sudo chown root:root /tmp/celestia-test

# Switch to the new user and try to run celestia-appd
sudo -u newuser celestia-appd start --home /tmp/celestia-test
