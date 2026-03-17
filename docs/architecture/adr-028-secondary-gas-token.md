# ADR: Secondary Fee Token Support

## Status

Draft

## Context

Celestia accepts fees only in `utia`, enforced in `app/ante/fee.go`. The rest of the fee path (`DeductFeeDecorator`, `x/bank`, fee collector, `x/distribution`) already operates on `sdk.Coins`.

This ADR proposes the smallest change that admits one hardcoded secondary fee denom, shipped in a chain upgrade the same way `BondDenom` is defined today.

## Decision

Accept a fee paid entirely in either `utia` or one hardcoded secondary denom. The denom is a compile-time constant in `appconsts`, selected by social consensus and shipped in an upgrade. Governance controls minimum gas prices in `x/minfee`; this ADR adds a `secondary_min_gas_price` field.

Each denom has its own minimum gas price and is validated independently. Mempool priority is local policy: nodes normalize actual gas price against their effective local minimum for that denom. Users choose the fee denom; validators receive fees in that denom.

Out of scope: new modules, new ante decorators, a generalized fee-token whitelist, governance-controlled denom selection, oracles, token conversion, and mixed-denom fees.

### Core Invariants

1. A transaction fee must contain exactly one coin.
2. The fee denom must be `utia` or the hardcoded secondary denom.
3. A configured secondary denom must have a positive minimum gas price in `x/minfee` params.
4. Each denom is validated against its own minimum gas price.
5. Mempool priority = actual gas price / effective local minimum gas price for that denom.

## Current Fee Path

```text
user fee coins
  -> app/ante.ValidateTxFee
  -> SDK DeductFeeDecorator
  -> FeeCollector module account
  -> x/distribution allocation
```

The single-denom assumption lives in:

| File | Consensus-critical? |
|------|---------------------|
| `app/ante/fee.go` | Yes |
| `app/app.go` | No (query helper) |
| `app/grpc/gasestimation/gas_estimator.go` | No (gas estimation) |
| `pkg/user/tx_options.go` | No (tx building) |
| `pkg/user/tx_client.go` | No (fee defaults) |

## Denom Configuration

```go
// pkg/appconsts/global_consts.go

const BondDenom = "utia"

// SecondaryFeeDenom is the accepted secondary fee token.
// Empty string means disabled. Set via social consensus in a chain upgrade.
const SecondaryFeeDenom = "" // set to the bank denom string when ready
```

When empty, the feature is fully disabled and all behavior is identical to today.

## Bridge Token Origin

The secondary fee token is presumably a bridged asset. It arrives on Celestia through either IBC or Hyperlane; the choice of bridge determines the denom string compiled into `SecondaryFeeDenom`.

### IBC (ICS-20)

A standard ICS-20 transfer mints the token as a bank coin with denom `ibc/<SHA256(path)>`, where `path` is the port/channel trace (e.g., `transfer/channel-0/usdc`). This denom is deterministic for a given channel but differs across channels carrying the same underlying asset.

- `SecondaryFeeDenom` is set to the full `ibc/...` string for the canonical channel.
- If the channel is ever closed and a new one opened, the denom changes and a new upgrade is required.

### Hyperlane (Warp Routes)

A Hyperlane Warp Route bridges the token via the `x/warp` module (from `hyperlane-cosmos`). The warp module holds `Minter` and `Burner` permissions on `x/bank` and mints synthetic tokens with denom `hyperlane/<token_id>`, where `token_id` is a hex address identifying the warp route token.

- `SecondaryFeeDenom` is set to the full `hyperlane/...` denom string for the relevant synthetic token.
- The denom is stable as long as the warp route token ID does not change.

### Implications for This ADR

The ante handler is bridge-agnostic: it only sees a `x/bank` denom string. The bridge choice affects the denom constant and the supply/redemption path, not fee validation logic. Bridging infrastructure is out of scope for this ADR.

## Param Model

Extend `celestia.minfee.v1.Params` by one field:

```protobuf
message Params {
  string network_min_gas_price = 1 [
    (cosmos_proto.scalar)  = "cosmos.Dec",
    (gogoproto.customtype) = "cosmossdk.io/math.LegacyDec",
    (gogoproto.nullable)   = false
  ];

  string secondary_min_gas_price = 2 [
    (cosmos_proto.scalar)  = "cosmos.Dec",
    (gogoproto.customtype) = "cosmossdk.io/math.LegacyDec",
    (gogoproto.nullable)   = false
  ];
}
```

Governance controls min gas prices via `MsgUpdateMinfeeParams`; denom selection stays in the upgrade constant.

### Param Validation

- `network_min_gas_price > 0`
- `SecondaryFeeDenom == ""` → `secondary_min_gas_price` must be `0`
- `SecondaryFeeDenom != ""` → `secondary_min_gas_price > 0`

No store migration required.

## Fee Validation

`ValidateTxFee` resolves the fee coin denom to its minimum gas price:

```go
func resolveMinGasPrice(fee sdk.Coins, params minfeetypes.Params) (sdk.Coin, math.LegacyDec, error) {
    if len(fee) != 1 {
        return sdk.Coin{}, math.LegacyDec{}, ErrInvalidFeeShape
    }

    coin := fee[0]

    switch coin.Denom {
    case appconsts.BondDenom:
        return coin, params.NetworkMinGasPrice, nil

    case appconsts.SecondaryFeeDenom:
        if appconsts.SecondaryFeeDenom == "" || !params.SecondaryMinGasPrice.IsPositive() {
            return sdk.Coin{}, math.LegacyDec{}, ErrSecondaryFeeDisabled
        }
        return coin, params.SecondaryMinGasPrice, nil

    default:
        return sdk.Coin{}, math.LegacyDec{}, ErrUnacceptedFeeDenom
    }
}
```

After resolution:

1. Compute `actualGasPrice = fee.Amount / gas`.
2. In `CheckTx`, enforce the local validator threshold for that denom.
3. In `CheckTx` and `DeliverTx`, enforce the network minimum for that denom.
4. In `CheckTx`, compute priority (see [Priority](#priority)).
5. Return the original `sdk.Coins` so the SDK deducts the chosen denom.

### Priority

Priority uses **normalized gas price** (`actual / effective local minimum`) so operators can favor one denom over the other without changing admission rules. The effective local minimum is the node's `min-gas-prices` for that denom if above the network minimum, otherwise the network minimum from `x/minfee`.

```go
// normalizedGasPrice = fee.Amount / (gas * effectiveLocalMinGasPrice)

normalizedDec := math.LegacyNewDecFromInt(coin.Amount).
    Quo(effectiveLocalMinGasPrice.MulInt64(int64(gas)))

priorityDec := normalizedDec.MulInt64(priorityScalingFactor)

priorityInt := priorityDec.TruncateInt()
if !priorityInt.IsInt64() {
    priority = math.MaxInt64
} else {
    priority = priorityInt.Int64()
}
```

Paying exactly the effective local minimum yields priority `1.0`; paying 2x yields `2.0`. Example: with `min-gas-prices = "0.002utia,0.004token"`, a tx at 0.004 utia/gas gets priority `2.0` while 0.004 token/gas gets `1.0` — that node favors `utia`.

## Local `min-gas-prices`

Validators may optionally configure a per-denom threshold:

```toml
# app.toml
min-gas-prices = "0.002utia,0.001<secondary-denom>"
```

If a validator omits the secondary denom, the node falls back to the network minimum from `x/minfee`. No operator action is required; local config is only needed for stricter thresholds or to express a denom preference via the priority normalization described above.

## Fee Collection and Distribution

No changes. The SDK deducts the submitted coin into the fee collector, and `x/distribution` allocates all fee collector balances via `sdk.DecCoins`, so rewards naturally accrue in both denoms.

## UX and Rollout

### Wallet / Tx Builder

Clients query `x/minfee`, let users choose a denom, and build a single fee coin. Gas estimation is unchanged.

### Validator / Operator

No action required. Operators who want to favor one denom can set per-denom values in `min-gas-prices`; raising the local minimum tightens admission and lowers that denom's relative priority.

### Governance

Governance does not select the denom (that stays in the upgrade constant). Governance sets `secondary_min_gas_price` via `MsgUpdateMinfeeParams` to define the network-wide admission floor.

### Rollout Sequence

1. Social consensus on the secondary fee denom.
2. Update clients, wallets, explorers, and observability.
3. Ship the chain upgrade with the denom in `appconsts.SecondaryFeeDenom`.
4. Set `secondary_min_gas_price` via governance.

## Implementation Surface

### Consensus-Critical

- `pkg/appconsts/global_consts.go` — add `SecondaryFeeDenom`
- `proto/celestia/minfee/v1/params.proto` — add `secondary_min_gas_price`
- `x/minfee/types/*.pb.go` (generated)
- `x/minfee/types/params.go`, `genesis.go`
- `app/ante/fee.go`
- `app/ante/fee_test.go`, `get_tx_priority_test.go`

### Additive (Non-Consensus)

- `app/app.go` — gas-price query helper
- `app/grpc/gasestimation/gas_estimator.go` — gas estimator
- `pkg/user/tx_options.go`, `pkg/user/tx_client.go` — tx building / fee defaults
- `app/errors/insufficient_gas_price.go` — error parsing

## Consequences

**Positive**: Minimal state changes. Backward compatible when disabled. Operators can express local preference between accepted fee denoms through existing `min-gas-prices` configuration.

**Negative**: Mempool ordering may differ across nodes because priority normalization depends on local operator policy. Rewards become multi-denom. Full UX requires follow-up outside the ante handler.

**Neutral**: Fee deduction and distribution structurally unchanged.
