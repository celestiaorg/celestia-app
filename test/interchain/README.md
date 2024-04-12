# Interchain

This folder contains tests that use [interchaintest](https://github.com/strangelove-ventures/interchaintest) to assert IBC features work as expected. Candidates for testing include:

1. Interchain Accounts (ICA)
1. Packet Forward Middleware (PFM)
1. Relayer Incentivization Middleware (RIM)

## Usage

Run the tests via

```bash
make test-interchain
```

## Contributing

If you have local modifications that you would like to test via interchaintest, you'll need to create a new Celestia Docker image with your modifications. CI should automatically create a Docker image and publish it to GHCR if you create a PR against celestia-app. If that doesn't work, you can manually create an image via:

```shell
# make local modifications and commit them
make build-ghcr-docker
make publish-ghcr-docker
```

After you have a new Docker image with your modifications, you must update the test to reference the new image (see `chainspec/celestia.go`).

## Troubleshooting

`interchaintest` issues can be difficult to debug. Here are a few tips that may help:

1. You can stop `interchaintest` from cleaning up the Docker containers it created by setting an environment variable:

    ```shell
    export KEEP_CONTAINERS=true
    ```

    See [this PR](https://github.com/strangelove-ventures/interchaintest/pull/725). It wasn't backported to the `v6` branch so if you want to use it, cherry-pick the commit locally and apply it to your local `interchaintest` fork and use Go mod replace to point interchaintest to your local fork. After you have that change, you can run commands on the docker containers to debug them. For example:

    ```shell
    docker exec -it gaia-val-0-TestICA gaiad tx interchain-accounts controller register connection-0 \
    --chain-id gaia \
    --node http://gaia-val-0-TestICA:26657 \
    --home /var/cosmos-chain/gaia-2 \
    --from TestICA-gaia-pez \
    --keyring-backend test \
    --fees 300000uatom \
    --gas 300000 \
    --yes
    ```
