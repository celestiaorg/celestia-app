# `x/forwarding`

## Abstract

The `x/forwarding` module enables single-signature cross-chain transfers through Celestia via Hyperlane warp routes. Users send tokens to a deterministically derived forwarding address (`forwardAddr`), and anyone can permissionlessly trigger forwarding to the committed destination.

> **Note**: This module is for Hyperlane cross-chain transfers only. It does NOT replace IBC Packet Forward Middleware (PFM).

## Key Properties

- **Single signature UX**: User signs once on source chain
- **CEX compatible**: Works with exchange withdrawals (no smart contract interaction needed)
- **Permissionless execution**: Anyone can trigger forwarding with correct parameters
- **Cryptographic binding**: Funds can only go to the committed destination

## Flow

1. Frontend computes `forwardAddr = derive(destDomain, destRecipient)`
2. User sends tokens to forwardAddr via warp transfer or CEX withdrawal
3. Relayer detects deposit and submits `MsgForward`
4. Module verifies derivation and executes warp transfer to destination
5. Tokens arrive at destRecipient on destination chain

## Relayer

For relayer implementation details and operational guides, see the [forwarding-relayer documentation](https://github.com/celestiaorg/forwarding-relayer/blob/master/RELAYER.md).

## Address Derivation

Forwarding addresses are deterministically derived:

```text
callDigest   = sha256(destDomain_32bytes || destRecipient)
salt         = sha256(version_byte || callDigest)       // version_byte = 0x01
forwardAddr  = address.Module("forwarding", salt)[:20]
```

One address handles all tokens for a given `(destDomain, destRecipient)` pair.

## State

### Params

```protobuf
message Params {
  // Currently empty; reserved for future governance-controlled parameters
}
```

Note: TIA collateral token is discovered at runtime by iterating warp tokens with `OriginDenom="utia"` and checking for routes to the destination domain.

## Messages

### MsgForward

Forwards up to 20 tokens at a forwarding address to the committed destination.
The relayer (signer) pays both Celestia gas and Hyperlane IGP fees.

```protobuf
message MsgForward {
  string signer = 1;        // Relayer (pays gas + IGP fees)
  string forward_addr = 2;  // The derived forwarding address
  uint32 dest_domain = 3;   // Destination chain domain ID
  string dest_recipient = 4; // Recipient on destination (32 bytes, hex)
  Coin max_igp_fee = 5;     // Max IGP fee relayer will pay per token
}

message MsgForwardResponse {
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

### Token Limits

- `MaxTokensPerForward = 20` - Maximum token denominations processed per call
- If an address has >20 denoms, first call forwards the first 20 (ordered by denom), subsequent calls handle the rest
- This limit prevents unbounded gas consumption in a single transaction

## Supported Token Types

| Source | Denom Format | Transfer Method |
|--------|--------------|-----------------|
| Warp transfer | `hyperlane/{tokenId}` | Synthetic warp transfer |
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

## Fee Handling

### Hyperlane IGP Fees

Cross-chain message delivery requires paying Hyperlane's IGP (Interchain Gas Paymaster).
The relayer (signer) pays these fees as part of `MsgForward`.

**Fee Flow:**

1. Relayer queries `QuoteForwardingFee` to estimate the required IGP fee
2. Relayer submits `MsgForward` with `max_igp_fee >= quoted fee`
3. Module quotes actual fee via Hyperlane's `QuoteDispatch`
4. IGP fee sent directly from relayer to `forwardAddr`
5. Warp executes from `forwardAddr` (which pays the IGP fee)
6. Any excess IGP fee is refunded from `forwardAddr` back to relayer

**Per-token fees:** Each token forwarded requires a separate IGP fee. The `max_igp_fee` is the maximum fee the relayer will pay *per token*. If forwarding 3 tokens, the relayer may pay up to 3x the max fee (but only the actual quoted fee for each).

**Fee on failure:** If warp transfer fails after IGP fee is collected, the fee is sent to the `fee_collector` module account (protocol revenue distributed to stakers). This incentivizes relayers to verify route availability before submitting. Failed tokens are returned to the forwarding address.

## Queries

### DeriveForwardingAddress

```bash
celestia-appd query forwarding derive-address 42161 \
  0x000000000000000000000000<recipient-address>
```

### QuoteForwardingFee

Returns the estimated IGP fee for forwarding TIA to a destination domain.

```bash
celestia-appd query forwarding quote-fee 42161
```

## CLI Usage

```bash
# Query derived address
celestia-appd query forwarding derive-address 42161 \
  0x000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef

# Query IGP fee estimate
celestia-appd query forwarding quote-fee 42161

# Check balance at forward address
celestia-appd query bank balances <forward-addr>

# Execute forwarding (forwards ALL tokens at address)
celestia-appd tx forwarding forward <forward-addr> 42161 \
  0x000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef \
  --max-igp-fee 1000utia --from relayer
```

**Parameter Formats:**

- `dest-domain`: uint32 domain ID (e.g., `1` for Ethereum mainnet, `42161` for Arbitrum)
- `dest-recipient`: 32-byte hex-encoded address with `0x` prefix. For EVM chains, use the 20-byte address left-padded with 12 zero bytes (e.g., `0x000000000000000000000000<20-byte-eth-address>`)
- `max-igp-fee`: Maximum IGP fee to pay per token (e.g., `1000utia`)

## Error Codes

| Code | Name | Description |
|------|------|-------------|
| 2 | ErrAddressMismatch | Derived address doesn't match provided address |
| 3 | ErrNoBalance | No balance at forwarding address |
| 4 | ErrBelowMinimum | Balance below minimum threshold |
| 5 | ErrUnsupportedToken | Token denom not supported for forwarding |
| 6 | ErrTooManyTokens | Too many tokens at forwarding address |
| 7 | ErrInvalidRecipient | Invalid recipient length |
| 8 | ErrNoWarpRoute | No warp route to destination domain |
| 9 | ErrInsufficientIgpFee | IGP fee provided is less than required |

## Security

- **Cryptographic binding**: The `forwardAddr` cryptographically commits to `(destDomain, destRecipient)`. Funds can only be forwarded to the committed destination.
- **Permissionless execution**: Anyone can trigger forwarding, but only to the pre-committed destination.
- **No fund loss**: Failed tokens stay at `forwardAddr` or are automatically returned there.
- **Collision resistance**: Same as standard Cosmos addresses (160-bit truncation). Draining requires 2^160 operations (second preimage), not 2^80 (birthday attack).
- **Blocked module account**: The forwarding module account is blocked and cannot receive funds via direct `bank send` or `MsgSend`. This prevents accidental fund loss from users sending to the module account instead of a forwarding address.

## Recovery from Failed Forwards

If a warp transfer fails (e.g., due to a missing warp route), the tokens remain at the `forwardAddr`:

1. The forwarding attempt fails with a clear error (e.g., `ErrNoWarpRoute`)
2. Tokens stay at `forwardAddr` - they are never moved to the module account
3. Once the issue is resolved (e.g., warp route created), call `MsgForward` again
4. Tokens will be forwarded normally

### Why No Ante Handler Validation for Invalid Domains

It's not feasible to add an ante handler that rejects transactions to forwarding addresses with invalid domains. At arrival time, a forwarding address is indistinguishable from any standard Celestia address - it's just a deterministically derived account. The source chain would need to include metadata indicating "this is a forwarding tx" which adds engineering overhead across all sending chains.

The current design fails gracefully: if someone sends to a forwarding address with a non-existent domain, the tokens remain at that address and the `MsgForward` will fail with a clear `ErrNoWarpRoute` error. The funds are never lost.

## Test Vectors

For cross-platform compatibility (Go, TypeScript, Rust):

| destDomain | destRecipient | Expected Address |
|------------|---------------|------------------|
| 1 | `0x000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef` | `celestia1gev9segv9333lpy27thrtwdjwhu9lgcjpkpz2x` |
| 42161 | `0x0000000000000000000000001234567890abcdef1234567890abcdef12345678` | `celestia1uvqe9n0eclkd55dj9g9m30nf77px6jq2nfqmyw` |
| 0 | `0x0000000000000000000000000000000000000000000000000000000000000000` | `celestia1w0c30l5s7q46nhnz7k7j82j6kdsgz4w4m25jjg` |
