# `x/burn`

The burn module permanently destroys TIA tokens sent to the burn address, reducing total supply.

## Burn Address

```
celestia1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqzf30as
```

This is a vanity address derived from 20 zero bytes, making it easy to recognize (32 `q` characters).

## Concepts

- **Burn Address**: A special address that accepts utia. Tokens sent here are automatically burned at the end of each block.
- **Denomination Restriction**: Only `utia` can be sent to the burn address. Other denominations are rejected.
- **Total Burned Tracking**: The module tracks cumulative burned tokens for analytics.

## State

| Key | Type | Description |
|-----|------|-------------|
| `TotalBurned` | `sdk.Coin` | Cumulative utia burned |

## State Transitions

At the end of each block (EndBlocker):
1. Check burn address utia balance
2. If zero, return early
3. Transfer tokens from burn address to burn module account
4. Burn tokens from module account (emits SDK `coin_burn` event)
5. Emit `EventBurn` with signer and amount
6. Update `TotalBurned` state

## Ante Decorator

The `BurnAddressRestrictionDecorator` validates transactions containing:
- `MsgSend` - checks `ToAddress`
- `MsgMultiSend` - checks all `Outputs[].Address`
- `MsgTransfer` (IBC) - checks `Receiver`

If recipient is the burn address, only `utia` denomination is allowed.

## Events

### EventBurn

Emitted when tokens are burned in EndBlocker.

| Attribute | Type   | Description                          |
|-----------|--------|--------------------------------------|
| signer    | string | Burn address                         |
| amount    | string | Amount burned (e.g., "1000000utia")  |

### SDK Events

The bank module's `BurnCoins` also emits a `coin_burn` event, which dashboards already track.

## Queries

### TotalBurned

Returns cumulative tokens burned.

```shell
grpcurl -plaintext localhost:9090 celestia.burn.v1.Query/TotalBurned
```

## Client

### CLI

Send tokens to the burn address:
```shell
celestia-appd tx bank send <from-key> celestia1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqzf30as <amount>
```

Example:
```shell
celestia-appd tx bank send mykey celestia1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqzf30as 1000000utia
```

### IBC Transfer

Tokens can also be burned via IBC transfer:
```shell
celestia-appd tx ibc-transfer transfer <channel> celestia1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqzf30as <amount> --from <key>
```

## Hyperlane

No special handling needed:
- **Outbound**: Recipient is on another chain, not Celestia
- **Inbound**: Tokens landing at burn address get burned (intended behavior)
