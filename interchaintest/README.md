# Interchaintest

This folder contains tests that use [interchaintest](https://github.com/strangelove-ventures/interchaintest) to assert IBC features work as expected. Candidates for testing include:

1. Interchain Accounts (ICA)
1. Packet Forward Middleware (PFM)
1. Relayer Incentivization Middleware

## Usage

To run these tests locally, you must have a Docker image of Celestia.

[Optional] If you have local modifications, you can generate a new Docker image and publish it via:

```shell
# make local modifications and commit them
make build-ghcr-docker
make publish-ghcr-docker
```

Run the tests via

```bash
make test-interchain
```
