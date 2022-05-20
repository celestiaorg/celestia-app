# celestia-app

**celestia-app** is a blockchain application built using Cosmos SDK and [celestia-core](https://github.com/celestiaorg/celestia-core) in place of tendermint. Disclaimer: **WIP**

## Install
```
make install
```

### Create your own single node devnet
```
celestia-appd init test --chain-id test
celestia-appd keys add user1
celestia-appd add-genesis-account <address from above command> 10000000utia,1000token
celestia-appd gentx user1 1000000utia --chain-id test
celestia-appd collect-gentxs
celestia-appd start
```
## Usage
Use the `celestia-appd` daemon cli command to post data to a local devent. 
  
```celestia-appd tx payment payForData [hexNamespace] [hexMessage] [flags]```
