# ADR 023: Gas used and gas price estimation

## Changelog

- 2025/01/17: Initial draft (@rach-id)

## Status

Proposed

## Context

As per [CIP-18: Standardised Gas and Pricing Estimation Interface](https://github.com/celestiaorg/CIPs/blob/main/cips/cip-18.md), celestia-app will provide a base implementation for the gas and fee estimation so it can be used when building transactions.

## Decision

We will proceed with the implementation in celestia-app.

The fee estimation mechanism is described below.

## Detailed Design

The API will be provided through gRPC following the interface provided in [CIP-18](https://github.com/celestiaorg/CIPs/blob/main/cips/cip-18.md).

### Gas used estimation

The gas used estimation will be calculated using the baseapp simulation mode:

```go
	gasInfo, _, err := app.BaseApp.Simulate(tx)
	if err != nil {
		return nil, err
	}
```

_Note:_ The returned result will exactly match the output of the state machine, without any gas multiplier applied to adjust the values.
Clients using this endpoint are responsible for accounting for any necessary multipliers.

### Gas price estimation

There are multiple ways to have a gas price estimation for a priority level. For a first implementation, we will use the following:

- High Priority: The gas price is the price at the start of the top 10% of transactions’ gas prices from the last 5 blocks.
- Medium Priority: The gas price is the median of all gas prices from the last 5 blocks.
- Low Priority: The gas price is the value at the end of the lowest 10% of gas prices from the last 5 blocks.
- None Priority: This is equivalent to the Medium priority, using the median of all gas prices from the last 5 blocks.

The calculation of the top 10% and bottom 10% will be done using the [standard deviation and z-scores](https://en.wikipedia.org/wiki/Standard_normal_table#Cumulative_(less_than_Z)).
A z-score or a standard score represents the number of standard deviations a data point is from the mean in a standard normal distribution.

Provided the z-scores table, we can see that:

- High priority: corresponds to `mean + 1.28 * standard_deviation` value.
- Medium priority: is the `mean` value.
- Low priority: corresponds to `mean - 1.28 * standard_deviation` value.
- None priority: is the `mean` value.

Note: if the last 5 blocks are all empty, we will return the node's min gas price.

The following is a basic implementation of the standard deviation that we can use:

```go
// mean calculates the mean value of the provided gas prices.
func mean(gasPrices []float64) float64 {
    if len(gasPrices) == 0 {
		return 0
	}
	sum := 0.0
	for _, gasPrice := range gasPrices {
		sum += gasPrice
	}
	return sum / float64(len(gasPrices))
}

// standardDeviation calculates the standard deviation of the provided gas prices.
func standardDeviation(gasPrices []float64) float64 {
    if len(gasPrices) < 2 {
		return 0
	}
	meanGasPrice := mean(gasPrices)
	var variance float64
	for _, gasPrice := range gasPrices {
		variance += math.Pow(gasPrice-meanGasPrice, 2)
	}
	variance /= float64(len(gasPrices))
	return math.Sqrt(variance)
}
```

## Alternative Approaches

There are multiple ways to estimate the gas price. However, a first default implementation can be as basic as described above. 

Better estimations can be done subsequently.

## Consequences

### Positive

- Provide a gas used and gas price estimation for clients to use

### Negative

None.

### Neutral

None.

## References

- [CIP-18: Standardised Gas and Pricing Estimation Interface](https://github.com/celestiaorg/CIPs/blob/main/cips/cip-18.md).
- [standard deviation and z-scores](https://en.wikipedia.org/wiki/Standard_normal_table#Cumulative_(less_than_Z)).
