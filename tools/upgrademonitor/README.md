# Upgrade Monitor

Upgrade monitor is a stand-alone binary that monitors that status of upgrades on a Celestia network.

## Build from source

```shell
go build .
```

## Prerequisites

1. Determine which network you would like to monitor upgrades on.
1. Get a GRPC endpoint for a consensus node on that network.

**NOTE** This tool only works for consensus nodes that are running app version >= 2. At the time of writing, Celestia mainnet and testnets are on app version 1 so upgrademonitor can only be used on a local devnet.

## Usage

1. In one terminal tab, start a local devnet

    ```shell
    cd celestia-app

    # Upgrade monitor can only be run on app version >= 2 so checkout main and install celestia-appd.
    git checkout main
    make install

    # This will start a GRPC server at 0.0.0.0:9000
    ./scripts/single-node.sh
    ```

1. In a new terminal tab, prepare the try upgrade transaction

    ```shell
    # Export environment variables. Modify these based on your needs.
    export CHAIN_ID="private"
    export KEY_NAME="validator"
    export KEYRING_BACKEND="test"
    export FROM=$(celestia-appd keys show $KEY_NAME --address --keyring-backend $KEYRING_BACKEND)

    # Prepare the unsigned transaction
    celestia-appd tx upgrade try-upgrade --from $FROM --fees 420utia --generate-only --keyring-backend $KEYRING_BACKEND > unsigned_tx.json

    # Verify the unsigned transactino
    cat unsigned_tx.json

    # Sign the unsigned transaction.
    # For some reason, this command sends output to STDERR instead of STDOUT so redirect both to a file.
    celestia-appd tx sign unsigned_tx.json --chain-id $CHAIN_ID --keyring-backend $KEYRING_BACKEND --from $FROM &> signed_tx.json
    ```

1. Run the upgrademonitor.

    ```shell
    cd celestia-app/tools/upgrademonitor

    # Build the binary
    go build .

    # Run the binary without auto publishing
    ./upgrademonitor

    # Run the binary with auto publishing
    ./upgrademonitor --auto-publish signed_tx.json
    ```
