# celestia-app

[![Go Reference](https://img.shields.io/badge/godoc-reference-blue.svg)](https://pkg.go.dev/github.com/celestiaorg/celestia-app)
[![GitHub Release](https://img.shields.io/github/v/release/celestiaorg/celestia-app)](https://github.com/celestiaorg/celestia-app/releases/latest)
[![Go Report Card](https://goreportcard.com/badge/github.com/celestiaorg/celestia-app)](https://goreportcard.com/report/github.com/celestiaorg/celestia-app)
[![Lint](https://github.com/celestiaorg/celestia-app/actions/workflows/lint.yml/badge.svg)](https://github.com/celestiaorg/celestia-app/actions/workflows/lint.yml)
[![Tests / Code Coverage](https://github.com/celestiaorg/celestia-app/actions/workflows/test.yml/badge.svg)](https://github.com/celestiaorg/celestia-app/actions/workflows/test.yml)
[![codecov](https://codecov.io/gh/celestiaorg/celestia-app/branch/main/graph/badge.svg?token=CWGA4RLDS9)](https://codecov.io/gh/celestiaorg/celestia-app)

**celestia-app** is a blockchain application built using Cosmos SDK and [celestia-core](https://github.com/celestiaorg/celestia-core) in place of Tendermint.

## Install

```shell
make install
```

### Create your own single node devnet

```shell
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

## Careers

We are hiring Go engineers! Join us in building the future of blockchain scaling and interoperability. [Apply here](https://jobs.lever.co/celestia).
