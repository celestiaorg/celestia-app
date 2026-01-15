# `x/feeaddress`

## Abstract

The `x/feeaddress` module provides a mechanism to forward utia tokens to the fee collector module, which distributes them to validators as staking rewards. Users can send utia to the well-known fee address, and the EndBlocker automatically forwards any received tokens to the fee collector.

## Fee Address

The fee address is a vanity address for easy recognition:

```
celestia1feefeefeefeefeefeefeefeefeefeefe8pxlcf
```

## How It Works

1. **Sending**: Users or contracts send utia tokens to the fee address via standard bank transfer or IBC transfer
2. **Forwarding**: At the end of each block, the EndBlocker checks the fee address balance
3. **Distribution**: Any utia at the fee address is automatically forwarded to the fee collector module
4. **Rewards**: The distribution module allocates fee collector funds to validators as staking rewards

## Restrictions

Only the native token (utia) can be sent to the fee address:

- **Ante Decorator**: Rejects transactions (MsgSend, MsgMultiSend, MsgTransfer) that attempt to send non-utia to the fee address
- **IBC Middleware**: Rejects inbound IBC transfers of non-utia tokens to the fee address

## Queries

### FeeAddress

Returns the bech32-encoded fee address for programmatic discovery.

```bash
grpcurl -plaintext localhost:9090 celestia.feeaddress.v1.Query/FeeAddress
```

## Events

### EventFeeForwarded

Emitted when tokens are forwarded from the fee address to the fee collector.

| Attribute | Type   | Description                     |
|-----------|--------|---------------------------------|
| from      | string | The fee address (bech32)        |
| amount    | string | Amount forwarded (e.g., "1000utia") |
