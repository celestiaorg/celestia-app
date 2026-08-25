# `x/fibre`

## Abstract

The `x/fibre` module handles payment for blobs published through [fibre](../../specs/src/fibre.md), Celestia's data availability protocol served by validator-operated fibre servers. It is the settlement layer of the protocol: users prepay into an escrow account, promise a payment when uploading a blob, and the promise is later settled on chain against their escrow.

The flow around the module:

1. A user deposits funds into their escrow account (`MsgDepositToEscrow`).
1. The [fibre client](../../specs/src/fibre_client.md) uploads the blob's shards to the validators' fibre servers together with a `PaymentPromise` signed by the escrow owner. Each server verifies its shards and the promise (via its app node) and endorses the promise with the validator's consensus key.
1. Once signatures representing more than 2/3 of the voting power are collected, a `MsgPayForFibre` carrying the promise and the signatures is submitted on chain, which deducts the payment from the escrow account (`PayForFibre` transactions are also referred to as "PFFs").
1. If a promise was handed out but never settled, anyone may submit `MsgPaymentPromiseTimeout` after the promise's timeout to settle it — so a promise is never free to issue.

Settled payments are routed to the fee collector and distributed like regular fees. The amount charged for a blob is `1 utia` per gas of `650,000 + 45,000 × ⌈blob_size / 256 KiB⌉` (see [`EstimateGasForPayForFibre`](./types/gas.go), the shared source of truth for the chain and the client-side escrow accounting).

Which host serves each validator's fibre traffic is tracked separately, in the [`x/valaddr`](../../x/valaddr/README.md) registry. The protocol-level specification of this module lives in [specs/src/fibre_module.md](../../specs/src/fibre_module.md).

The module is available starting from app version 10.

## State

- **Escrow accounts** — one per depositor, tracking `balance` (total funds held by the module) and `available_balance` (funds not locked by pending withdrawal requests).
- **Withdrawals** — pending withdrawal requests, indexed both by signer and by the time they become available.
- **Processed payments** — the hash and processing time of every settled promise, kept to reject replays. Records are pruned once they leave the retention window (`withdrawal_delay + 10min` clock skew), after which the promise itself can no longer validate.
- **Promise freshness floor** — a single timestamp that only moves forward; promises created before it are rejected, so a governance increase of `withdrawal_delay` can never resurrect an already-pruned promise.

Every block, the `BeginBlocker` advances the freshness floor, pays out withdrawal requests whose delay has elapsed, and prunes processed payments outside the retention window.

## Messages

### `MsgDepositToEscrow`

Transfers `amount` from the signer's account to the module and credits the signer's escrow account, creating it if needed. `amount` must be a valid positive coin.

### `MsgRequestWithdrawal`

Requests withdrawal of `amount` from the signer's escrow account. The amount is locked (deducted from `available_balance`) immediately and paid out automatically by the `BeginBlocker` once `withdrawal_delay` has elapsed. Rejected when the escrow account does not exist or its available balance is insufficient.

The delay exists so a user cannot hand out a payment promise and drain the escrow before the promise settles. For the same reason, if a payment arrives that the available balance cannot cover, pending withdrawals are reduced (oldest first) to cover it — the promise wins over the withdrawal.

### `MsgPayForFibre`

Settles a `PaymentPromise` against the promise signer's escrow account. The message carries the original promise and the endorsing validator signatures, and can be submitted by anyone (typically one of the endorsing validators). Validation, beyond the promise's own [stateless checks](./types/msgs.go):

1. The escrow-owner signature on the promise must verify.
1. The validator signatures must come from the validator set at the promise's `height` and represent more than 2/3 of the voting power.
1. The promise's `height` must be within `payment_promise_height_window` of the current height, its `creation_timestamp` fresh (not older than the freshness floor, not further than 10 minutes in the future), and the promise not yet expired or already processed.
1. The escrow account must exist and its total balance cover the payment.

Validator signatures (check 2) are verified in CheckTx and ProcessProposal only: a committed block already had them verified by honest validators, so FinalizeBlock skips the check and the state machine alone does not enforce it — the same trust model as blob share commitments. All other checks run at settlement on every node.

In practice the promise JSON and the signatures are produced by the fibre client's upload flow — a PFF is not constructed by hand. See [specs/src/fibre_module.md](../../specs/src/fibre_module.md) for the full promise format and verification rules.

### `MsgPaymentPromiseTimeout`

Settles a promise whose `payment_promise_timeout` has elapsed without a `MsgPayForFibre` landing. Anyone may submit it (no validator signatures required); the same freshness, replay, and balance rules as `MsgPayForFibre` apply, except expiry — which is required rather than rejected. This is the mechanism that charges for promises that were issued but whose upload never completed.

### `MsgUpdateFibreParams`

Updates the module parameters. Only the governance authority may submit it, and all parameters must be supplied.

## Events

The module emits typed events (the event type is the proto message name, e.g. `celestia.fibre.v1.EventPayForFibre`):

| Event                             | Fields                                             | Emitted on                       |
|-----------------------------------|----------------------------------------------------|----------------------------------|
| `EventDepositToEscrow`            | `signer`, `amount`                                 | deposit                          |
| `EventWithdrawFromEscrowRequest`  | `signer`, `amount`, `requested_at`, `available_at` | withdrawal request               |
| `EventWithdrawFromEscrowExecuted` | `signer`, `amount`                                 | withdrawal payout (BeginBlocker) |
| `EventPayForFibre`                | `signer`, `namespace`, `commitment`, `validator_count` | PFF settlement               |
| `EventPaymentPromiseTimeout`      | `processor`, `escrow_signer`, `payment_promise_hash` | timeout settlement             |
| `EventUpdateFibreParams`          | `signer`, `params`                                 | parameter update                 |
| `EventProcessedPaymentPruned`     | `payment_promise_hash`, `processed_at`             | retention pruning (BeginBlocker) |

## Parameters

| Key                        | Type            | Default            | Bounds                  |
|----------------------------|-----------------|--------------------|-------------------------|
| WithdrawalDelay            | time.Duration   | 24h                | [12h10m, 168h]          |
| PaymentPromiseTimeout      | time.Duration   | 1h                 | [10m, 12h]              |
| PaymentPromiseHeightWindow | uint64          | 1000               | > 0                     |
| ShardRetention             | time.Duration   | 4h                 | [10m, 168h]             |
| FullStakeStorageBudget     | uint64          | 2199023255552 (2 TiB) | > 0                  |

All parameters are changeable by governance. `WithdrawalDelay`'s lower bound is `MaxPaymentPromiseTimeout + 10m`, which guarantees every promise leaves a usable timeout-settlement window. `ShardRetention` is how long fibre servers keep uploaded shards on disk; `FullStakeStorageBudget` caps the fibre disk usage of a hypothetical 100%-stake validator over one retention window, from which each server derives its own stake-proportional budget. `PaymentPromiseHeightWindow` should stay well below the staking module's `HistoricalEntries` (default 10000): a promise whose height has been pruned from historical info can no longer be signature-verified, so such PFFs fail CheckTx on nodes that have not already verified them, risk split ProcessProposal votes, and can only settle via timeout.

## Usage

Deposit into your escrow account (the fibre client can also do this automatically):

```shell
celestia-appd tx fibre deposit-to-escrow 1000000utia --from mykey
```

Request a withdrawal (paid out automatically after `withdrawal_delay`):

```shell
celestia-appd tx fibre request-withdrawal 1000000utia --from mykey
```

Inspect state:

```shell
celestia-appd query fibre params
celestia-appd query fibre escrow-account <account-address>
celestia-appd query fibre withdrawals <account-address>
celestia-appd query fibre is-payment-processed <payment-promise-hash>
```

`tx fibre pay-for-fibre` and `tx fibre payment-promise-timeout` also exist, taking the promise as JSON; they are normally invoked by fibre infrastructure rather than by hand.

To publish blobs through fibre as a user, see the [fibre client quickstart](../../fibre/README.md).
