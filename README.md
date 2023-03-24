# celestia-app

[![Go Reference](https://img.shields.io/badge/godoc-reference-blue.svg)](https://pkg.go.dev/github.com/celestiaorg/celestia-app)
[![mdBook Specs](https://img.shields.io/badge/mdBook-specs-blue)](https://celestiaorg.github.io/celestia-app/)
[![GitHub Release](https://img.shields.io/github/v/release/celestiaorg/celestia-app)](https://github.com/celestiaorg/celestia-app/releases/latest)
[![Go Report Card](https://goreportcard.com/badge/github.com/celestiaorg/celestia-app)](https://goreportcard.com/report/github.com/celestiaorg/celestia-app)
[![Lint](https://github.com/celestiaorg/celestia-app/actions/workflows/lint.yml/badge.svg)](https://github.com/celestiaorg/celestia-app/actions/workflows/lint.yml)
[![Tests / Code Coverage](https://github.com/celestiaorg/celestia-app/actions/workflows/test.yml/badge.svg)](https://github.com/celestiaorg/celestia-app/actions/workflows/test.yml)
[![codecov](https://codecov.io/gh/celestiaorg/celestia-app/branch/main/graph/badge.svg?token=CWGA4RLDS9)](https://app.codecov.io/gh/celestiaorg/celestia-app/tree/main)
[![GitPOAP Badge](https://public-api.gitpoap.io/v1/repo/celestiaorg/celestia-app/badge)](https://www.gitpoap.io/gh/celestiaorg/celestia-app)

**celestia-app** is a blockchain application built using Cosmos SDK and [celestia-core](https://github.com/celestiaorg/celestia-core) in place of Tendermint.

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
                |  +-------------------------------+  |   celestia-core (fork of Tendermint Core)
                |  |                               |  |
                |  |           Networking          |  |
                |  |                               |  |
                v  +-------------------------------+  v
```

## Install

1. [Install Go](https://go.dev/doc/install) 1.18
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
# WARNING: this deletes config, data, and keyrings from previous local devnets
rm -r ~/.celestia-app

# Start a single node devnet
./scripts/single-node.sh

# Post data to the local devnet
celestia-appd tx blob PayForBlobs [hexNamespace] [hexBlob] [flags]
```

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

### Package-specific documentation

- [Shares](https://pkg.go.dev/github.com/celestiaorg/celestia-app/pkg/shares)

## Careers

We are hiring Go engineers! Join us in building the future of blockchain scaling and interoperability. [Apply here](https://jobs.lever.co/celestia).
