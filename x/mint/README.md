# `x/mint`

`x/mint` is a fork of the Cosmos SDK `x/mint` module that makes some changes to the inflation mechanism. The changes were motivated by a desire for Celestia to have a predictable [inflation schedule](#inflation-schedule). See [ADR-019](../../docs/architecture/adr-019-strict-inflation-schedule.md) for more details.

## Concepts

- **Inflation Rate**: The percentage of the total supply that will be minted each year. The inflation rate is calculated once per year on the anniversary of chain genesis based on the number of years since genesis. The inflation rate is calculated as `InitialInflationRate * ((1 - DisinflationRate) ^ YearsSinceGenesis)`. See [./types/constants.go](./types/constants.go) for the constants used in this module.
- **Annual Provisions**: The total amount of tokens that will be minted each year. Annual provisions are calculated once per year on the anniversary of chain genesis based on the total supply and the inflation rate. Annual provisions are calculated as `TotalSupply * InflationRate`
- **Block Provision**: The amount of tokens that will be minted in the current block. Block provisions are calculated once per block based on the annual provisions and the number of seconds elapsed between the current block and the previous block. Block provisions are calculated as `AnnualProvisions * (NanoSecondsSincePreviousBlock / NanoSecondsPerYear)`

## Implementation Details

This module assumes `DaysPerYear = 365.2425` so when modifying tests, developers must define durations based on this assumption because ordinary durations won't return the expected results. In other words:

```go
// oneYear is 31,556,952 seconds which will likely return expected results in tests
oneYear := time.Duration(minttypes.NanosecondsPerYear)

// oneYear is 31,536,000 seconds which will likely return unexpected results in tests
oneYear := time.Hour * 24 * 365
```

## Inflation Schedule

| Year | Inflation (%)     |
|------|-------------------|
| 0    | 8.00              |
| 1    | 7.20              |
| 2    | 6.48              |
| 3    | 5.832             |
| 4    | 5.2488            |
| 5    | 4.72392           |
| 6    | 4.251528          |
| 7    | 3.8263752         |
| 8    | 3.44373768        |
| 9    | 3.099363912       |
| 10   | 2.7894275208      |
| 11   | 2.51048476872     |
| 12   | 2.259436291848    |
| 13   | 2.0334926626632   |
| 14   | 1.83014339639688  |
| 15   | 1.647129056757192 |
| 16   | 1.50              |
| 17   | 1.50              |
| 18   | 1.50              |
| 19   | 1.50              |
| 20   | 1.50              |

## State

See [./types/minter.go](./types/minter.go) for the `Minter` struct which contains all of the state of this module. The

## State Transitions

The `Minter` struct is updated every block via `BeginBlocker`.

## Messages

N/A

## Begin Block

See `BeginBlocker` in [./abci.go](./abci.go).

## End Block

N/A

## Hooks

N/A

## Events

An event is emitted every time a block provision is minted. See `mintBlockProvision` in [./abci.go](./abci.go).

## Client

### CLI

```shell
$ celestia-appd query mint annual-provisions
80235005639941.760000000000000000
```

```shell
$ celestia-appd query mint genesis-time
2023-05-09 00:56:15.59304 +0000 UTC

```

```shell
$ celestia-appd query mint inflation
0.080000000000000000
```

## Params

All params have been removed from this module.

## Future Improvements

1. <https://github.com/celestiaorg/celestia-app/issues/1755>
1. <https://github.com/celestiaorg/celestia-app/issues/1757>
1. <https://github.com/celestiaorg/celestia-app/issues/1758>

## Tests

See [./test/mint_test.go](./test/mint_test.go) for an integration test suite for this module.

## References

1. [ADR-019](../../docs/architecture/adr-019-strict-inflation-schedule.md)
