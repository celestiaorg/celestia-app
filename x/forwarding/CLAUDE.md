# CLAUDE.md - Forwarding Module

Agent context for reviewing `celestia-app/x/forwarding/`.

## Module Purpose

Single-signature cross-chain transfers through Celestia. Users send tokens to a deterministically derived `forwardAddr`, anyone can permissionlessly trigger forwarding to the committed destination.

## Address Derivation (Critical)

```go
func DeriveForwardingAddress(destDomain uint32, destRecipient []byte) sdk.AccAddress {
    // Step 1: Encode destDomain as 32-byte big-endian (right-aligned)
    destDomainBytes := make([]byte, 32)
    binary.BigEndian.PutUint32(destDomainBytes[28:], destDomain)

    // Step 2: callDigest = keccak256(abi.encode(destDomain, destRecipient))
    callDigest := crypto.Keccak256(append(destDomainBytes, destRecipient...))

    // Step 3: salt = keccak256("CELESTIA_FORWARD_V1" || callDigest)
    salt := crypto.Keccak256(append([]byte("CELESTIA_FORWARD_V1"), callDigest...))

    // Step 4: address = sha256("forwarding" || salt)[:20]
    hash := sha256.Sum256(append([]byte("forwarding"), salt...))
    return sdk.AccAddress(hash[:20])
}
```

**Key invariant**: `derive(destDomain, destRecipient) == forwardAddr` - this IS the authorization check.

## Security Properties

- **Permissionless execution**: Anyone with correct params can trigger forwarding
- **Cryptographic binding**: Funds can ONLY go to destination encoded in address
- **No theft possible**: Relayer/caller cannot redirect funds
- **destRecipient MUST be exactly 32 bytes** (validation critical)

## Module Structure

```
x/forwarding/
├── keeper/
│   ├── keeper.go       # Keeper with bankKeeper, warpKeeper, accountKeeper
│   ├── msg_server.go   # ExecuteForwarding handler (core logic)
│   └── query_server.go # DeriveForwardingAddress query
├── types/
│   ├── address.go      # DeriveForwardingAddress function
│   ├── msgs.go         # MsgExecuteForwarding validation
│   ├── errors.go       # ErrAddressMismatch, ErrNoBalance, etc.
│   └── expected_keepers.go # WarpKeeper interface
└── module.go           # AppModule registration
```

## Critical Implementation Patterns

### 1. Pre-Check Pattern (Before Moving Tokens)

Validate BEFORE `SendCoins` to keep failed tokens at `forwardAddr`:
```go
// PRE-CHECK 1: Find HypToken
hypToken, err := k.findHypTokenByDenom(ctx, denom)
if err != nil { return result }  // Token stays at forwardAddr

// PRE-CHECK 2: Verify warp route exists
hasRoute, _ := k.warpKeeper.EnrolledRouters.Has(ctx, ...)
if !hasRoute { return result }  // Token stays at forwardAddr

// NOW safe to SendCoins
```

### 2. Partial Failure (Intentional Design)

Multi-token forwarding processes each token independently:
- If USDC succeeds but WETH fails → tx succeeds, WETH stays at `forwardAddr`
- Failed tokens can be retried later
- Response contains per-token `ForwardingResult`

### 3. Recovery on Warp Failure

If pre-checks pass but warp transfer fails:
```go
if err != nil {
    // Return tokens to forwardAddr for retry
    k.bankKeeper.SendCoins(ctx, moduleAddr, forwardAddr, coins)
}
```

## Token Types

| Denom Format | Token Type | Transfer Method |
|--------------|------------|-----------------|
| `hyperlane/{tokenId}` | Synthetic | `RemoteTransferSynthetic()` |
| `utia` | Collateral | `RemoteTransferCollateral()` |

## Message Types

```protobuf
message MsgExecuteForwarding {
  string signer = 1;        // Relayer (pays gas)
  string forward_addr = 2;  // Derived address (bech32)
  uint32 dest_domain = 3;   // Hyperlane domain ID
  string dest_recipient = 4; // 32-byte hex recipient
}

message MsgExecuteForwardingResponse {
  repeated ForwardingResult results = 1; // Per-token results
}
```

## Warp Transfer Parameters

```go
k.warpKeeper.RemoteTransferSynthetic(
    ctx, hypToken, moduleAddr.String(),
    destDomain, destRecipient, amount,
    nil,                              // customHookId
    math.ZeroInt(),                   // gasLimit=0 (uses router default)
    sdk.NewCoin("utia", math.ZeroInt()), // maxFee
    nil,                              // customHookMetadata
)
```

## DoS Protection

- `MaxTokensPerForward = 20` - limits gas exhaustion from many small tokens

## Module Account

- Registered in `app.go` with `nil` permissions (no mint/burn)
- Holds tokens temporarily between `SendCoins` and warp transfer

## Review Checklist

- [ ] Address derivation matches exactly (keccak256, sha256, byte ordering)
- [ ] destRecipient validated as exactly 32 bytes before use
- [ ] Pre-checks happen BEFORE any `SendCoins`
- [ ] Failed warp transfers return tokens to `forwardAddr`
- [ ] Partial success handled correctly (some tokens fail, others succeed)
- [ ] Events emitted for each token result
- [ ] No panic on invalid inputs (return errors instead)

## Build & Test

```bash
# In celestia-app directory
go test ./x/forwarding/... -v
go build ./cmd/celestia-appd
```
