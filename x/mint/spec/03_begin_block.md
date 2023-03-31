<!--
order: 3
-->

# Begin-Block

Minting parameters are recalculated and inflation
paid at the beginning of each block.

## Inflation rate calculation

Inflation rate is calculated using an "inflation calculation function" that's
passed to the `NewAppModule` function. If no function is passed, then the SDK's
default inflation function will be used (`NextInflationRate`). In case a custom
inflation calculation logic is needed, this can be achieved by defining and
passing a function that matches `InflationCalculationFn`'s signature.

```go
type InflationCalculationFn func(ctx sdk.Context, minter Minter, params Params, bondedRatio sdk.Dec) sdk.Dec
```

### NextInflationRate

The target annual inflation rate is recalculated each block.
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

## NextAnnualProvisions

Calculate the annual provisions based on current total supply and inflation
rate. This parameter is calculated once per block.

```go
NextAnnualProvisions(params Params, totalSupply sdk.Dec) (provisions sdk.Dec) {
	return Inflation * totalSupply
```

## BlockProvision

Calculate the provisions generated for each block based on current annual provisions. The provisions are then minted by the `mint` module's `ModuleMinterAccount` and then transferred to the `auth`'s `FeeCollector` `ModuleAccount`.

```go
BlockProvision(params Params) sdk.Coin {
	provisionAmt = AnnualProvisions/ params.BlocksPerYear
	return sdk.NewCoin(params.MintDenom, provisionAmt.Truncate())
```
