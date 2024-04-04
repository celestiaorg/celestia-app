# celestia-app

[![Go Reference](https://img.shields.io/badge/godoc-reference-blue.svg)](https://pkg.go.dev/github.com/celestiaorg/celestia-app)
[![mdBook Specs](https://img.shields.io/badge/mdBook-specs-blue)](https://celestiaorg.github.io/celestia-app/)
[![GitHub Release](https://img.shields.io/github/v/release/celestiaorg/celestia-app)](https://github.com/celestiaorg/celestia-app/releases/latest)
[![Go Report Card](https://goreportcard.com/badge/github.com/celestiaorg/celestia-app)](https://goreportcard.com/report/github.com/celestiaorg/celestia-app)
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

### Source

1. [Install Go](https://go.dev/doc/install) 1.22.2
1. Clone this repo
1. Install the celestia-app CLI

    ```shell
    make install
    ```

### Prebuilt binary

If you'd rather not install from source, you can download a prebuilt binary from the [releases](https://github.com/celestiaorg/celestia-app/releases) page.

1. Navigate to the latest release on <https://github.com/celestiaorg/celestia-app/releases>.
1. Download the binary for your platform (e.g. `celestia-app_Linux_x86_64.tar.gz`) from the **Assets** section. Tip: if you're not sure what platform you're on, you can run `uname -a` and look for the operating system (e.g. `Linux`, `Darwin`) and architecture (e.g. `x86_64`, `arm64`).
1. Extract the archive

    ```shell
    tar -xvf celestia-app_Linux_x86_64.tar.gz
    ```

1. Verify the extracted binary works

    ```shell
    ./celestia-appd --help
    ```

1. [Optional] verify the prebuilt binary checksum. Download `checksums.txt` and then verify the checksum:

    ```shell
    sha256sum --ignore-missing --check checksums.txt
    ```

    You should see output like this:

    ```shell
    celestia-app_Linux_x86_64.tar.gz: OK
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
|-----------------|------------------------------------|----------------------------------------------------------|----------|
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

This repo contains multiple go modules. When using it, rename `go.work.example` to `go.work` and run `go work sync`.

### Tools

1. Install [golangci-lint](https://golangci-lint.run/welcome/install) 1.57.0
1. Install [markdownlint](https://github.com/DavidAnson/markdownlint) 0.39.0
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

### Docs

Package-specific READMEs aim to explain implementation details for developers that are contributing to these packages. The [specs](https://celestiaorg.github.io/celestia-app/) aim to explain the protocol as a whole for developers building on top of Celestia.

- [pkg/wrapper](./pkg/wrapper/README.md)
- [x/blob](./x/blob/README.md)
- [x/blobstream](./x/blobstream/README.md)

## Audits

| Date       | Auditor                                       | Version                                                                             | Report                                                  |
|------------|-----------------------------------------------|-------------------------------------------------------------------------------------|---------------------------------------------------------|
| 2023/9/15  | [Informal Systems](https://informal.systems/) | [v1.0.0-rc6](https://github.com/celestiaorg/celestia-app/releases/tag/v1.0.0-rc6)   | [informal-systems.pdf](docs/audit/informal-systems.pdf) |
| 2023/10/17 | [Binary Builders](https://binary.builders/)   | [v1.0.0-rc10](https://github.com/celestiaorg/celestia-app/releases/tag/v1.0.0-rc10) | [binary-builders.pdf](docs/audit/binary-builders.pdf)   |

## Careers

We are hiring Go engineers! Join us in building the future of blockchain scaling and interoperability. [Apply here](https://jobs.lever.co/celestia).
