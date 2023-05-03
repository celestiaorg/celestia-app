# celestia-app

[![Go Reference](https://img.shields.io/badge/godoc-reference-blue.svg)](https://pkg.go.dev/github.com/celestiaorg/celestia-app)
[![mdBook Specs](https://img.shields.io/badge/mdBook-specs-blue)](https://celestiaorg.github.io/celestia-app/)
[![GitHub Release](https://img.shields.io/github/v/release/celestiaorg/celestia-app)](https://github.com/celestiaorg/celestia-app/releases/latest)
[![Go Report Card](https://goreportcard.com/badge/github.com/celestiaorg/celestia-app)](https://goreportcard.com/report/github.com/celestiaorg/celestia-app)
[![Lint](https://github.com/celestiaorg/celestia-app/actions/workflows/lint.yml/badge.svg)](https://github.com/celestiaorg/celestia-app/actions/workflows/lint.yml)
[![Tests / Code Coverage](https://github.com/celestiaorg/celestia-app/actions/workflows/test.yml/badge.svg)](https://github.com/celestiaorg/celestia-app/actions/workflows/test.yml)
[![codecov](https://codecov.io/gh/celestiaorg/celestia-app/branch/main/graph/badge.svg?token=CWGA4RLDS9)](https://app.codecov.io/gh/celestiaorg/celestia-app/tree/main)
[![GitPOAP Badge](https://public-api.gitpoap.io/v1/repo/celestiaorg/celestia-app/badge)](https://www.gitpoap.io/gh/celestiaorg/celestia-app)

celestia-app is a blockchain application built using parts of the Cosmos stack. celestia-app uses

- [celestiaorg/cosmos-sdk](https://github.com/celestiaorg/cosmos-sdk) a fork of [cosmos/cosmos-sdk](https://github.com/cosmos/cosmos-sdk)
- [celestiaorg/celestia-core](https://github.com/celestiaorg/celestia-core) a fork of [cometbft/cometbft](https://github.com/cometbft/cometbft)

## Diagram

```ascii
                ^  +-------------------------------+  ^
                |  |                               |  |
                |  |  State-machine = Application  |  |
                |  |                               |  |   celestia-app (built with Cosmos SDK)
                |  |            ^      +           |  |
                |  +----------- | ABCI | ----------+  v
Celestia        |  |            +      v           |  ^
validator or    |  |                               |  |
full consensus  |  |           Consensus           |  |
node            |  |                               |  |
                |  +-------------------------------+  |   celestia-core (fork of CometBFT)
                |  |                               |  |
                |  |           Networking          |  |
                |  |                               |  |
                v  +-------------------------------+  v
```

## Install

1. [Install Go](https://go.dev/doc/install) 1.19
1. Clone this repo
1. Install the celestia-app CLI

    ```shell
    make install
    ```

## Usage

```sh
# Print help
celestia-appd --help
```

### Environment variables

| Variable        | Explanation                        | Default value                                            | Required |
| --------------- | ---------------------------------- | -------------------------------------------------------- | -------- |
| `CELESITA_HOME` | Home directory for the application | User home dir. [Ref](https://pkg.go.dev/os#UserHomeDir). | Optional |

### Create your own single node devnet

```sh

# Start a single node devnet using the pre-installed celestia app
./scripts/single-node.sh

# Build and start a single node devnet
./scripts/build-run-single-node.sh

# Post data to the local devnet
celestia-appd tx blob PayForBlobs [hexNamespace] [hexBlob] [flags]
```

**Note:** please note that the `./scripts/` commands above, created a random `tmp` directory and keeps all data/configs there.

<!-- markdown-link-check-disable -->
<!-- markdown-link encounters an HTTP 503 on this link even though it works. -->
<!-- See https://github.com/celestiaorg/celestia-app/actions/runs/3296219513/jobs/5439416229#step:4:185 -->
See <https://docs.celestia.org/category/celestia-app> for more information
<!-- markdown-link-check-enable -->

## Contributing

### Tools

1. Install [golangci-lint](https://golangci-lint.run/usage/install/)
1. Install [markdownlint](https://github.com/DavidAnson/markdownlint)
1. Install [hadolint](https://github.com/hadolint/hadolint)
1. Install [yamllint](https://yamllint.readthedocs.io/en/stable/quickstart.html)

### Helpful Commands

```sh
# Build a new celestia-app binary and output to build/celestia-appd
make build

# Run tests
make test

# Format code with linters (this assumes golangci-lint and markdownlint are installed)
make fmt

# Regenerate Protobuf files (this assumes Docker is running)
make proto-gen
```

### Docs

Package-specific docs aim to explain implementation details for developers that are contributing to these packages. The [specs](https://celestiaorg.github.io/celestia-app/) aim to explain the protocol as a whole for developers building on top of Celestia.

- [pkg/shares](https://pkg.go.dev/github.com/celestiaorg/celestia-app/pkg/shares)
- [pkg/wrapper](https://github.com/celestiaorg/celestia-app/blob/main/pkg/wrapper)
- [x/blob](https://github.com/celestiaorg/celestia-app/tree/main/x/blob)
- [x/qgb](https://github.com/celestiaorg/celestia-app/tree/main/x/qgb)

## Careers

We are hiring Go engineers! Join us in building the future of blockchain scaling and interoperability. [Apply here](https://jobs.lever.co/celestia).
