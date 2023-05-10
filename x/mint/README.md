# `x/mint`

`x/mint` is a fork of the Cosmos SDK `x/mint` module that makes some changes to the inflation mechanism.

1. Remove all parameters from the module
1. Calculate inflation rate based on the number of years since genesis

## Constants

See [./types/constants.go](./types/constants.go) for the constants used in this module.

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
