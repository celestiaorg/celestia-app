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

1. Frontend selects a trusted Hyperlane `tokenId` off-chain and computes `forwardAddr = derive(destDomain, destRecipient, tokenId)`
2. User sends tokens to forwardAddr via warp transfer or CEX withdrawal
3. Relayer detects deposit and submits `MsgForward`
4. Module verifies derivation and executes warp transfer to destination
5. Tokens arrive at destRecipient on destination chain

## Relayer

For relayer implementation details and operational guides, see the [forwarding-relayer documentation](https://github.com/celestiaorg/forwarding-relayer/blob/master/RELAYER.md).

## Address Derivation

Forwarding addresses are deterministically derived:

```text
callDigest   = sha256(destDomain_32bytes || destRecipient || tokenId)
salt         = sha256(version_byte || callDigest)       // version_byte = 0x02
forwardAddr  = address.Module("forwarding", salt)[:20]
```

Each address handles exactly one token for a given `(destDomain, destRecipient, tokenId)` tuple.

## State

### Params

```protobuf
message Params {
  // Currently empty; reserved for future governance-controlled parameters
}
```

Route selection is explicit and off-chain. The forwarding module does not infer a canonical token from the global warp registry.

## Messages

### MsgForward

Forwards the single token bound to a forwarding address to the committed destination.
The relayer (signer) pays both Celestia gas and Hyperlane IGP fees.

```protobuf
message MsgForward {
  string signer = 1;        // Relayer (pays gas + IGP fees)
  string forward_addr = 2;  // The derived forwarding address
  uint32 dest_domain = 3;   // Destination chain domain ID
  string dest_recipient = 4; // Recipient on destination (32 bytes, hex)
  string token_id = 5;      // Hyperlane token identifier bound to this address
  Coin max_igp_fee = 6;     // Max IGP fee relayer will pay
}

message MsgForwardResponse {
  string denom = 1;
  string amount = 2;
  string message_id = 3;  // Hyperlane message ID
}
```

## Trust Model

- The caller supplies `token_id` when deriving the address, quoting fees, and executing `MsgForward`.
- The chain verifies consistency with that explicit token choice.
- Frontends, wallets, or relayers are the source of trust for which token route should be used.
- The forwarding module never scans the permissionless warp registry to discover a token at execution time.

## Supported Token Types

| Source               | Denom Format          | Transfer Method          |
|----------------------|-----------------------|--------------------------|
| Warp transfer        | `hyperlane/{tokenId}` | Synthetic warp transfer  |
| CEX withdrawal (TIA) | `utia`                | Collateral warp transfer |

## Events

### EventTokenForwarded

| Attribute    | Description                            |
|--------------|----------------------------------------|
| forward_addr | The forwarding address                 |
| token_id     | Hyperlane token identifier             |
| denom        | Token denomination                     |
| amount       | Amount forwarded                       |
| message_id   | Hyperlane message ID                   |

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

**Fee on failure:** If the warp transfer fails, the token remains at `forwardAddr`, and the relayer is not charged the IGP fee for the failed attempt.

## Queries

### DeriveForwardingAddress

```bash
celestia-appd query forwarding derive-address 0x<token-id> \
  42161 \
  0x000000000000000000000000<recipient-address>
```

### QuoteForwardingFee

Returns the estimated IGP fee for forwarding the specified token to a destination domain.

```bash
celestia-appd query forwarding quote-fee 0x<token-id> 42161
```

## CLI Usage

```bash
# Query derived address
celestia-appd query forwarding derive-address 0x726f757465725f61707000000000000000000000000000010000000000000000 \
  42161 \
  0x000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef

# Query IGP fee estimate
celestia-appd query forwarding quote-fee \
  0x726f757465725f61707000000000000000000000000000010000000000000000 \
  42161

# Check balance at forward address
celestia-appd query bank balances <forward-addr>

# Execute forwarding
celestia-appd tx forwarding forward <forward-addr> \
  0x726f757465725f61707000000000000000000000000000010000000000000000 \
  42161 \
  0x000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef \
  --max-igp-fee 1000utia --from relayer
```

**Parameter Formats:**

- `dest-domain`: uint32 domain ID (e.g., `1` for Ethereum mainnet, `42161` for Arbitrum)
- `dest-recipient`: 32-byte hex-encoded address with `0x` prefix. For EVM chains, use the 20-byte address left-padded with 12 zero bytes (e.g., `0x000000000000000000000000<20-byte-eth-address>`)
- `token-id`: 32-byte Hyperlane token identifier in hex form
- `max-igp-fee`: Maximum IGP fee to pay for the bound token (e.g., `1000utia`)

## Error Codes

| Code | Name                  | Description                                    |
|------|-----------------------|------------------------------------------------|
| 2    | ErrAddressMismatch    | Derived address doesn't match provided address |
| 3    | ErrNoBalance          | No balance at forwarding address               |
| 4    | ErrBelowMinimum       | Balance below minimum threshold                |
| 5    | ErrUnsupportedToken   | Token denom not supported for forwarding       |
| 6    | ErrTooManyTokens      | Too many tokens at forwarding address          |
| 7    | ErrInvalidRecipient   | Invalid recipient length                       |
| 8    | ErrNoWarpRoute        | No warp route to destination domain            |
| 9    | ErrInsufficientIgpFee | IGP fee provided is less than required         |

## Security

- **Cryptographic binding**: The `forwardAddr` cryptographically commits to `(destDomain, destRecipient, tokenId)`. Funds can only be forwarded using the committed token route and destination.
- **Permissionless execution**: Anyone can trigger forwarding, but only to the pre-committed destination.
- **Off-chain route trust**: Frontends and relayers choose the token route; the chain enforces consistency but does not decide which token is canonical.
- **No fund loss**: Failed forwarding attempts do not commit state changes, so the bound token remains at `forwardAddr`.
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

| destDomain | destRecipient                                                        | tokenId                                                                | Expected Address                                  |
|------------|----------------------------------------------------------------------|------------------------------------------------------------------------|---------------------------------------------------|
| 1          | `0x000000000000000000000000deadbeefdeadbeefdeadbeefdeadbeefdeadbeef` | `0x726f757465725f61707000000000000000000000000000010000000000000000` | `celestia10r95aktjh073m6jf3n0tt9s3c4dppc8uun0nzg` |
| 42161      | `0x0000000000000000000000001234567890abcdef1234567890abcdef12345678` | `0x726f757465725f61707000000000000000000000000000010000000000000001` | `celestia18pu7000vcgwwdrcrf6y2ukepd6y9ln890p36t0` |
| 0          | `0x0000000000000000000000000000000000000000000000000000000000000000` | `0x726f757465725f61707000000000000000000000000000010000000000000002` | `celestia10cg94zshzuzturxq9ws24qfgf9lfxjgkuggugd` |
