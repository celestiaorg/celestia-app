# ADR 019: Inflation rate

## Status

Proposed

## Changelog

- 2023/3/31: initial draft

## Context

The inflation rate is decided to have a fixed rate with an initial value and a target value.
Table below shows that how it looks like according to the made decision.

| Name                     | Value  |
|--------------------------|--------|
| Initial Inflation        | 8.00%  |
| Disinflation rate p.a    | 10.00% |
| Target Inflation (floor) | 1.50%  |

Once we approach the target inflation, we stick to it forever.
The table below depicts the values over the coming years:

| year | inflation (%) |
|------|------|
|0  | 8.00 |
|1  | 7.20 |
|2  | 6.48 |
|3  | 5.832 |
|4  | 5.2488 |
|5  | 4.72392 |
|6  | 4.251528 |
|7  | 3.8263752 |
|8  | 3.44373768 |
|9  | 3.099363912 |
|10 | 2.7894275208 |
|11 | 2.51048476872 |
|12 | 2.259436291848 |
|13 | 2.0334926626632 |
|14 | 1.83014339639688 |
|15 | 1.647129056757192 |
|16 | 1.50 |
|17 | 1.50 |
|18 | 1.50 |
|19 | 1.50 |
|20 | 1.50 |

## Alternative Approaches

In order to do so, we need to have a simple mint module. So we remove the dynamic inflation rate calculation and simply map the current block height to the respective year and calculate the inflation rate according to that.

The logic is implemented in the `NextInflationRate` function and is called in the `BeginBlock` of the `mint` module.

If no custom inflation calculator function is passed to the Minter, the default function calculates the inflation rate that this ADR desires to achieve.

## Decision

The decision is already made and the ADR is accepted. We are waiting for a final inspection though.

## Detailed Design

Since the rates are fixed, we hardcoded them in a set of constants.

```go
const (
	InitialInflationRate    = 0.08
	TargetInflationRate     = 0.015 // floor
	DisinflationRatePerYear = 0.1
)
```

The `NextInflationRate` function calculated the `year` based on the current block height that comes from the `sdk.Context` and the number of `BlocksPerYear` that comes from the params.
Then it computes the inflation rate according to the determined fixed rate per year.

```go
NextInflationRate(ctx sdk.Context, params Params) sdk.Dec {

	year := uint64(ctx.BlockHeader().Height) / params.BlocksPerYear

	initInflationRate := sdk.NewDecWithPrec(InitialInflationRate*1000, 3 /* since we used 1000 */)
	targetInflationRate := sdk.NewDecWithPrec(TargetInflationRate*1000, 3 /* since we used 1000 */)

	// initInflationRate * ((1 - DisinflationRate) ^ year)
	newInflationRate := initInflationRate.Mul(
		sdk.OneDec().Sub(
			sdk.NewDecWithPrec(DisinflationRatePerYear*100, 2 /* since we used 100 */)).
			Power(year))

	if newInflationRate.LT(targetInflationRate) {
		newInflationRate = targetInflationRate
	} else {
		if newInflationRate.GT(initInflationRate) {
			newInflationRate = initInflationRate
		}
	}

	return newInflationRate
}
```

## Consequences

> This section describes the consequences, after applying the decision. All consequences should be summarized here, not just the "positive" ones.

### Positive

### Negative

### Neutral

## References

- {reference link}
