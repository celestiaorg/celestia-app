# `x/mint`

## Abstract

celestia-app's `x/mint` is a fork of the Cosmos SDK [`x/mint`](https://github.com/cosmos/cosmos-sdk/tree/5cd0b2316a7103468af38eab5d886f9f069c9cd7/x/mint) module that makes some changes to the inflation mechanism. The changes were motivated by a desire for Celestia to have a pre-determined inflation schedule. See [ADR-019](../../docs/architecture/adr-019-strict-inflation-schedule.md) for more details.

### Inflation Schedule

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

- **Year** indicates the number of years elapsed since chain genesis.
- **Inflation (%)** indicates the percentage of the total supply that will be minted in the next year.

## Terms

- **Inflation Rate**: The percentage of the total supply that will be minted each year. The inflation rate is calculated once per year on the anniversary of chain genesis based on the number of years elapsed since genesis. The inflation rate is calculated as `InitialInflationRate * ((1 - DisinflationRate) ^ YearsSinceGenesis)`. See [./types/constants.go](./types/constants.go) for the constants used in this module.
- **Annual Provisions**: The total amount of tokens that will be minted each year. Annual provisions are calculated once per year on the anniversary of chain genesis based on the total supply and the inflation rate. Annual provisions are calculated as `TotalSupply * InflationRate`
- **Block Provision**: The amount of tokens that will be minted in the current block. Block provisions are calculated once per block based on the annual provisions and the number of nanoseconds elapsed between the current block and the previous block. Block provisions are calculated as `AnnualProvisions * (NanosecondsSincePreviousBlock / NanosecondsPerYear)`

## State

See [./types/minter.go](./types/minter.go) for the `Minter` struct which contains this module's state.

## State Transitions

The `Minter` struct is updated every block via `BeginBlocker`.

### Begin Block

See `BeginBlocker` in [./abci.go](./abci.go).

### Events

An event is emitted every block when a block provision is minted. See `mintBlockProvision` in [./abci.go](./abci.go).

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

## Genesis State

The genesis state is defined in [./types/genesis.go](./types/genesis.go).

## Params

All params have been removed from this module because they should not be modifiable via governance. The constants used in this module are defined in [./types/constants.go](./types/constants.go) and they are subject to change via hardforks.

## Tests

See [./test/mint_test.go](./test/mint_test.go) for an integration test suite for this module.

## Assumptions and Considerations

> For the Gregorian calendar, the average length of the calendar year (the mean year) across the complete leap cycle of 400 years is 365.2425 days (97 out of 400 years are leap years).

Source: <https://en.wikipedia.org/wiki/Year#Calendar_year>

This module assumes `DaysPerYear = 365.2425` so when modifying tests, developers must define durations based on this assumption because ordinary durations won't return the expected results. In other words:

```go
// oneYear is 31,556,952 seconds which will likely return expected results in tests
oneYear := time.Duration(minttypes.NanosecondsPerYear)

// oneYear is 31,536,000 seconds which will likely return unexpected results in tests
oneYear := time.Hour * 24 * 365
```

### Security

Q: Can validators manipulate the amount of tokens minted due to inflation?

A:

This x/mint module calculates block provisions based on block timestamps so it is important to understand how block timestamps work in CometBFT. In CometBFT, block timestamps are monotonically increasing. A block's timestamp is the median of the `Vote.Time` fields from vote messages, where each vote's timestamp is weighted based on the voting power of the validator that cast it.

Based on [BFT time](https://docs.cometbft.com/v0.34/spec/consensus/bft-time), a block's timestamp is manipulable by malicious validators if they control > 1/3 of the total voting power. Consequently, if malicious validators control > 1/3 of the total voting power, they could manipulate a block's timestamp to some arbitrary value (e.g. one year in the future) effectively advancing the inflation schedule by a year.

It is worth noting that in the scenario above, a CometBFT light client will reject a block if the block's timestamp is > 10 seconds ahead of the light client's current time. See [`verifyNewHeaderAndVals`](https://github.com/celestiaorg/celestia-core/blob/c6954760907680ab3f492f518d58d3d90237bed2/light/verifier.go#L176-L181) and [`defaultMaxClockDrift`](https://github.com/celestiaorg/celestia-core/blob/d02553fdba2720f0314f2f6451a5d50b6755e62c/light/client.go#L34-L38). Therefore, it seems infeasible for a malicious validator set with > 1/3 total voting power to manipulate the inflation schedule by more than 10 seconds.

## Implementation

See [x/mint](../../x/mint) for the implementation of this module.

## References

1. [ADR-019](../../docs/architecture/adr-019-strict-inflation-schedule.md)
