# `x/fibre`

## Abstract

The `x/fibre` payment mechanism enables users to pay for fibre blobs without waiting for a transaction to be confirmed. This is done by users depositing funds into [escrow accounts](#escrow-accounts), and signing over offchain messages that can be moved onchain at a later point.

## Contents

1. [Abstract](#abstract)
1. [State](#state)
1. [Messages](#messages)
1. [Automatic State Transitions](#automatic-state-transitions)
1. [Events](#events)
1. [Queries](#queries)
1. [Parameters](#parameters)
1. [Client](#client)

## Abstract

DoS resistance for a protocol with a global limit on throughput requires a guarantee for payment. Normally this is done simply by paying for gas, however paying for gas requires waiting for a transaction to be confirmed. The payment portion of this module (mainly the [`PaymentPromise`](#msgpayforfibre) and [`EscrowAccount`](#escrow-accounts)) is to provide a guarantee for payment without having to wait for a transaction to be confirmed.

Therefore, it is an invariant of the payment system that a signed [`PaymentPromise`](#msgpayforfibre) guarantees payment.

## State

The fibre module maintains state for [escrow accounts](#escrow-accounts), [pending withdrawals](#pending-withdrawals), and module [parameters](#parameters).

### Params

```proto
message Params {
  option (gogoproto.goproto_stringer) = false;
  uint32 gas_per_blob_byte = 1
      [ (gogoproto.moretags) = "yaml:\"gas_per_blob_byte\"" ];
  google.protobuf.Duration withdrawal_delay = 2
      [ (gogoproto.moretags) = "yaml:\"withdrawal_delay\"" ];
  google.protobuf.Duration promise_timeout = 3
  [ (gogoproto.moretags) = "yaml:\"promise_timeout\"" ];
}
```

#### `GasPerBlobByte`

`GasPerBlobByte` is the amount of gas consumed per byte of blob data when payment is processed. This determines the gas cost for fibre blob inclusion.

#### `WithdrawalDelay`

`WithdrawalDelay` is the duration that must pass between requesting a withdrawal and when funds become available for withdrawal (default: 24 hours). This value is also used for pruning [ProcessedPromise](#processed-promises) from the state.

#### `PromiseTimeout`

`PromiseTimeout` is the duration after which anyone can submit a promise for processing if the user hasn't submitted a [`MsgPayForFibre`](#msgpayforfibre) (default: 1 hour).

### Escrow Accounts

Escrow accounts help guarantee payment for a signed [`PaymentPromise`](#msgpayforfibre) by ensuring that a user does not remove funds directly after validators sign over and provide service for a blob. Each user can only have one escrow account, indexed by their signer address.

```proto
message EscrowAccount {
  // signer is the address that controls this escrow account
  string signer = 1;
  // balance is the total amount currently held in escrow
  cosmos.base.v1beta1.Coin balance = 2;
  // available_balance is the amount available for new payments
  cosmos.base.v1beta1.Coin available_balance = 3;
}
```

### Pending Withdrawals

Withdrawal requests are tracked to implement the delay mechanism.

```proto
message PendingWithdrawal {
  // signer is the address that owns the escrow account this withdrawal is for
  string signer = 1;
  // amount is the amount to be withdrawn
  cosmos.base.v1beta1.Coin amount = 2;
  // requested_at is the timestamp when withdrawal was requested
  google.protobuf.Timestamp requested_at = 3;
}
```

### Processed Promises

To prevent double payment, the module tracks which promises have been processed. Only the processing timestamp is stored, indexed by the promise hash.

#### Indexing

**Escrow Accounts**:
- **Primary Index**: `escrows/{signer}` → `EscrowAccount`

**Pending Withdrawals**:
- **By signer**: `withdrawals/{signer}/{requested_at}` → `cosmos.base.v1beta1.Coin` (amount)
- **By Availability**: `available_withdrawals/{available_at}/{signer}` → `cosmos.base.v1beta1.Coin` (amount)

**Processed Promises**:
- **Primary Index**: `processed/{promise_hash}` → `google.protobuf.Timestamp` (processed_at)
- **By Timestamp**: `pruning/{processed_at}/{promise_hash}` → `null` (for pruning)

#### Pruning Mechanism

Processed promises are automatically pruned after [`withdrawal_delay`](#withdrawaldelay) to prevent unbounded state growth. See [Automatic Promise Pruning](#automatic-promise-pruning) for implementation details.

## Messages

### Gas Consumption

All messages use the existing gas consumption mechanism in the cosmos-sdk. In addition to the standard resource pricing, the messages that deduct fees for blobs, `MsgPayForFibre` and `MsgPaymentTimeout`, manually add gas consumption based on blob size.

**Blob Gas Calculation**:

Gas cost is calculated using the following formula:
```
total_gas = (rows * row_size(blob_size) * gas_per_blob_byte)
```

This means that users pay for padding as well, just like PFBs.

Where:
- `rows` is the constant number of rows needed for the blob data
- `row_size(blob_size)` is the size of each row in bytes
- `gas_per_blob_byte` is the gas cost per byte parameter

### MsgDepositToEscrow

Deposits funds to the signer's escrow account. If no escrow account exists for the signer, one will be created automatically. Deposits are processed instantly.

```proto
message MsgDepositToEscrow {
  // signer is the bech32 encoded signer address
  string signer = 1;
  // amount is the amount to deposit
  cosmos.base.v1beta1.Coin amount = 2;
}
```

#### Validation and Processing

**Stateless Validation**:
- Signer address must be valid
- Amount must be positive

**Stateful Processing**:
1. If signer's escrow account doesn't exist, create one with zero balance
2. Transfer funds from signer to module account
3. Increase both balance and available_balance by deposit amount
4. Emit EventDepositToEscrow

### MsgRequestWithdrawal

Requests withdrawal from the signer's escrow account. Funds become available after the withdrawal delay.

```proto
message MsgRequestWithdrawal {
  // signer is the bech32 encoded signer address
  string signer = 1;
  // amount is the amount to withdraw
  cosmos.base.v1beta1.Coin amount = 2;
}
```

#### Validation and Processing

**Stateless Validation**:
- Signer address must be valid
- Amount must be positive

**Stateful Processing**:
1. Verify signer's escrow account exists
2. Verify sufficient available balance
3. Verify no existing withdrawal request at current timestamp (prevent key collision)
4. Decrease available_balance immediately (balance remains unchanged until withdrawal is processed)
5. Store withdrawal amount in both indexes with available_at = current_timestamp + withdrawal_delay
6. Emit EventRequestWithdrawalFromEscrow

#### Automatic Withdrawal Processing

Withdrawals are automatically processed in `BeginBlocker` when `current_time >= withdrawal.available_at`:

```go
func processAvailableWithdrawals(ctx sdk.Context, k Keeper) {
    currentTime := ctx.BlockTime()

    // Iterate over available_withdrawals index starting from earliest timestamp
    iterator := k.GetAvailableWithdrawalsIterator(ctx, currentTime)
    defer iterator.Close()

    for ; iterator.Valid(); iterator.Next() {
        // Parse key to extract available_at timestamp and signer address
        available_at, signer := k.ParseAvailableWithdrawalKey(iterator.Key())

        // Stop if we've reached withdrawals not yet available
        if available_at.After(currentTime) {
            break
        }

        // Get withdrawal amount from value
        var amount cosmos.base.v1beta1.Coin
        k.cdc.Unmarshal(iterator.Value(), &amount)

        // Process withdrawal: transfer from module to user account
        err := k.bankKeeper.SendCoinsFromModuleToAccount(
            ctx, types.ModuleName, signer, amount)
        if err != nil {
            // Log error but continue processing other withdrawals
            ctx.Logger().Error("failed to process withdrawal", "error", err, "signer", signer)
            continue
        }

        // Update escrow account balance (decrease total balance)
        escrow := k.GetEscrowAccount(ctx, signer)
        escrow.balance = escrow.balance.Sub(amount)
        k.SetEscrowAccount(ctx, escrow)

        // Remove from both withdrawal indexes
        requested_at := available_at.Add(-k.GetWithdrawalDelay(ctx))
        k.DeletePendingWithdrawal(ctx, signer, requested_at)
        k.DeleteAvailableWithdrawal(ctx, available_at, signer)

        // Emit event
        ctx.EventManager().EmitEvent(EventProcessWithdrawal{
            processor: types.AutomaticProcessor, // system account
            signer:    signer,
            amount:    amount,
        })
    }
}
```

### MsgPayForFibre

Contains the original payment promise with validator signatures, submitted by the user. Successful `MsgPayForFibre` transactions are included in their own reserved namespace. The commitment from the promise is also included in the data square in the namespace specified in the promise.

```proto
message MsgPayForFibre {
  // signer is the bech32 encoded address submitting this message
  string signer = 1;
  // promise contains the original payment promise
  PaymentPromise promise = 2;
  // validator_signatures contains signatures from validators
  repeated bytes validator_signatures = 3;
}

message PaymentPromise {f
  // signer is the owner of the escrow account to charge
  string signer = 1;
  // namespace is the namespace the blob is associated with. share version must be 2.
  bytes namespace = 2;
  // blob_size is the size of the blob in bytes
  uint32 blob_size = 3;
  // commitment is the hash of the row root and the Random Linear Combination (RLC) root
  bytes commitment = 4;
  // row_version is the version of the row format
  uint32 row_version = 5;
  // valset_height is the height that is used to determine the validator set.
  int64 valset_height = 6;
  // creation_timestamp is the timestamp when this promise was created. This
  // is critical for determining which validators sign the commitment and
  // determining when service stops for this blob.
  google.protobuf.Timestamp creation_timestamp = 7;
  // signature is the escrow owner's signature over the sign bytes
  bytes signature = 8;
}
```

#### PaymentPromise Validation

**Stateless Validation**:
- `signer` must be valid bech32 address
- `namespace` must be valid
- `blob_size` must be positive
- `commitment` must be 32 bytes
- `row_version` must be supported version
- `valset_height` must be positive
- `creation_timestamp` must be positive
- `signature` must be properly formatted and non-empty

**Gas Consumption**:

Gas cost is calculated as described in the [Gas Consumption](#gas-consumption) section.

**Stateful Validation**:
1. Verify `creation_timestamp` is:
  - less than or equal to current confirmed timestamp
  - greater than (current_timestamp - withdrawal_delay)

2. Verify escrow account exists for `signer`
3. Verify sufficient available balance for gas cost (see [Gas Consumption](#gas-consumption) section). This includes all yet to be processed `PaymentPromises` that the validator has signed over.
4. Verify promise signature by escrow owner over promise sign bytes (see [Sign Bytes Format](#sign-bytes-format) below)
5. Verify promise hasn't been processed already

#### Sign Bytes Format

The sign bytes for a PaymentPromise signature are constructed by concatenating all fields except the `signature` field, along with prepending the chainID:

```
sign_bytes = chainID || signer_bytes || namespace || blob_size_bytes || commitment || row_version_bytes || valset_height_bytes || creation_timestamp_bytes
```

**Field Encoding**:
- `signer`: raw bytes of signer address secp256k1 (20 bytes)
- `namespace`: Raw namespace bytes (fixed 29 bytes)
- `blob_size_bytes`: Big-endian encoded uint32 (4 bytes)
- `commitment`: Raw commitment bytes (32 bytes)
- `row_version_bytes`: Big-endian encoded uint32 (4 bytes)
- `valset_height_bytes`: Big-endian encoded int64 (8 bytes)
- `creation_timestamp_bytes`: Protobuf-encoded google.protobuf.Timestamp (variable length)

**Total Length**: Variable length (20 + 29 + 4 + 32 + 4 + 8 + timestamp_bytes)

#### MsgPayForFibre Validation and Processing

**Stateless Validation**:
- Must have at least one validator signature
- All validator signatures must be properly formatted

**Stateful Processing**:
1. Validate PaymentPromise (see [PaymentPromise Validation](#paymentpromise-validation) above)
2. Verify validator signatures represent 2/3+ threshold from validator set at `promise.valset_height` (obtained via historical info query from staking module):
   - Signatures must represent 2/3+ of total voting power AND 2/3+ of validator count
3. Calculate gas cost (see [Gas Consumption](#gas-consumption) section) and deduct from both escrow balance and available_balance
4. Mark promise as processed (stores `processed_at` timestamp and creates pruning index entry)
5. Include commitment in data square (see [Inclusion Processing](#inclusion-processing) below)
6. Emit EventPayForFibre

#### Inclusion Processing

When processing a successful `MsgPayForFibre`, the commitment must be included in the data square:

1. Extract the namespace from `promise.namespace`
2. Place the commitment as the sole data in the specified namespace
3. The commitment data is included as a single blob in the namespace

### MsgPaymentTimeout

Processes a payment promise after the timeout period if no `MsgPayForFibre` was submitted. This mechanism is critical to guaranteeing that payment occurs. `MsgPaymentTimeout` transactions are included in the default transaction reserved namespace.

```proto
message MsgPaymentTimeout {
  // signer is the bech32 encoded address submitting this message (can be anyone)
  string signer = 1;
  // promise contains the original payment promise
  PaymentPromise promise = 2;
}
```

#### MsgPaymentTimeout Validation and Processing

**Stateless Validation**:
- All [PaymentPromise](#paymentpromise-validation) stateless validation applies (including signature validation)

**Stateful Processing**:
1. Validate PaymentPromise (see [PaymentPromise Validation](#paymentpromise-validation) above)
2. Verify `promise.creation_timestamp + promise_timeout <= current_timestamp` (timeout has passed)
3. Calculate gas cost (see [Gas Consumption](#gas-consumption) section) and deduct from both escrow balance and available_balance
4. Mark promise as processed (stores `processed_at` timestamp and creates pruning index entry)
5. DO NOT include commitment in data square (since no validator consensus was reached)
6. Emit EventProcessPromiseTimeout

#### Automatic Promise Pruning

Processed promises are automatically pruned in `BeginBlocker` when `current_time >= processed_at + withdrawal_delay` to prevent unbounded state growth:

```go
func pruneProcessedPromises(ctx sdk.Context, k Keeper) {
    currentTime := ctx.BlockTime()
    pruneThreshold := currentTime.Add(-k.GetWithdrawalDelay(ctx))

    // Iterate over pruning index starting from earliest timestamp
    iterator := k.GetPruningIterator(ctx, pruneThreshold)
    defer iterator.Close()

    for ; iterator.Valid(); iterator.Next() {
        // Extract processed_at timestamp and promise_hash from key
        processed_at, promise_hash := k.ParsePruningKey(iterator.Key())

        // Stop if we've reached promises not yet eligible for pruning
        if processed_at.After(pruneThreshold) {
            break
        }

        // Remove from both indexes atomically
        k.DeleteProcessedPromise(ctx, promise_hash)
        k.DeletePruningEntry(ctx, processed_at, promise_hash)
    }
}
```

## Transaction Flow

The Fibre blob submission follows this flow:

```mermaid
sequenceDiagram
    participant C as Client
    participant S as Server/Validator
    participant A as Celestia-App

    Note over C,A: Setup Phase
    C->>A: MsgDepositToEscrow

    Note over C,A: Promise Creation & Data Distribution
    C->>C: Create signed PaymentPromise
    C->>S: Send data chunks + PaymentPromise

    Note over S,A: Validator Verification
    S->>A: QueryValidatePaymentPromise(promise, signature)
    A-->>S: ValidationResponse (valid, balance check, etc.)

    alt Promise is valid
        S->>S: Sign commitment
        S-->>C: Return validator signature
    else Promise is invalid
        S-->>C: Reject request
    end

    Note over C,A: Happy Path - Payment Confirmation
    C->>C: Collect 2/3+ validator signatures
    C->>A: MsgPayForFibre(promise, validator_signatures)
    A->>A: Deduct payment from escrow
    A->>A: Include commitment in data square

    Note over C,A: Fallback - Timeout Processing
    alt User doesn't submit within timeout
        C->>A: MsgPaymentTimeout(promise, signature)
        A->>A: Deduct payment from escrow
        Note right of A: No data square inclusion
    end

    Note over C,A: Withdrawal Flow
    C->>A: MsgRequestWithdrawal(signer, amount)
    A->>A: Decrease available_balance immediately

    Note over C,A: After withdrawal delay
    A->>A: Transfer funds to user account
```

### Flow Description

1. **Setup Phase**: User deposits funds using [`MsgDepositToEscrow`](#msgdeposittoescrow), which creates an escrow account if one doesn't exist.

2. **Promise Creation**: User creates a signed [`PaymentPromise`](#msgpayforfibre) containing escrow details, commitment, validator set height, and creation timestamp.

3. **Data Distribution Phase**: User distributes data chunks to validators along with the signed promise.

4. **Validator Verification**: Validators query the celestia-app instance using [`QueryValidatePaymentPromise`](#validatepaymentpromise) to verify the promise signature, check escrow has sufficient funds, and confirm the promise hasn't been processed. If valid, validators sign over the commitment.

5. **Payment Confirmation (Happy Path)**: User collects 2/3+ validator signatures and submits [`MsgPayForFibre`](#msgpayforfibre) containing the promise and signatures. The commitment gets included in the data square.

6. **Timeout Processing (Fallback)**: If user doesn't submit [`MsgPayForFibre`](#msgpayforfibre) within `promise_timeout_blocks`, anyone can submit [`MsgPaymentTimeout`](#msgpaymenttimeout) to process payment. This prevents the user from getting free service.

7. **Withdrawal**: Users can request withdrawals via [`MsgRequestWithdrawal`](#msgrequestwithdrawal) (decreases available balance immediately) and process them after the delay (which decreases total balance and transfers funds to user). Processing occurs automatically in BeginBlocker (see [Automatic Withdrawal Processing](#automatic-withdrawal-processing)).

## Automatic State Transitions

The fibre module requires automatic processing in `BeginBlocker` to handle time-based state transitions that cannot rely on user-submitted transactions. Two key operations must occur automatically:

1. **Withdrawal Processing**: Transfer funds from escrow to user accounts when withdrawal delay expires (see [Automatic Withdrawal Processing](#automatic-withdrawal-processing))
2. **Promise Pruning**: Remove old processed promises to prevent unbounded state growth (see [Automatic Promise Pruning](#automatic-promise-pruning))

### BeginBlocker Implementation

```go
func BeginBlocker(ctx sdk.Context, k Keeper) {
    // Process available withdrawals first (affects escrow balances)
    processAvailableWithdrawals(ctx, k)

    // Prune old processed promises (cleanup operation)
    pruneProcessedPromises(ctx, k)
}
```

## Events

### Escrow Events

#### `EventDepositToEscrow`

| Attribute Key | Attribute Value                    |
|---------------|------------------------------------|
| signer        | {bech32 encoded signer address}    |
| amount        | {deposit amount}                   |

#### `EventRequestWithdrawalFromEscrow`

| Attribute Key | Attribute Value                    |
|---------------|------------------------------------|
| signer         | {bech32 encoded signer address}     |
| amount        | {withdrawal amount}                |
| available_at  | {timestamp when available}          |

#### `EventProcessWithdrawal`

| Attribute Key | Attribute Value                    |
|---------------|------------------------------------|
| processor     | {bech32 encoded processor address} |
| signer        | {bech32 encoded escrow owner}      |
| amount        | {withdrawal amount}                |

#### `EventPayForFibre`

| Attribute Key | Attribute Value                      |
|---------------|--------------------------------------|
| signer        | {bech32 encoded submitter address}   |
| signer  | {bech32 encoded escrow owner}        |
| namespace     | {namespace the blob is published to} |
| validator_count | {number of validator signatures}   |

#### `EventProcessPromiseTimeout`

| Attribute Key | Attribute Value                      |
|---------------|--------------------------------------|
| processor     | {bech32 encoded processor address}   |
| signer  | {bech32 encoded escrow owner}        |
| promise_hash  | {hash for the promise that is being timed out} |

## Queries

### EscrowAccount

Queries an [escrow account](#escrow-accounts) by ID.

**Request**:
```proto
message QueryEscrowAccountRequest {
  string signer = 1;
}
```

**Response**:
```proto
message QueryEscrowAccountResponse {
  EscrowAccount escrow_account = 1;
  bool found = 2;
}
```

### PendingWithdrawals

Queries [pending withdrawals](#pending-withdrawals) for an escrow account.

**Request**:
```proto
message QueryPendingWithdrawalsRequest {
  string signer = 1;
  cosmos.base.query.v1beta1.PageRequest pagination = 2;
}
```

**Response**:
```proto
message QueryPendingWithdrawalsResponse {
  repeated PendingWithdrawal pending_withdrawals = 1;
  cosmos.base.query.v1beta1.PageResponse pagination = 2;
}
```

### ProcessedPromise

Queries whether a [promise](#processed-promises) has been processed.

**Request**:
```proto
message QueryProcessedPromiseRequest {
  bytes promise_hash = 1;
}
```

**Response**:
```proto
message QueryProcessedPromiseResponse {
  google.protobuf.Timestamp processed_at = 1;
  bool found = 2;
}
```

### ValidatePaymentPromise

Validates a [payment promise](#msgpayforfibre) for server use, performing all required checks including escrow balance and processing status.

**Request**:
```proto
message QueryValidatePaymentPromiseRequest {
  PaymentPromise promise = 1;
}
```

**Response**:
```proto
message QueryValidatePaymentPromiseResponse {
  bool valid = 1;
  string error_message = 2;
  bool sufficient_balance = 3;
  bool already_processed = 4;
  cosmos.base.v1beta1.Coin required_payment = 5;
  cosmos.base.v1beta1.Coin available_balance = 6;
}
```

**Validation Checks**:
1. Verify escrow account exists and has sufficient available balance for the gas cost (see [Gas Consumption](#gas-consumption) section)
2. Verify promise hasn't been processed already
3. Perform all standard PaymentPromise validation (see [PaymentPromise Validation](#paymentpromise-validation) section)

## Parameters

| Key                | Type                        | Default    | Description |
|--------------------|-----------------------------|-----------:|-------------|
| GasPerBlobByte     | uint32                      | 8          | Gas cost per byte of blob data |
| WithdrawalDelay    | google.protobuf.Duration    | 24h        | Duration to wait before withdrawal |
| PromiseTimeout     | google.protobuf.Duration    | 1h         | Duration before promise can be processed by timeout |

## Client

### CLI

#### Transactions

```shell
# Deposit to escrow account (creates escrow if it doesn't exist)
celestia-appd tx fibre deposit-to-escrow <amount> [flags]

# Request withdrawal from escrow
  celestia-appd tx fibre request-withdrawal <amount> [flags]


# Generate signed promise for validators
celestia-appd tx fibre create-promise <namespace> <blob_size> <commitment> [flags]

# Submit payment with validator signatures
celestia-appd tx fibre pay-for-fibre <promise_json> <validator_signatures_json> [flags]

# Process promise timeout (fallback mechanism)
celestia-appd tx fibre process-promise-timeout <promise_json> <promise_signature> [flags]
```

#### Queries

```shell
# Query escrow account
celestia-appd query fibre escrow-account <signer_address>

# Query pending withdrawals
celestia-appd query fibre pending-withdrawals <signer_address>

# Query if promise was processed
celestia-appd query fibre processed-promise <promise_hash>

# Query module parameters
celestia-appd query fibre params
```
