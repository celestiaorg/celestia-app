# Fee Address (`pkg/feeaddress`)

## Overview

The fee address mechanism enables applications to contribute TIA to protocol
revenue via a dedicated module account. Funds sent to the fee address are
converted to transaction fees in the next block and distributed to delegators
as staking rewards.

Fee Address module account: `celestia18sjk23yldd9dg7j33sk24elwz2f06zt7ahx39y`

## Why `pkg/` and not `x/`?

This is NOT a full Cosmos SDK module. It has:

- No keeper with state
- No genesis
- No governance parameters
- No custom queries (fee address queryable via x/auth)

It's just types and constants. The logic lives in ante handlers (`app/ante/`)
and PrepareProposal/ProcessProposal.


## Architecture

```text
User sends utia to fee address
         │
         ▼
PrepareProposal: Check balance, inject MsgPayProtocolFee tx
         │
         ▼
ProcessProposal: Validate block includes correct protocol fee tx
         │
         ▼
Ante Handler: ProtocolFeeTerminatorDecorator deducts from fee address
         │
         ▼
Distribution: Fees go to fee_collector → delegators
```

### Why Protocol-Injected Tx (not BeginBlock)?

Dashboard compatibility. Blockworks, Celenium, and other analytics tools track
protocol revenue via transaction fees. BeginBlock transfers would be invisible
to block explorers. Protocol-injected tx makes fees visible and auditable.

### Why No-Op MsgServer?

The SDK's message router requires a handler for every registered message type.
When `MsgPayProtocolFee` is in a transaction:

1. Ante handlers run → `ProtocolFeeTerminatorDecorator` does all work
2. Message router calls handler → no-op because work already done

Without this handler, the router panics with "unknown message type". The
alternative would be SDK-level modifications to not route certain messages.

## Consensus Safety

### PrepareProposal

1. Reads fee address balance
2. If non-zero: creates `MsgPayProtocolFee` tx with fee = balance
3. Prepends to tx list (executes first)
4. Verifies tx wasn't filtered out (defense-in-depth)

**Error handling**: Returns error on failure. Impact: single validator fails
to propose. NOT a chain halt - other validators can still propose.

The protocol fee tx should NEVER be filtered because:

- No signatures to fail verification
- Minimal gas (40k, well under any limit)
- Positive fees

If the defense-in-depth check fails, it indicates a bug in the ante handler
chain, not normal operation.

### ProcessProposal Invariants

| Fee Address Balance | First Tx                    | Result |
|---------------------|-----------------------------|--------|
| Zero                | Anything but MsgPayProtocolFee | ACCEPT |
| Zero                | MsgPayProtocolFee           | REJECT |
| Non-zero            | Not MsgPayProtocolFee       | REJECT |
| Non-zero            | MsgPayProtocolFee (wrong)   | REJECT |
| Non-zero            | MsgPayProtocolFee (correct) | ACCEPT |

"Correct" means: fee amount = balance, gas = ProtocolFeeGasLimit, denom = utia

### Why Block-Level Validation (not just Ante Handler)?

Ante handler validates: "Is this tx's fee valid?"
ProcessProposal validates: "Does THIS BLOCK correctly handle fee address?"

These are different concerns:

- Ante handler runs in CheckTx/DeliverTx for individual transactions
- ProcessProposal validates block structure (did proposer correctly inject or
  omit the protocol fee tx based on fee address balance?)

## Security / Threat Model

### What can a malicious proposer do?

| Attack                   | Result                                              |
|--------------------------|-----------------------------------------------------|
| Steal fee address funds  | Impossible. No signers, funds → fee_collector only  |
| Inflate protocol revenue | Impossible. Fee MUST match balance exactly          |
| Censor protocol fee tx   | Detected. ProcessProposal rejects                   |
| Inject fake protocol fee | Rejected. No balance = no tx allowed                |

**Worst case**: Malicious validator proposes invalid blocks that get rejected.
Chain continues with honest proposers.

### Token Restrictions

Only utia allowed via direct transactions (MsgSend, MsgMultiSend).

**Known bypass vectors** (non-utia gets stuck, not stolen):

- Inbound IBC transfers
- ICA host messages
- Hyperlane MsgProcessMessage

Non-utia tokens sent via these paths will be permanently stuck at the fee
address. They cannot be forwarded (only utia is forwarded) and cannot be
recovered (no governance mechanism exists).

## Message Types

### MsgPayProtocolFee

Protocol-injected by block proposers. User submission rejected by ante handler.

```protobuf
message MsgPayProtocolFee {
  option (cosmos.msg.v1.signer) = "from_address";
  string from_address = 1;
}
```

The `from_address` annotation is required by SDK but signature verification
is skipped (`ProtocolFeeTerminatorDecorator` terminates ante chain early).

## Related

- Spec: [`specs/src/ante_handler_v7.md`](../../specs/src/ante_handler_v7.md)
- CIP: [CIP-43](https://github.com/celestiaorg/CIPs/blob/main/cips/cip-043.md)
