# `x/feeaddress`

## Abstract

The `x/feeaddress` module provides a mechanism to forward utia tokens to the fee collector module, which distributes them to validators as staking rewards. Users can send utia to the well-known fee address, and tokens are automatically forwarded as transaction fees via protocol-injected transactions in the next block.

## Fee Address

The fee address is a vanity address for easy recognition:

```
celestia1feefeefeefeefeefeefeefeefeefeefe8pxlcf
```

## How It Works

1. **Sending**: Users or contracts send utia tokens to the fee address via standard bank transfer or IBC transfer
2. **PrepareProposal**: Block proposers check the fee address balance and inject a `MsgForwardFees` transaction with the fee set to the balance
3. **ProcessProposal**: Validators strictly enforce that blocks forward any non-zero fee address balance
4. **Ante Handler**: The `FeeForwardDecorator` deducts the fee from the fee address and sends it to the fee collector
5. **Distribution**: The distribution module allocates fee collector funds to validators as staking rewards

## Dashboard Compatibility

This design converts fee address funds into real transaction fees (via the `tx.AuthInfo.Fee` field), making them visible to blockchain analytics dashboards that track protocol revenue.

## Restrictions

Only the native token (utia) can be sent to the fee address:

- **Ante Decorator**: Rejects transactions (MsgSend, MsgMultiSend, MsgTransfer) that attempt to send non-utia to the fee address
- **IBC Middleware**: Rejects inbound IBC transfers of non-utia tokens to the fee address

## Message Types

### MsgForwardFees

This message is protocol-injected by block proposers and should not be submitted by users directly.

```protobuf
message MsgForwardFees {
  string proposer = 1;  // Hex-encoded block proposer address
}
```

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
