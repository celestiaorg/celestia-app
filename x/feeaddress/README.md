# `x/feeaddress`

## Abstract

The `x/feeaddress` module provides a mechanism to forward utia tokens to the fee collector module, which distributes them to delegators as staking rewards. Users can send utia to the well-known fee address, and tokens are automatically forwarded as transaction fees via protocol-injected transactions in the next block.

## Fee Address

The fee address is a module account address derived from the module name "feeaddress":

```text
celestia18sjk23yldd9dg7j33sk24elwz2f06zt7ahx39y
```

The address can also be queried via the `Query/FeeAddress` gRPC endpoint.

## How It Works

1. **Sending**: Users or contracts send utia tokens to the fee address via standard bank transfer, IBC transfer, or Hyperlane transfer
2. **PrepareProposal**: Block proposers check the fee address balance and inject a `MsgPayProtocolFee` transaction with the tx fee set to the balance from the fee address
3. **ProcessProposal**: Validators strictly enforce that blocks forward any non-zero fee address balance
4. **Ante Handler**: The `ProtocolFeeTerminatorDecorator` deducts the fee from the fee address and sends it to the fee collector
5. **Distribution**: The distribution module allocates fee collector funds to delegators as staking rewards

## Dashboard Compatibility

This design converts fee address funds into real transaction fees (via the `tx.AuthInfo.Fee` field), making them visible to blockchain analytics dashboards that track protocol revenue.

## Restrictions

Only the native token (utia) can be sent to the fee address via direct transactions:

- **Ante Decorator**: Rejects transactions (MsgSend, MsgMultiSend) that attempt to send non-utia to the fee address

## Security Considerations

### Known Bypass Vectors

The following paths can bypass the non-utia restriction:

1. **IBC Transfers**: Inbound IBC transfers from counterparty chains are not blocked
2. **ICA Host Messages**: Interchain Accounts can execute MsgSend to the fee address, bypassing ante handlers
3. **Hyperlane MsgProcessMessage**: Cross-chain messages via Hyperlane are not blocked

**Impact**: Non-utia tokens sent via these paths will be **permanently stuck** at the fee address. They cannot be forwarded (only utia is forwarded) and cannot be recovered (no governance mechanism exists). The tokens are not stolen or at risk of theft.

**Recommendation**: Do not send non-utia tokens to the fee address

## Message Types

### MsgPayProtocolFee

This message is protocol-injected by block proposers and should not be submitted by users directly.

```protobuf
message MsgPayProtocolFee {}
```

The message has no fields. Validation happens via ProcessProposal checking that the transaction fee equals the fee address balance.

## Queries

### FeeAddress

Returns the bech32-encoded fee address for programmatic discovery.

```bash
grpcurl -plaintext localhost:9090 celestia.feeaddress.v1.Query/FeeAddress
```
