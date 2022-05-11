# Eth private key: 002ad18ca3def673345897b063bfa98d829a4d812dbd07f1938676828a82c4f9
celestia-appd tx staking create-validator \
 --amount=5000000000uceles \
 --pubkey=$(celestia-appd tendermint show-validator --home /opt) \
 --moniker=core1 \
 --chain-id="qgb-e2e" \
 --commission-rate=0.1 \
 --commission-max-rate=0.2 \
 --commission-max-change-rate=0.01 \
 --min-self-delegation=1000000 \
 --from=core1 \
 --keyring-backend=test \
 --orchestrator-address $(celestia-appd keys show core1 -a --keyring-backend="test" --home /opt) \
 --ethereum-address 0x91DEd26b5f38B065FC0204c7929Da1b2A21877Ad \
 --home /opt \
 --broadcast-mode block \
 --yes
