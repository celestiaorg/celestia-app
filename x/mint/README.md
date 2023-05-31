# `x/mint`

`x/mint` is a fork of the Cosmos SDK `x/mint` module that makes some changes to the inflation mechanism.

1. Remove all parameters from the module
1. Calculate inflation rate once per year based on the number of years since genesis
1. Calculate annual provisions once per year based on the total supply
1. Calculate block provision once per block based on the number of seconds elapsed between the current block and the previous block

## Constants

See [./types/constants.go](./types/constants.go) for the constants used in this module.

Note: this module assumes `DaysPerYear = 365.2425` so when modifying tests, developers must define durations based on this assumption because ordinary durations won't return the expected results. In other words:

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

## References

1. ADR-019
