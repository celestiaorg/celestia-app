#!/bin/sh

# This script initializes a consensus node on Mainnet Beta with state sync enabled.
# It is designed to run inside a Docker container.

# Stop script execution if an error is encountered
set -o errexit
# Stop script execution if an undefined variable is used
set -o nounset

CHAIN_ID="celestia"
NODE_NAME="node-name"
SEEDS="e6116822e1a5e283d8a85d3ec38f4d232274eaf3@consensus-full-seed-1.celestia-bootstrap.net:26656,cf7ac8b19ff56a9d47c75551bd4864883d1e24b5@consensus-full-seed-2.celestia-bootstrap.net:26656,12ad7c73c7e1f2460941326937a039139aa78884@celestia-mainnet-seed.itrocket.net:40656,400f3d9e30b69e78a7fb891f60d76fa3c73f0ecc@celestia.rpc.kjnodes.com:12059,59df4b3832446cd0f9c369da01f2aa5fe9647248@135.181.220.61:11756,86bd5cb6e762f673f1706e5889e039d5406b4b90@seed.celestia.node75.org:20356,ebc272824924ea1a27ea3183dd0b9ba713494f83@celestia-mainnet-seed.autostake.com:27206,9b1d22c3a78487d1a664a4b6a331fce527d14fb4@seed.celestia.mainnet.dteam.tech:27656,9aa8a73ea9364aa3cf7806d4dd25b6aed88d8152@celestia.seed.mzonder.com:13156,6f6a3a908634b79b6fe7c4988efec2553f188234@celestia.seed.nodeshub.online:11656,e8657b97bcfcf7e522f2481f17358c4273ee0d55@185.144.99.12:26656,2030b8d022cd3c65b6f267943df82d69c3e6ba64@celestia-rpc.tienthuattoan.com:26656,20e1000e88125698264454a884812746c2eb4807@seeds.lavenderfive.com:11656,0c8ec01f1c37734274e7ac2f91021a55194bb0bb@65.109.26.242:11656,edc6bc6ee3c37a698225e17bd4b8c687ee05f977@celestia-seed.easy2stake.com:26756"
PEERS="aa15a9c698773412b13054b992bafc1554cfbbd7@193.35.57.185:11656,d535cbf8d0efd9100649aa3f53cb5cbab33ef2d6@celestia-mainnet-rpc.itrocket.net:26656,d9bfa29e0cf9c4ce0cc9c26d98e5d97228f93b0b@celestia.rpc.kjnodes.com:12056,acca7837e4eb5f9dc7f5a94ed1d82edda6931ff8@135.181.246.172:26656,d00942da93f790767d515b0ae2fd700272b0147c@141.98.217.124:26656,a02a5bcc78a33526300f7550f552fcd1fd133db7@141.98.217.135:26656,79b10d69bfa65b070d18d8896864e880fcfa4375@103.219.169.97:43656,f12493a5a69bac967cd9bd04b32589bdbe954ee7@136.243.94.113:26666,dafdad7fd23dfa60b061a817587df24ab88ab910@220.88.50.228:26656,77a32a64b073b1e5bcee3bfcfc9be0bca23bcb07@185.191.117.91:26656,7eb7dd953738cf966e0faad927821be980b101ab@95.217.192.250:26616,6f7ac7b93950aa548d2eeac482d4262659cacd24@176.9.124.52:11656,ab1d5930fd1550d03dec8669a88cf260c156c455@91.148.167.131:26656,9793f589efc28c84568752ebfd1f7803cdcba511@148.72.141.192:26656,6662a92fd5490191a83a84f969df3c2b4071a9ac@88.211.219.55:26656,9d907b8aed06cbbb96bfdd3fffeb823a7544aa9a@148.72.141.245:26656,47e521b6089caf796dfa9c0d2423f66a4bd28f6e@185.44.207.247:26656,3666a13ae086942cf6cda89b07b85491b5214669@65.21.227.52:26656,2a1dac5970f171d2849302211d0dfdaeef74e0b0@135.181.117.37:56656,d272293cec9585b414566dc2943675b5021d63fd@88.198.27.51:56656"
CELESTIA_APP_HOME="${CELESTIA_APP_HOME:-/home/celestia/.celestia-app}"
RPC="https://celestia-rpc.polkachu.com:443"

echo "celestia-app home: ${CELESTIA_APP_HOME}"
echo "Initializing celestia-app for Mainnet..."
echo ""

# Initialize config files if they don't exist
if [ ! -f "${CELESTIA_APP_HOME}/config/config.toml" ]; then
    echo "Initializing config files..."
    if ! celestia-appd init ${NODE_NAME} --chain-id ${CHAIN_ID} --home ${CELESTIA_APP_HOME} > /dev/null 2>&1; then
        echo "ERROR: Failed to initialize config files!"
        exit 1
    fi
fi

echo "Setting seeds in config.toml..."
sed -i.bak -e "s/^seeds *=.*/seeds = \"$SEEDS\"/" ${CELESTIA_APP_HOME}/config/config.toml

echo "Setting persistent peers in config.toml..."
sed -i.bak -e "/^\[p2p\]/,/^\[/{s/^[[:space:]]*persistent_peers *=.*/persistent_peers = \"$PEERS\"/}" ${CELESTIA_APP_HOME}/config/config.toml

echo "Fetching state sync parameters..."
LATEST_HEIGHT=$(curl -s $RPC/block | jq -r .result.block.header.height)
if [ -z "$LATEST_HEIGHT" ] || [ "$LATEST_HEIGHT" = "null" ]; then
    echo "ERROR: Failed to fetch latest block height from RPC!"
    exit 1
fi
BLOCK_HEIGHT=$((LATEST_HEIGHT - 2000))
TRUST_HASH=$(curl -s "$RPC/block?height=$BLOCK_HEIGHT" | jq -r .result.block_id.hash)
if [ -z "$TRUST_HASH" ] || [ "$TRUST_HASH" = "null" ]; then
    echo "ERROR: Failed to fetch trust hash from RPC!"
    exit 1
fi

echo "Block height: $BLOCK_HEIGHT"
echo "Trust hash: $TRUST_HASH"
echo "Enabling state sync in config.toml..."
sed -i.bak -E "s|^(enable[[:space:]]+=[[:space:]]+).*$|\1true| ; \
s|^(rpc_servers[[:space:]]+=[[:space:]]+).*$|\1\"$RPC,$RPC\"| ; \
s|^(trust_height[[:space:]]+=[[:space:]]+).*$|\1$BLOCK_HEIGHT| ; \
s|^(trust_hash[[:space:]]+=[[:space:]]+).*$|\1\"$TRUST_HASH\"|" ${CELESTIA_APP_HOME}/config/config.toml

echo "Enabling Prometheus metrics in config.toml..."
# Check if [instrumentation] section exists
if grep -q "^\[instrumentation\]" ${CELESTIA_APP_HOME}/config/config.toml; then
    # Update existing prometheus settings
    sed -i.bak -E "s|^(prometheus[[:space:]]+=[[:space:]]+).*$|\1true|" ${CELESTIA_APP_HOME}/config/config.toml
    sed -i.bak -E "s|^(prometheus_listen_addr[[:space:]]+=[[:space:]]+).*$|\1\":26660\"|" ${CELESTIA_APP_HOME}/config/config.toml
else
    # Add [instrumentation] section if it doesn't exist
    echo "" >> ${CELESTIA_APP_HOME}/config/config.toml
    echo "#######################################################" >> ${CELESTIA_APP_HOME}/config/config.toml
    echo "###       Instrumentation Configuration Options     ###" >> ${CELESTIA_APP_HOME}/config/config.toml
    echo "#######################################################" >> ${CELESTIA_APP_HOME}/config/config.toml
    echo "[instrumentation]" >> ${CELESTIA_APP_HOME}/config/config.toml
    echo "prometheus = true" >> ${CELESTIA_APP_HOME}/config/config.toml
    echo "prometheus_listen_addr = \":26660\"" >> ${CELESTIA_APP_HOME}/config/config.toml
fi

echo "Downloading genesis file..."
if ! celestia-appd download-genesis ${CHAIN_ID} --home ${CELESTIA_APP_HOME}; then
    echo "ERROR: Failed to download genesis file!"
    exit 1
fi

# Verify genesis file exists
if [ ! -f "${CELESTIA_APP_HOME}/config/genesis.json" ]; then
    echo "ERROR: Genesis file was not downloaded successfully!"
    exit 1
fi

echo "Initialization complete!"
echo ""
