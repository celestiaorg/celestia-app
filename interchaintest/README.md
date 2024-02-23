# Interchaintest

This folder contains tests that use [interchaintest](https://github.com/strangelove-ventures/interchaintest) to assert IBC features work as expected. Candidates for testing include:

1. Interchain Accounts (ICA)
1. Packet Forward Middleware (PFM)
1. Relayer Incentivization Middleware

## Usage

To run these tests locally, you must have a Docker image of Celestia. Run `make build-docker` to create a celestia-appd docker image from the current branch.

```go
go test ./...
```
