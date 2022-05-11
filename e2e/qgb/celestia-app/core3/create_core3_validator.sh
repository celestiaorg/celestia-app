# Eth private key: 2f7f6763500dfb48b710a4c5c6c6a487b8aa6c7bc8b8a9b637a23f651f1c9b51
celestia-appd tx staking create-validator \
 --amount=5000000000uceles \
 --pubkey=$(celestia-appd tendermint show-validator --home /opt) \
 --moniker=core3 \
 --chain-id="qgb-e2e" \
 --commission-rate=0.1 \
 --commission-max-rate=0.2 \
 --commission-max-change-rate=0.01 \
 --min-self-delegation=1000000 \
 --from=core3 \
 --keyring-backend=test \
 --orchestrator-address $(celestia-appd keys show core3 -a --keyring-backend="test" --home /opt) \
 --ethereum-address 0x3EE99606625E740D8b29C8570d855Eb387F3c790 \
 --home /opt \
 --broadcast-mode block \
 --yes
