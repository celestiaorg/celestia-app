#!/bin/sh

set -o errexit -o nounset

HOME_DIR=$(mktemp -d -t "celestia_app_XXXXXXXXXXXXX")

echo "Home directory: ${HOME_DIR}"

CHAINID="private"

# Build genesis file incl account for passed address
coins="1000000000000000utia"
celestia-appd init $CHAINID --chain-id $CHAINID --home ${HOME_DIR}
celestia-appd keys add validator --keyring-backend="test" --home ${HOME_DIR}
celestia-appd add-genesis-account $(celestia-appd keys show validator -a --keyring-backend="test" --home ${HOME_DIR}) $coins --home ${HOME_DIR}
celestia-appd gentx validator 5000000000utia \
	--keyring-backend="test" \
	--chain-id $CHAINID \
	--home ${HOME_DIR} \
	--evm-address 0x966e6f22781EF6a6A82BBB4DB3df8E225DfD9488 # private key: da6ed55cb2894ac2c9c10209c09de8e8b9d109b910338d5bf3d747a7e1fc9eb9

celestia-appd collect-gentxs --home ${HOME_DIR}

# Set proper defaults and change ports
# If you encounter: `sed: -I or -i may not be used with stdin` on MacOS you can mitigate by installing gnu-sed
# https://gist.github.com/andre3k1/e3a1a7133fded5de5a9ee99c87c6fa0d?permalink_comment_id=3082272#gistcomment-3082272
sed -i'.bak' 's#"tcp://127.0.0.1:26657"#"tcp://0.0.0.0:26657"#g' ${HOME_DIR}/config/config.toml

# Start the celestia-app
celestia-appd start --home ${HOME_DIR}
