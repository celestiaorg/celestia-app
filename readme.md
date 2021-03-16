# lazyledger-app

**lazyledger-app** is a blockchain application built using Cosmos SDK and [lazyledger-core](https://github.com/lazyledger/lazyledger-core) in place of tendermint. Disclaimer: **WIP**

## Install
```
make install
```

### Create your own single node devnet
```
lazyledger-appd init test --chain-id test
lazyledger-appd keys add user1
lazyledger-appd add-genesis-account <address from above command> 10000000stake,1000token
lazyledger-appd gentx user1 100000stake --chain-id test
lazyledger-appd collect-gentxs
lazyledger-appd start
```
## Usage
Use the `lazyledger-appd` daemon cli command to post data to a local devent. 
  
```lazyledger-appd tx lazyledgerapp payForMessage [hexNamespace] [hexMessage] [flags]```
