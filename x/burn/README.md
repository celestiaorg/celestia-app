# `x/burn`

The burn module provides functionality for permanently destroying TIA tokens. When tokens are burned, they are removed from the signer's account and the total circulating supply, effectively reducing the total token supply.

## Concepts

- Burn: The irreversible destruction of tokens, removing them from circulation permanently.
- Only the native staking token (`utia`) can be burned.

## State

This module is stateless. It does not persist any data and relies entirely on the bank module for token operations.

## State Transitions

When a burn is executed:
1. Tokens are transferred from the signer's account to the burn module account
2. Tokens are burned from the module account, reducing total supply

## Messages

### MsgBurn

`MsgBurn` permanently removes tokens from the signer's account and circulating supply.

```protobuf
message MsgBurn {
  string signer = 1;
  cosmos.base.v1beta1.Coin amount = 2;
}
```

Validation:
- `signer` must be a valid bech32 address
- `amount.denom` must be `utia`
- `amount.amount` must be positive

## Events

### EventBurn

Emitted when tokens are successfully burned.

| Attribute | Type   | Description                          |
|-----------|--------|--------------------------------------|
| signer    | string | Address that burned the tokens       |
| amount    | string | Amount burned (e.g., "1000000utia")  |

## Client

### CLI

```shell
celestia-appd tx burn burn <amount> --from <key>
```

Example:
```shell
celestia-appd tx burn burn 1000000utia --from mykey
```

### gRPC

```shell
grpcurl -plaintext localhost:9090 celestia.burn.v1.Msg/Burn
```
