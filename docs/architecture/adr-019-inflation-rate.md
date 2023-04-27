# ADR 019: Inflation rate

## Status

Proposed

## Changelog

- 2023/3/31: initial draft
- 2023/4/27: height-based vs time-based

## Context

Cosmos SDK chains that use the [x/mint](https://docs.cosmos.network/v0.46/modules/mint/) module have a flexible inflation rate that increases/decreases so that the total % of tokens bonded target some value.

In contrast to a flexible inflation rate, Celestia intends on having a predictable inflation rate with the following constants:

| Constant                   | Value (%) |
|----------------------------|-----------|
| Initial inflation          | 8.00      |
| Disinflation rate per year | 10.00     |
| Target inflation           | 1.50      |

When the target inflation is reached, it remains at that rate.
The table below depicts the inflation rate for the forseeable future:

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

## Design

In order to implement the inflation rate specified above, we need to make a few modifications to the existing `x/mint` module:

1. Instead of relying on a block height (i.e. `header.height`) based inflation schedule, we are considering a time-based (i.e. `header.time`) inflation schedule. Using a time-based schedule has one key advantages: since the block interval isn't fixed, individual validators are able to produce blocks at a rate (i.e. one block every 10 seconds) that differs from the network target (i.e. one block every 15 seconds). An inflation schedule based on block height needs to make an assumption on the number of blocks created per year so a large discrepancy between the expected and actual block interval will result in a discrepancy between target and actual inflation rate. A time-based schedule is potentially more robust against this attack because "the timestamp is equal to the weighted median of validators present in the last commit." [cometbft/specs](https://github.com/cometbft/cometbft/blob/c58597d656d5c816334aff9ea8e600bdbc534817/spec/core/data_structures.md?plain=1#L127).

## Detailed Design

1. Remove blocks per year calculation
1. Move module parameters to be constants because they should not be modifiable via governance

### Flow of funds

// TODO

## References

// TODO
