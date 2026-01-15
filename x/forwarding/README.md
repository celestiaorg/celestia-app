# `x/forwarding`

## Abstract

The `x/forwarding` module enables single-signature cross-chain transfers through Celestia via Hyperlane warp routes. Users send tokens to a deterministically derived forwarding address (`forwardAddr`), and anyone can permissionlessly trigger forwarding to the committed destination.

## Key Properties

- **Single signature UX**: User signs once on source chain
- **CEX compatible**: Works with exchange withdrawals (no smart contract interaction needed)
- **Permissionless execution**: Anyone can trigger forwarding with correct parameters
- **Cryptographic binding**: Funds can only go to the committed destination

## Flow

1. Frontend computes `forwardAddr = derive(destDomain, destRecipient)`
2. User sends tokens to forwardAddr via EVM warp transfer or CEX withdrawal
3. Relayer detects deposit and submits `MsgExecuteForwarding`
4. Module verifies derivation and executes warp transfer to destination
5. Tokens arrive at destRecipient on destination chain

## Address Derivation

Forwarding addresses are deterministically derived:

```
callDigest   = sha256(destDomain_32bytes || destRecipient)
salt         = sha256(version_byte || callDigest)       // version_byte = 0x01
forwardAddr  = address.Module("forwarding", salt)[:20]
```

One address handles all tokens for a given `(destDomain, destRecipient)` pair.

## State

### Params

```protobuf
message Params {
  string min_forward_amount = 1;    // Minimum amount to forward (0 = disabled)
  string tia_collateral_token_id = 2; // Token ID for native TIA forwarding
}
```

## Messages

### MsgExecuteForwarding

Forwards ALL tokens at a forwarding address to the committed destination.

```protobuf
message MsgExecuteForwarding {
  string signer = 1;        // Relayer/anyone (pays gas)
  string forward_addr = 2;  // The derived forwarding address
  uint32 dest_domain = 3;   // Destination chain domain ID
  string dest_recipient = 4; // Recipient on destination (32 bytes, hex)
}

message MsgExecuteForwardingResponse {
  repeated ForwardingResult results = 1;  // Per-token results
}

message ForwardingResult {
  string denom = 1;
  string amount = 2;
  string message_id = 3;  // Hyperlane message ID (empty if failed)
  bool success = 4;
  string error = 5;
}
```

## Multi-Token Forwarding

- Gets ALL balances at `forwardAddr` and processes each independently
- Partial failures allowed: one token failing doesn't block others
- Failed tokens stay at `forwardAddr` for retry

## Supported Token Types

| Source | Denom Format | Transfer Method |
|--------|--------------|-----------------|
| EVM warp transfer | `hyperlane/{tokenId}` | Synthetic warp transfer |
| CEX withdrawal (TIA) | `utia` | Collateral warp transfer |

## Events

### EventTokenForwarded

| Attribute | Description |
|-----------|-------------|
| forward_addr | The forwarding address |
| denom | Token denomination |
| amount | Amount forwarded |
| message_id | Hyperlane message ID (empty if failed) |
| success | Whether forwarding succeeded |
| error | Error message if failed |

### EventForwardingComplete

| Attribute | Description |
|-----------|-------------|
| forward_addr | The forwarding address |
| dest_domain | Destination chain domain |
| dest_recipient | Recipient on destination |
| tokens_forwarded | Count of successful forwards |
| tokens_failed | Count of failed forwards |

## Queries

### DeriveForwardingAddress

```bash
celestia-appd query forwarding derive-address \
  --dest-domain 42161 \
  --dest-recipient 0x000000000000000000000000<recipient-address>
```

## CLI Usage

```bash
# Query derived address
celestia-appd query forwarding derive-address \
  --dest-domain 1 \
  --dest-recipient 0x000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef

# Check balance at forward address
celestia-appd query bank balances <forward-addr>

# Execute forwarding (forwards ALL tokens at address)
celestia-appd tx forwarding execute \
  --forward-addr <forward-addr> \
  --dest-domain 1 \
  --dest-recipient 0x000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef \
  --from relayer
```

## Error Codes

| Code | Name | Description |
|------|------|-------------|
| 1 | ErrAddressMismatch | Derived address doesn't match provided address |
| 2 | ErrNoBalance | No balance at forwarding address |
| 3 | ErrBelowMinimum | Balance below minimum threshold |
| 4 | ErrUnsupportedToken | Token denom not supported for forwarding |

## Security

- **Cryptographic binding**: The `forwardAddr` cryptographically commits to `(destDomain, destRecipient)`. Funds can only be forwarded to the committed destination.
- **Permissionless execution**: Anyone can trigger forwarding, but only to the pre-committed destination.
- **No fund loss**: Failed tokens stay at `forwardAddr` or are automatically returned there.

## Test Vectors

For cross-platform compatibility (Go, TypeScript, Rust):

| destDomain | destRecipient | Expected Address |
|------------|---------------|------------------|
| 1 | `0x000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef` | `cosmos1gev9segv9333lpy27thrtwdjwhu9lgcjpkpz2x` |
| 42161 | `0x0000000000000000000000001234567890abcdef1234567890abcdef12345678` | `cosmos1uvqe9n0eclkd55dj9g9m30nf77px6jq2nfqmyw` |
| 0 | `0x0000000000000000000000000000000000000000000000000000000000000000` | `cosmos1w0c30l5s7q46nhnz7k7j82j6kdsgz4w4m25jjg` |
