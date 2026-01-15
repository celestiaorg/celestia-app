# `x/burn`

The burn module permanently destroys TIA tokens sent to the burn address, reducing total supply.

## Burn Address

```text
celestia1nullnullnullnullnullnullnullnull8qanmn
```

This is a vanity address that encodes to "null" repeated 8 times, making it recognizable as a null/void destination where tokens are permanently destroyed.

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
5. Emit `EventBurn` with burner and amount
6. Update `TotalBurned` state

## Ante Decorator

The `BurnAddressRestrictionDecorator` validates transactions containing:

- `MsgSend` - checks `ToAddress`
- `MsgMultiSend` - checks all `Outputs[].Address`
- `MsgTransfer` (IBC) - checks `Receiver`
- `MsgExec` (authz) - recursively validates all nested messages

If recipient is the burn address, only `utia` denomination is allowed.

## Events

### EventBurn

Emitted when tokens are burned in EndBlocker.

| Attribute | Type   | Description                          |
|-----------|--------|--------------------------------------|
| burner    | string | Burn address                         |
| amount    | string | Amount burned (e.g., "1000000utia")  |

### SDK Events

The bank module's `BurnCoins` also emits a `coin_burn` event, which dashboards already track.

## Queries

### TotalBurned

Returns cumulative tokens burned.

```shell
grpcurl -plaintext localhost:9090 celestia.burn.v1.Query/TotalBurned
```

### BurnAddress

Returns the burn address for programmatic discovery.

```shell
grpcurl -plaintext localhost:9090 celestia.burn.v1.Query/BurnAddress
```

## Client

### CLI

Send tokens to the burn address:

```shell
celestia-appd tx bank send <from-key> celestia1nullnullnullnullnullnullnullnull8qanmn <amount>
```

Example:

```shell
celestia-appd tx bank send mykey celestia1nullnullnullnullnullnullnullnull8qanmn 1000000utia
```

### IBC Transfer

Tokens can also be burned via IBC transfer:

```shell
celestia-appd tx ibc-transfer transfer <channel> celestia1nullnullnullnullnullnullnullnull8qanmn <amount> --from <key>
```

## Hyperlane

**Outbound** (`MsgRemoteTransfer`): Recipient is on another chain, not Celestia. The burn address
restriction does not apply since the recipient is specified as a hex address on the destination chain.

**Inbound** (via `MsgProcessMessage`): If tokens are bridged TO Celestia with the burn address as
recipient, they will land at the burn address. Native utia will be burned in EndBlocker. However,
synthetic Hyperlane tokens (non-utia) would be permanently stuck since EndBlocker only burns utia.
This is documented behavior - avoid sending non-utia to the burn address via Hyperlane.

## IBC Inbound Transfers

Inbound IBC transfers are validated by the `BurnAddressIBCMiddleware`. If a remote chain attempts
to send non-utia tokens to the burn address, the transfer is rejected with an error acknowledgement
and the sender receives a refund on the source chain.

**Allowed:** Native utia returning to Celestia can be sent to the burn address (e.g., utia sent
to another chain via IBC and then sent back to the burn address).

**Rejected:** Foreign tokens (e.g., `uosmo`, `uatom`, or any `ibc/HASH...` denom) sent to the
burn address are rejected with an error acknowledgement.

## ICA (Interchain Accounts)

ICA host messages bypass the ante handler chain. If an ICA controller on another chain executes
`MsgSend` with the burn address as recipient, the burn address restriction is not enforced.
Non-utia tokens sent this way would be permanently stuck (not burned, not stolen).

**Accepted Risk:** This bypass is documented and accepted because:

1. Stuck tokens do not benefit any party (they are not stolen, just inaccessible)
2. ICA usage is uncommon and requires explicit controller setup on another chain
3. The operational complexity of mitigating this edge case outweighs the risk
