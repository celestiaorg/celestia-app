# `x/forwarding`

## Abstract

The `x/forwarding` module enables single-signature cross-chain transfers through Celestia via Hyperlane warp routes. Users send tokens to a deterministically derived forwarding address (`forwardAddr`), and anyone can permissionlessly trigger forwarding to the committed destination.

## Key Properties

- **Single signature UX**: User signs once on source chain
- **CEX compatible**: Works with exchange withdrawals (no smart contract interaction needed)
- **Permissionless execution**: Anyone can trigger forwarding with correct parameters
- **Cryptographic binding**: Funds can only go to the committed destination

## Flow

```
1. Frontend computes forwardAddr = derive(destDomain, destRecipient)
2. User sends tokens to forwardAddr via EVM warp transfer or CEX withdrawal
3. Relayer detects deposit and submits MsgExecuteForwarding
4. Module verifies derivation and executes warp transfer to destination
5. Tokens arrive at destRecipient on destination chain
```

## State

The forwarding module maintains minimal state:

### Params

```protobuf
message Params {
  // Global minimum amount required to forward a token (set to 0 to disable)
  string min_forward_amount = 1;

  // TIA collateral token ID for native TIA forwarding
  string tia_collateral_token_id = 2;
}
```

## Address Derivation

Forwarding addresses are deterministically derived from the destination parameters:

```go
func DeriveForwardingAddress(destDomain uint32, destRecipient []byte) sdk.AccAddress {
    // 1. Encode destDomain as 32-byte big-endian
    destDomainBytes := make([]byte, 32)
    binary.BigEndian.PutUint32(destDomainBytes[28:], destDomain)

    // 2. callDigest = keccak256(destDomain || destRecipient)
    callDigest := crypto.Keccak256(append(destDomainBytes, destRecipient...))

    // 3. salt = keccak256("CELESTIA_FORWARD_V1" || callDigest)
    salt := crypto.Keccak256(append([]byte("CELESTIA_FORWARD_V1"), callDigest...))

    // 4. address = sha256("forwarding" || salt)[:20]
    hash := sha256.Sum256(append([]byte("forwarding"), salt...))
    return sdk.AccAddress(hash[:20])
}
```

One address handles all tokens for a given `(destDomain, destRecipient)` pair.

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
  string error = 5;       // Error message if failed
}
```

## Multi-Token Forwarding

When `MsgExecuteForwarding` is called:
1. Gets ALL balances at `forwardAddr`
2. Processes each token independently
3. Returns per-token success/failure results

### Partial Failure Behavior (By Design)

Multi-token forwarding intentionally allows partial failures:
- **Permissionless retry**: Failed tokens stay at `forwardAddr` for later retry
- **Progressive forwarding**: New warp routes can forward previously unsupported tokens
- **No stuck transactions**: One bad token doesn't block others

### Pre-check Pattern

Most failures are caught BEFORE moving tokens:
- Token not supported -> Fails pre-check, stays at `forwardAddr`
- No warp route -> Fails pre-check, stays at `forwardAddr`
- Below minimum threshold -> Fails pre-check, stays at `forwardAddr`

In the rare case where warp transfer fails after pre-checks pass, tokens are **automatically returned to `forwardAddr`**.

## Supported Token Types

| Source | Denom Format | Transfer Method |
|--------|--------------|-----------------|
| EVM warp transfer | `hyperlane/{tokenId}` | Synthetic warp transfer |
| CEX withdrawal (TIA) | `utia` | Collateral warp transfer |

## Events

### EventTokenForwarded

Emitted for each token (success or failure):

| Attribute | Description |
|-----------|-------------|
| forward_addr | The forwarding address |
| denom | Token denomination |
| amount | Amount forwarded |
| message_id | Hyperlane message ID (empty if failed) |
| success | Whether forwarding succeeded |
| error | Error message if failed |

### EventForwardingComplete

Summary event after processing all tokens:

| Attribute | Description |
|-----------|-------------|
| forward_addr | The forwarding address |
| dest_domain | Destination chain domain |
| dest_recipient | Recipient on destination |
| tokens_forwarded | Count of successful forwards |
| tokens_failed | Count of failed forwards |

## Queries

### DeriveForwardingAddress

Compute a forwarding address off-chain:

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

For cross-platform compatibility (Go, TypeScript, Rust), use these test vectors:

| destDomain | destRecipient | Expected Address |
|------------|---------------|------------------|
| 1 | `0x000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef` | `cosmos1gev9segv9333lpy27thrtwdjwhu9lgcjpkpz2x` |
| 42161 | `0x0000000000000000000000001234567890abcdef1234567890abcdef12345678` | `cosmos1uvqe9n0eclkd55dj9g9m30nf77px6jq2nfqmyw` |
| 0 | `0x0000000000000000000000000000000000000000000000000000000000000000` | `cosmos1w0c30l5s7q46nhnz7k7j82j6kdsgz4w4m25jjg` |
