# celestia-app

[![Go Reference](https://img.shields.io/badge/godoc-reference-blue.svg)](https://pkg.go.dev/github.com/celestiaorg/celestia-app)
[![mdBook Specs](https://img.shields.io/badge/mdBook-specs-blue)](https://celestiaorg.github.io/celestia-app/)
[![GitHub Release](https://img.shields.io/github/v/release/celestiaorg/celestia-app)](https://github.com/celestiaorg/celestia-app/releases/latest)
[![Go Report Card](https://goreportcard.com/badge/github.com/celestiaorg/celestia-app)](https://goreportcard.com/report/github.com/celestiaorg/celestia-app)
[![GitPOAP Badge](https://public-api.gitpoap.io/v1/repo/celestiaorg/celestia-app/badge)](https://www.gitpoap.io/gh/celestiaorg/celestia-app)

celestia-app is the software used by [validators](https://docs.celestia.org/nodes/validator-node) and [consensus nodes](https://docs.celestia.org/nodes/consensus-node) on the Celestia consensus network. celestia-app is a blockchain application built using parts of the Cosmos stack:

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

1. [Install Go](https://go.dev/doc/install) 1.23.1
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

See <https://docs.celestia.org/nodes/celestia-app> for more information.

## Usage

First, make sure that the [BBR](https://www.ietf.org/archive/id/draft-cardwell-iccrg-bbr-congestion-control-01.html) ("Bottleneck Bandwidth and Round-trip propagation time") congestion control algorithm is enabled in the
system's kernel. The result should contain `bbr`:

```sh
sysctl net.ipv4.tcp_congestion_control
```

If not, enable it on Linux by calling the `make use-bbr` or by running:

```sh
sudo modprobe tcp_bbr
net.core.default_qdisc=fq
net.ipv4.tcp_congestion_control=bbr
sudo sysctl -p
```

```sh
# Print help
celestia-appd --help
```

### Environment variables

| Variable        | Explanation                                                       | Default value                                            | Required |
|-----------------|-------------------------------------------------------------------|----------------------------------------------------------|----------|
| `CELESTIA_HOME` | Where the application directory (`.celestia-app`) should be saved | [User home directory](https://pkg.go.dev/os#UserHomeDir) | Optional |

### Create your own single node devnet

```sh
# Start a single node devnet
./scripts/single-node.sh

# Publish blob data to the local devnet
celestia-appd tx blob pay-for-blob 0x00010203040506070809 0x48656c6c6f2c20576f726c6421 \
	--chain-id private \
	--from validator \
	--keyring-backend test \
	--fees 21000utia \
	--yes
```

> [!NOTE]
> The celestia-appd binary doesn't support signing with Ledger hardware wallets on Windows and OpenBSD.

### Usage as a library

If import celestia-app as a Go module, you may need to add some Go module `replace` directives to avoid type incompatibilities. Please see the `replace` directive in [go.mod](./go.mod) for inspiration.

## Contributing

This repo attempts to conform to [conventional commits](https://www.conventionalcommits.org/en/v1.0.0/) so PR titles should ideally start with `fix:`, `feat:`, `build:`, `chore:`, `ci:`, `docs:`, `style:`, `refactor:`, `perf:`, or `test:` because this helps with semantic versioning and changelog generation. It is especially important to include an `!` (e.g. `feat!:`) if the PR includes a breaking change.

This repo contains multiple go modules. When using it, rename `go.work.example` to `go.work` and run `go work sync`.

### Tools

1. Install [golangci-lint](https://golangci-lint.run/welcome/install) 1.61.0
1. Install [markdownlint](https://github.com/DavidAnson/markdownlint) 0.39.0
1. Install [hadolint](https://github.com/hadolint/hadolint)
1. Install [yamllint](https://yamllint.readthedocs.io/en/stable/quickstart.html)
1. Install [markdown-link-check](https://github.com/tcort/markdown-link-check)
1. Install [goreleaser](https://goreleaser.com/install/)

### Helpful Commands

```sh
# Get more info on make commands.
make help

# Build the celestia-appd binary into the ./build directory.
make build

# Build and install the celestia-appd binary into the $GOPATH/bin directory.
make install

# Run tests
make test

# Format code with linters (this assumes golangci-lint and markdownlint are installed)
make fmt

# Regenerate Protobuf files (this assumes Docker is running)
make proto-gen
```

### Docs

Package-specific READMEs aim to explain implementation details for developers that are contributing to these packages. The [specs](https://celestiaorg.github.io/celestia-app/) aim to explain the protocol as a whole for developers building on top of Celestia.

## Audits

| Date       | Auditor                                       | Version                                                                             | Report                                                        |
|------------|-----------------------------------------------|-------------------------------------------------------------------------------------|---------------------------------------------------------------|
| 2023/9/15  | [Informal Systems](https://informal.systems/) | [v1.0.0-rc6](https://github.com/celestiaorg/celestia-app/releases/tag/v1.0.0-rc6)   | [informal-systems.pdf](docs/audit/informal-systems.pdf)       |
| 2023/10/17 | [Binary Builders](https://binary.builders/)   | [v1.0.0-rc10](https://github.com/celestiaorg/celestia-app/releases/tag/v1.0.0-rc10) | [binary-builders.pdf](docs/audit/binary-builders.pdf)         |
| 2024/7/1   | [Informal Systems](https://informal.systems/) | [v2.0.0-rc1](https://github.com/celestiaorg/celestia-app/releases/tag/v2.0.0-rc1)   | [informal-systems-v2.pdf](docs/audit/informal-systems-v2.pdf) |
