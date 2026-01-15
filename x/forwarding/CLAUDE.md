# CLAUDE.md - Forwarding Module

Agent context for reviewing `celestia-app/x/forwarding/`.

## Module Purpose

Single-signature cross-chain transfers through Celestia. Users send tokens to a deterministically derived `forwardAddr`, anyone can permissionlessly trigger forwarding to the committed destination.

## Address Derivation (Critical)

```go
func DeriveForwardingAddress(destDomain uint32, destRecipient []byte) ([]byte, error) {
    // Step 1: Encode destDomain as 32-byte big-endian (right-aligned)
    destDomainBytes := make([]byte, 32)
    binary.BigEndian.PutUint32(destDomainBytes[28:], destDomain)

    // Step 2: callDigest = sha256(destDomain || destRecipient)
    callDigest := sha256.Sum256(append(destDomainBytes, destRecipient...))

    // Step 3: salt = sha256(version_byte || callDigest)  // version_byte = 0x01
    salt := sha256.Sum256(append([]byte{ForwardVersion}, callDigest[:]...))

    // Step 4: address = address.Module("forwarding", salt)[:20]
    addr := address.Module(ModuleName, salt[:])
    return addr[:20], nil
}
```

**Key invariant**: `derive(destDomain, destRecipient) == forwardAddr` - this IS the authorization check.

### Derivation Pipeline

For SDK implementers ensuring cross-platform consistency:

```
        (destDomain, destRecipient)
                    │
                    ▼
   ┌─────────────────────────────────────┐
   │ destDomain → 32-byte big-endian     │
   │ (value at bytes 28-31)              │
   └─────────────────────────────────────┘
                    │
                    ▼
   ┌─────────────────────────────────────┐
   │ sha256(domainBytes || recipient)    │
   │              = callDigest           │
   └─────────────────────────────────────┘
                    │
                    ▼
   ┌─────────────────────────────────────┐
   │ sha256(0x01 || callDigest) = salt   │
   └─────────────────────────────────────┘
                    │
                    ▼
   ┌─────────────────────────────────────┐
   │ address.Module("forwarding", salt)  │
   │              [:20] = forwardAddr    │
   └─────────────────────────────────────┘
```

## Security Properties

- **Permissionless execution**: Anyone with correct params can trigger forwarding
- **Cryptographic binding**: Funds can ONLY go to destination encoded in address
- **No theft possible**: Relayer/caller cannot redirect funds
- **destRecipient MUST be exactly 32 bytes** (validation critical)

## State Machine Diagrams

### Token Lifecycle at ForwardAddr

Shows what happens to tokens deposited at a forwarding address. Key insight: tokens only leave `forwardAddr` on success - all failures keep tokens safe for retry.

```
                         ┌───────────┐
                         │   Empty   │
                         └─────┬─────┘
                               │ deposit (EVM warp / CEX)
                               ▼
                         ┌───────────┐
              ┌─────────▶│  Pending  │◀────────────┐
              │          └─────┬─────┘             │
              │                │ MsgExecuteForwarding
              │                ▼                   │
              │          ┌───────────┐             │
              │          │ Pre-Check │             │
              │          └─────┬─────┘             │
              │          pass  │   fail            │
              │       ┌────────┴────────┐          │
              │       ▼                 │          │
              │  ┌─────────┐            │          │
              │  │  Warp   │            │          │
              │  └────┬────┘            │          │
              │  pass │  fail           │          │
              │  ┌────┴────┐            │          │
              │  ▼         ▼            ▼          │
              │ ┌──────┐ ┌─────────────────┐       │
              │ │ Done │ │ Stays at Addr   │───────┘
              │ └──────┘ │ (retry later)   │  new deposit or
              │          └─────────────────┘  retry execution
              │
              │ new deposit to same addr
              └──────────────────────────────
```

### Per-Token Processing Flow

Details the pre-check pattern that ensures tokens never get stuck in the module account:

```
┌─────────────────────────────────────────────────────────────┐
│                  forwardSingleToken()                       │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌─────────┐   below    ┌─────────────────────────────────┐ │
│  │ Check   │───min─────▶│ SKIP: stays at forwardAddr      │ │
│  │ minimum │            └─────────────────────────────────┘ │
│  └────┬────┘                                                │
│       │ ok                                                  │
│       ▼                                                     │
│  ┌─────────┐  not found ┌─────────────────────────────────┐ │
│  │ Lookup  │───────────▶│ SKIP: stays at forwardAddr      │ │
│  │HypToken │            └─────────────────────────────────┘ │
│  └────┬────┘                                                │
│       │ found                                               │
│       ▼                                                     │
│  ┌─────────┐  no route  ┌─────────────────────────────────┐ │
│  │ Check   │───────────▶│ SKIP: stays at forwardAddr      │ │
│  │ route   │            └─────────────────────────────────┘ │
│  └────┬────┘                                                │
│       │ has route                                           │
│       ▼                                                     │
│  ┌─────────────────────┐                                    │
│  │ SendCoins to module │                                    │
│  └──────────┬──────────┘                                    │
│             ▼                                               │
│  ┌─────────────────────┐  fail  ┌───────────────────────┐   │
│  │ Warp Transfer       │───────▶│ Return to forwardAddr │   │
│  └──────────┬──────────┘        └───────────────────────┘   │
│             │ success                                       │
│             ▼                                               │
│  ┌─────────────────────┐                                    │
│  │ SUCCESS: forwarded  │                                    │
│  └─────────────────────┘                                    │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### End-to-End System Flow

Shows all component interactions from user intent to final delivery:

```
┌────────┐  ┌────────┐  ┌─────────┐  ┌─────────┐  ┌──────────┐  ┌───────────┐
│  User  │  │ Source │  │ Backend │  │ Relayer │  │ Celestia │  │   Dest    │
│        │  │ Chain  │  │         │  │         │  │          │  │   Chain   │
└───┬────┘  └───┬────┘  └────┬────┘  └────┬────┘  └────┬─────┘  └─────┬─────┘
    │           │            │            │            │              │
    │ 1. Request forward     │            │            │              │
    │──────────────────────▶ │            │            │              │
    │                        │            │            │              │
    │ ◀── forwardAddr ───────│            │            │              │
    │                        │            │            │              │
    │ 2. Deposit             │            │            │              │
    │──────▶│                │            │            │              │
    │       │                │            │            │              │
    │       │ 3. Warp transfer (EVM)      │            │              │
    │       │ ─ ─ ─ OR ─ ─ ─ ─ ─ ─ ─ ─ ─ ─│─ ─ ─ ─ ─ ▶│              │
    │       │ 3. CEX withdrawal           │            │              │
    │       │─────────────────────────────┼───────────▶│              │
    │       │                │            │            │              │
    │       │                │  4. Poll   │            │              │
    │       │                │◀───────────│            │              │
    │       │                │            │            │              │
    │       │                │            │ 5. Watch   │              │
    │       │                │            │───────────▶│              │
    │       │                │            │            │              │
    │       │                │            │ 6. MsgExecuteForwarding   │
    │       │                │            │───────────▶│              │
    │       │                │            │            │              │
    │       │                │            │            │ 7. Warp msg  │
    │       │                │            │            │─────────────▶│
    │       │                │            │            │              │
    │       │                │            │◀─ result ──│              │
    │       │                │            │            │              │
    │◀────────────────────── status ──────┤            │              │
    │                        │            │            │              │
```

**Flow summary:**
1. User requests forwarding address from frontend
2. User initiates deposit on source chain (EVM or CEX)
3. Tokens arrive at Celestia via Hyperlane warp OR CEX withdrawal
4. Relayer polls backend for registered intents
5. Relayer watches Celestia for deposits to known addresses
6. Relayer submits `MsgExecuteForwarding`
7. Module triggers outbound warp transfer to destination

### Multi-Token Batch Processing

Shows how multiple tokens at a forwarding address are processed independently. Key design: one failing token doesn't block others.

```
        MsgExecuteForwarding
                 │
                 ▼
    ┌────────────────────────┐
    │   GetAllBalances()     │
    │   [USDC, WETH, TIA]    │
    └───────────┬────────────┘
                │
      ┌─────────┼─────────┐
      ▼         ▼         ▼
  ┌───────┐ ┌───────┐ ┌───────┐
  │ USDC  │ │ WETH  │ │  TIA  │
  │process│ │process│ │process│
  └───┬───┘ └───┬───┘ └───┬───┘
      │         │         │
      ▼         ▼         ▼
   SUCCESS   FAILED    SUCCESS
  (forward) (no route) (forward)
      │         │         │
      └─────────┴─────────┘
                │
                ▼
    ┌────────────────────────┐
    │ Response:              │
    │  USDC: ✓ msgId=0x...   │
    │  WETH: ✗ "no route"    │
    │  TIA:  ✓ msgId=0x...   │
    └────────────────────────┘
```

WETH stays at `forwardAddr` for retry when a route is added.

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

Multi-token forwarding intentionally allows partial failures:
- **Permissionless retry**: Failed tokens stay at `forwardAddr` for later retry
- **Progressive forwarding**: New warp routes can forward previously unsupported tokens
- **No stuck transactions**: One bad token doesn't block others

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

- [ ] Address derivation matches exactly (sha256 throughout, byte ordering)
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
