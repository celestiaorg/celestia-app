# ADR 023: Gas used and gas price estimation

## Changelog

- 2025/01/17: Initial draft (@rach-id)
- 2025/03/13: Improve the estimation mechanism and make it rely on the mempool transactions (@rach-id)

## Status

Implemented

## Context

As per [CIP-18: Standardised Gas and Pricing Estimation Interface](https://github.com/celestiaorg/CIPs/blob/main/cips/cip-018.md), celestia-app will provide a base implementation for the gas and fee estimation so it can be used when building transactions.

## Decision

We will proceed with the implementation in celestia-app.

The fee estimation mechanism is described below.

## Detailed Design

The API will be provided through gRPC following the interface provided in [CIP-18](https://github.com/celestiaorg/CIPs/blob/main/cips/cip-018.md).

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

There are multiple ways to have a gas price estimation for a priority level. This implementation will use the following:

- High Priority: The gas price is the median price of the top 10% of transactions’ gas prices in the mempool.
- Medium Priority: The gas price is the median price of the all gas prices in the mempool.
- Low Priority: The gas price is the median price of the bottom 10% of gas prices in the mempool.
- None Priority: This is equivalent to the Medium priority, using is the median price of all gas prices in the mempool.

If the mempool has more transactions that it can fit in the next block, the estimation will be based on the top gas prices that can fit in a full block. Otherwise, if the mempool transactions can't fill more than 70% of the max block size, the minimum gas price will be returned.

If the estimated gas price is equal to the minimum gas price, an increase of 30% and 10% will be added for high and medium priority respectively.

The following is a basic implementation of the median that we can use:

```go
// Median calculates the median value of the provided gas prices.
// Expects a sorted slice.
func Median(gasPrices []float64) (float64, error) {
    n := len(gasPrices)
    if n == 0 {
        return 0, errors.New("cannot compute median of an empty slice")
    }

    if n%2 == 1 {
        return gasPrices[n/2], nil
    }
    mid1 := gasPrices[n/2-1]
    mid2 := gasPrices[n/2]
    return (mid1 + mid2) / 2.0, nil
}
```

For the top/bottom 10%, they will be extracted after sorting the list of gas prices incrementally, and slicing `gasPrices[:len(gasPrices)*10/100]` and `gasPrices[len(gasPrices)*90/100:]` respectively.

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

- [CIP-18: Standardised Gas and Pricing Estimation Interface](https://github.com/celestiaorg/CIPs/blob/main/cips/cip-018.md).
