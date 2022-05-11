# Eth private key: 6adac8b5de0ba702ec8feab6d386a0c7334c6720b9174c02333700d431057af8
celestia-appd tx staking create-validator \
 --amount=5000000000uceles \
 --pubkey=$(celestia-appd tendermint show-validator --home /opt) \
 --moniker=core2 \
 --chain-id="qgb-e2e" \
 --commission-rate=0.1 \
 --commission-max-rate=0.2 \
 --commission-max-change-rate=0.01 \
 --min-self-delegation=1000000 \
 --from=core2 \
 --keyring-backend=test \
 --orchestrator-address $(celestia-appd keys show core2 -a --keyring-backend="test" --home /opt) \
 --ethereum-address 0x3d22f0C38251ebdBE92e14BBF1bd2067F1C3b7D7 \
 --home /opt \
 --broadcast-mode block \
 --yes
