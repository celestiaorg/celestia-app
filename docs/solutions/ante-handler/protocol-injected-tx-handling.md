---
title: Handling Protocol-Injected Transactions in Ante Handler
category: ante-handler
tags: [ante-handler, protocol-injected, unsigned-tx, cosmos-sdk, fee-forwarding]
created: 2026-01-22
symptoms:
  - "Complex wrapper pattern needed to skip decorators"
  - "Transaction has no signers but needs to pass ante handler"
  - "ValidateBasic fails for unsigned protocol messages"
---

# Handling Protocol-Injected Transactions in Ante Handler

## Symptoms

When implementing protocol-injected transactions (like fee forwarding):
- Need to skip signature verification (no signers)
- Need to skip sequence increment (no signers)
- Need custom fee deduction (from module account, not signer)
- Standard ante chain assumptions break down

Initial attempt might look like:
```go
// Complex wrapper approach (DON'T DO THIS)
NewEarlyFeeForwardDetector()  // Sets context flag
NewSkipForFeeForwardDecorator(ValidateBasicDecorator)
NewSkipForFeeForwardDecorator(SigVerificationDecorator)
// ... many wrapped decorators
```

## Root Cause

Standard Cosmos SDK ante handlers assume:
1. Every tx has at least one signer
2. Fees come from the first signer
3. Signatures exist and need verification
4. Sequence numbers need incrementing

Protocol-injected transactions (like `MsgForwardFees`) break all these assumptions:
- No signers (injected by block proposer in PrepareProposal)
- Fees come from a module/special address
- No signatures to verify
- No sequence to increment

## Solution

Use a **single terminating decorator** placed early in the chain that:
1. Detects the special transaction type
2. Does all necessary validation and state changes
3. Returns early WITHOUT calling `next()` - skipping rest of chain

```go
// FeeForwardTerminatorDecorator handles MsgForwardFees completely and
// terminates the ante chain early.
type FeeForwardTerminatorDecorator struct {
    bankKeeper FeeForwardBankKeeper
}

func (d FeeForwardTerminatorDecorator) AnteHandle(
    ctx sdk.Context,
    tx sdk.Tx,
    simulate bool,
    next sdk.AnteHandler,
) (sdk.Context, error) {
    msg := IsFeeForwardMsg(tx)
    if msg == nil {
        // Not our tx type - continue with normal ante chain
        return next(ctx, tx, simulate)
    }

    // Reject user submissions (only valid when protocol-injected)
    if ctx.IsCheckTx() || ctx.IsReCheckTx() || simulate {
        return ctx, errors.Wrap(sdkerrors.ErrInvalidRequest,
            "cannot be submitted by users; protocol-injected only")
    }

    // Validate and execute
    feeTx, ok := tx.(sdk.FeeTx)
    if !ok {
        return ctx, errors.Wrap(sdkerrors.ErrInvalidRequest, "must implement FeeTx")
    }

    // Do your validation...
    if err := ValidateFee(feeTx.GetFee()); err != nil {
        return ctx, err
    }

    // Do your state changes (fee transfer, etc.)
    err := d.bankKeeper.SendCoinsFromAccountToModule(
        ctx, SpecialAddress, authtypes.FeeCollectorName, feeTx.GetFee())
    if err != nil {
        return ctx, errors.Wrap(err, "failed to transfer fee")
    }

    // Store data for message handler if needed
    ctx = ctx.WithValue(FeeAmountContextKey{}, feeTx.GetFee())

    // TERMINATE - don't call next()
    return ctx, nil
}
```

### Ante Chain Placement

```go
func NewAnteHandler(...) sdk.AnteHandler {
    return sdk.ChainAnteDecorators(
        ante.NewSetUpContextDecorator(),
        // Put terminator EARLY, right after setup
        ante.NewFeeForwardTerminatorDecorator(bankKeeper),
        // Rest of normal decorators...
        ante.NewValidateBasicDecorator(),
        ante.NewSigVerificationDecorator(...),
        // ...
    )
}
```

## Why This Works

| Aspect | Wrapper Pattern | Terminator Pattern |
|--------|-----------------|-------------------|
| Code complexity | High (wrappers, context flags) | Low (single decorator) |
| Maintenance | Hard (track which decorators wrapped) | Easy (self-contained) |
| Understanding | Confusing (implicit skipping) | Clear (explicit early return) |
| Testing | Complex (mock context flags) | Simple (test one decorator) |

## Prevention

When implementing protocol-injected transactions:

1. **Design for early termination**: Protocol messages should be fully handled by a single decorator
2. **Don't force-fit unsigned txs through signed-tx decorators**: Skip them entirely
3. **Keep it explicit**: A single decorator that does everything is clearer than scattered skip logic
4. **Reject in CheckTx/ReCheckTx**: Ensure users can't submit protocol-only messages
5. **Validate in ProcessProposal**: Double-check protocol messages during block validation

## Related

- [Cosmos SDK Ante Handler Docs](https://docs.cosmos.network/main/learn/advanced/ante-handlers)
- [CIP-43: Fee Forwarding](https://github.com/celestiaorg/CIPs/blob/main/cips/cip-43.md)
- PrepareProposal/ProcessProposal for protocol message injection
