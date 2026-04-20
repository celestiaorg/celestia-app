#!/bin/bash
# If an AWS instance-store NVMe is mounted at /mnt/data (set up by the
# cloud-init user-data in tools/talis/aws.go for i-family instances),
# symlink the celestia homes to it so pebble writes go to fast local
# NVMe instead of the small EBS root. CELES_HOME stays relative: every
# tool that runs here (celestia-appd, fibre, fibre-txsim, setup-fibre)
# resolves relative paths under $HOME, which is now transparently
# NVMe-backed through the symlink. Works on DO (no /mnt/data, plain
# $HOME dir) and AWS sizes without local NVMe (same fallback).
if mountpoint -q /mnt/data 2>/dev/null; then
  mkdir -p /mnt/data/.celestia-app /mnt/data/.celestia-fibre
  [ -L "$HOME/.celestia-app"   ] || rm -rf "$HOME/.celestia-app"
  [ -L "$HOME/.celestia-fibre" ] || rm -rf "$HOME/.celestia-fibre"
  ln -sfn /mnt/data/.celestia-app   "$HOME/.celestia-app"
  ln -sfn /mnt/data/.celestia-fibre "$HOME/.celestia-fibre"
fi
CELES_HOME=".celestia-app"
MONIKER="validator"
ARCHIVE_NAME="payload.tar.gz"

export DEBIAN_FRONTEND=noninteractive
apt-get update -y -o Dpkg::Options::="--force-confdef" -o Dpkg::Options::="--force-confold"
apt-get install git build-essential ufw curl jq chrony snapd btop nethogs unzip --yes -o Dpkg::Options::="--force-confdef" -o Dpkg::Options::="--force-confold"

ufw allow 26657/tcp
ufw allow 26656/tcp
ufw allow 26657/udp
ufw allow 26656/udp

systemctl enable chrony
systemctl start chrony

# Ensure the script is run as root
if [ "$(id -u)" -ne 0 ]; then
  echo "This script must be run as root. Please run with sudo or as root."
  exit 1
fi

# Load the BBR module
echo "Loading BBR module..."
modprobe tcp_bbr

# Verify if the BBR module is loaded
if lsmod | grep -q "tcp_bbr"; then
  echo "BBR module loaded successfully."
else
  echo "Failed to load BBR module."
  exit 1
fi

# Add BBR to the list of available congestion control algorithms
echo "Updating sysctl settings..."
sysctl -w net.core.default_qdisc=fq
sysctl -w net.ipv4.tcp_congestion_control=bbr

# Enable MPTCP
sysctl -w net.mptcp.enabled=1

# Set the path manager to ndiffports
sysctl -w net.mptcp.mptcp_path_manager=ndiffports

# Specify the number of subflows
SUBFLOWS=16
sysctl -w net.mptcp.mptcp_ndiffports=$SUBFLOWS

# Make the changes persistent across reboots
echo "Making changes persistent..."
echo "net.core.default_qdisc=fq" >> /etc/sysctl.conf
echo "net.ipv4.tcp_congestion_control=bbr" >> /etc/sysctl.conf

#Verify the current TCP congestion control algorithm
current_algo=$(sysctl net.ipv4.tcp_congestion_control | awk '{print $3}')
if [ "$current_algo" == "bbr" ]; then
  echo "Successfully switched to BBR congestion control algorithm."
else
  echo "Failed to switch to BBR. Current algorithm is $current_algo."
  exit 1
fi

echo "Script completed successfully."

tar -xzf /root/$ARCHIVE_NAME -C /root/

source ./vars.sh

sudo snap install go --channel=1.26/stable --classic

echo 'export GOPATH="$HOME/go"' >> ~/.profile
echo 'export GOBIN="$GOPATH/bin"' >> ~/.profile
echo 'export PATH="$GOBIN:$PATH"' >> ~/.profile
source ~/.profile

cd $HOME

# Get the hostname
hostname=$(hostname)

# Parse the first part of the hostname
parsed_hostname=$(echo $hostname | awk -F'-' '{print $1 "-" $2}')

cp payload/build/celestia-appd /bin/celestia-appd
cp payload/build/txsim /bin/txsim
cp payload/build/latency-monitor /bin/latency-monitor
cp payload/build/fibre /bin/fibre
cp payload/build/fibre-txsim /bin/fibre-txsim

cd $HOME

# Clean slate. On AWS NVMe hosts the rm also removes the symlink we
# set up above — fine, we re-establish it immediately so `celestia-appd
# init` below recreates the state directory on /mnt/data through it.
rm -rf .celestia-app/
if mountpoint -q /mnt/data 2>/dev/null; then
  mkdir -p /mnt/data/.celestia-app
  ln -sfn /mnt/data/.celestia-app "$HOME/.celestia-app"
fi

celestia-appd config chain-id $CHAIN_ID

celestia-appd init --chain-id=$CHAIN_ID --home $CELES_HOME $MONIKER

mv payload/$parsed_hostname/node_key.json $HOME/$CELES_HOME/config/node_key.json

mv payload/$parsed_hostname/priv_validator_key.json $HOME/$CELES_HOME/config/priv_validator_key.json

mv payload/$parsed_hostname/priv_validator_state.json $HOME/$CELES_HOME/data/priv_validator_state.json

cp payload/genesis.json $HOME/$CELES_HOME/config/genesis.json

cp payload/addrbook.json $HOME/$CELES_HOME/config/addrbook.json

mv payload/$parsed_hostname/app.toml $HOME/$CELES_HOME/config/app.toml

mv payload/$parsed_hostname/config.toml $HOME/$CELES_HOME/config/config.toml

cp -r payload/$parsed_hostname/keyring-test $HOME/$CELES_HOME

# run txsim script which starts a sleep timer and txsim in a different tmux session
source payload/txsim.sh

# Get the hostname of the machine
HOSTNAME=$(hostname)

# Base command
COMMAND="celestia-appd start"

# Define log file path
LOG_FILE="/root/logs"

# Execute the command and redirect output to the log file
eval $COMMAND 2>&1 | tee -a "$LOG_FILE"
