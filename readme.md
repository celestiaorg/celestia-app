# lazyledger-app

**lazyledgerapp** is a blockchain application built using Cosmos SDK and [lazyledger-core](https://github.com/lazyledger/lazyledger-core) in place of tendermint.

## Install
```
make install
```

## Usage
Use the `lazyledger-appd` daemon cli command to interact with a lazyledger devnet or testnet.

### Create your own single node devnet
```
lazyledger-appd init test --chain-id test
lazyledger-appd keys add user1
lazyledger-appd add-genesis-account <address from above command> 10000000stake,1000token
lazyledger-appd gentx user1 1000stake --chain-id test
lazyledger-appd collect-gentxs
lazyledger-appd start
```

