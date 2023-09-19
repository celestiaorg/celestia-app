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

1. [Install Go](https://go.dev/doc/install) 1.21.1
1. Clone this repo
1. Install the celestia-app CLI

    ```shell
    make install
    ```

### Ledger Support

Ledger is not supported on Windows and OpenBSD.

## Usage

```sh
# Print help
celestia-appd --help
```

### Environment variables

| Variable        | Explanation                        | Default value                                            | Required |
| --------------- | ---------------------------------- | -------------------------------------------------------- | -------- |
| `CELESTIA_HOME` | Home directory for the application | User home dir. [Ref](https://pkg.go.dev/os#UserHomeDir). | Optional |

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

This repo attempts to conform to [conventional commits](https://www.conventionalcommits.org/en/v1.0.0/) so PR titles should ideally start with `fix:`, `feat:`, `build:`, `chore:`, `ci:`, `docs:`, `style:`, `refactor:`, `perf:`, or `test:` because this helps with semantic versioning and changelog generation. It is especially important to include an `!` (e.g. `feat!:`) if the PR includes a breaking change.

### Tools

1. Install [golangci-lint](https://golangci-lint.run/usage/install/)
1. Install [markdownlint](https://github.com/DavidAnson/markdownlint)
1. Install [hadolint](https://github.com/hadolint/hadolint)
1. Install [yamllint](https://yamllint.readthedocs.io/en/stable/quickstart.html)
1. Install [markdown-link-check](https://github.com/tcort/markdown-link-check)
1. Install [goreleaser](https://goreleaser.com/install/)

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

# Build binaries with goreleaser
make goreleaser-build
```

### Publishing a Release

> **NOTE** Due to `goreleaser`'s CGO limitations, cross-compiling the binary does not work. So the binaries must be built on the target platform. This means that the release process must be done on a Linux amd64 machine.

To generate the binaries for the Github release, you can run the following command:

```sh
make goreleaser-release
```

This will generate the binaries as defined in `.goreleaser.yaml` and put them in `build/goreleaser` like so:

```sh
build
└── goreleaser
    ├── CHANGELOG.md
    ├── artifacts.json
    ├── celestia-app_Linux_x86_64.tar.gz
    ├── celestia-app_linux_amd64_v1
    │   └── celestia-appd
    ├── checksums.txt
    ├── config.yaml
    └── metadata.json
```

For the Github release, you just need to upload the `checksums.txt` and `celestia-app_Linux_x86_64.tar.gz` files.

### Docs

Package-specific READMEs aim to explain implementation details for developers that are contributing to these packages. The [specs](https://celestiaorg.github.io/celestia-app/) aim to explain the protocol as a whole for developers building on top of Celestia.

- [pkg/shares](./pkg/shares/README.md)
- [pkg/wrapper](./pkg/wrapper/README.md)
- [x/blob](./x/blob/README.md)
- [x/qgb](./x/qgb/README.md)

## Audits

[Informal Systems](https://informal.systems/) audited celestia-app [v1.0.0-rc6](https://github.com/celestiaorg/celestia-app/releases/tag/v1.0.0-rc6) in Q3 of 2023. See [audit/informal-systems.pdf](audit/informal-systems.pdf) for the full report.

## Careers

We are hiring Go engineers! Join us in building the future of blockchain scaling and interoperability. [Apply here](https://jobs.lever.co/celestia).
