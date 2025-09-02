# `x/fibre` Payment Channel Specification

## Abstract

The `x/fibre` payment channel mechanism enables users to maintain escrow accounts for data publication by the Celestia validator set. This specification outlines a payment system where users deposit funds into escrow accounts, create signed promises for data commitments, validators sign over data chunks they receive, and either the user submits a payment confirmation with aggregated signatures or anyone can process the promise after a timeout period.

## Contents

1. [State](#state)
1. [Messages](#messages)
1. [Events](#events)
1. [Queries](#queries)
1. [Parameters](#parameters)
1. [Client](#client)

## State

The fibre payment channel module maintains state for escrow accounts, pending withdrawals, and module parameters.

### Params

```proto
message Params {
  option (gogoproto.goproto_stringer) = false;
  uint32 gas_per_blob_byte = 1
      [ (gogoproto.moretags) = "yaml:\"gas_per_blob_byte\"" ];
  uint64 withdrawal_delay_blocks = 2
      [ (gogoproto.moretags) = "yaml:\"withdrawal_delay_blocks\"" ];
  uint64 promise_timeout_blocks = 3
  [ (gogoproto.moretags) = "yaml:\"promise_timeout_blocks\"" ];
}
```

#### `GasPerBlobByte`

`GasPerBlobByte` is the amount of gas consumed per byte of blob data when payment is processed. This determines the gas cost for fibre blob inclusion.

#### `WithdrawalDelayBlocks`

`WithdrawalDelayBlocks` is the number of blocks that must pass between requesting a withdrawal and when funds become available for withdrawal (default: ~24 hours worth of blocks).

#### `PromiseTimeoutBlocks`

`PromiseTimeoutBlocks` is the number of blocks after which anyone can submit an promise for processing if the user hasn't submitted a PFF (default: ~1 hour worth of blocks).

### Escrow Accounts

Escrow accounts are lightweight payment channels that hold funds for fibre payments. Unlike regular accounts, they don't have keys or addresses and are referenced by a unique number.

```proto
message EscrowAccount {
  // escrow_id is the unique identifier for this escrow account
  uint64 escrow_id = 1;
  // owner is the address that controls this escrow account
  string owner = 2;
  // balance is the total deposited amount
  cosmos.base.v1beta1.Coin balance = 3;
  // available_balance is the amount available for payments (balance - pending_withdrawals)
  cosmos.base.v1beta1.Coin available_balance = 4;
  // sequence increments with each promise signed by the owner
  uint64 sequence = 5;
  // created_at is the block height when this escrow was created
  int64 created_at = 6;
}
```

### Pending Withdrawals

Withdrawal requests are tracked to implement the delay mechanism that protects validators.

```proto
message PendingWithdrawal {
  // escrow_id is the escrow account this withdrawal is for
  uint64 escrow_id = 1;
  // amount is the amount to be withdrawn
  cosmos.base.v1beta1.Coin amount = 2;
  // requested_at is the block height when withdrawal was requested
  int64 requested_at = 3;
  // available_at is the block height when funds become available
  int64 available_at = 4;
}
```

### Processed Promises

To prevent double payment, the module tracks which promises have been processed.

```proto
message ProcessedPromise {
  // promise_hash is the hash of the promise that was processed
  bytes promise_hash = 1;
  // processed_at is the block height when the promise was processed
  int64 processed_at = 2;
  // processed_via indicates how it was processed ("pff" or "timeout")
  string processed_via = 3;
}
```

#### Indexing

**Escrow Accounts**:
- **Primary Index**: `escrows/{escrow_id}` → `EscrowAccount`
- **By Owner**: `owners/{owner}/{escrow_id}` → `null` (for listing user's escrows)

**Pending Withdrawals**:
- **By Escrow**: `withdrawals/{escrow_id}/{requested_at}` → `PendingWithdrawal`
- **By Availability**: `available_withdrawals/{available_at}/{escrow_id}` → `null` (for processing)

**Processed Promises**:
- **Primary Index**: `processed/{promise_hash}` → `ProcessedPromise`
- **By Height**: `pruning/{processed_at}/{promise_hash}` → `null` (for pruning)

#### Pruning Mechanism

Processed promises are automatically pruned after 24 hours to prevent unbounded state growth.

## Messages

### MsgCreateEscrow

Creates a new escrow account for the signer.

```proto
message MsgCreateEscrow {
  // signer is the bech32 encoded signer address who will own the escrow
  string signer = 1;
  // initial_deposit is the initial amount to deposit (optional)
  cosmos.base.v1beta1.Coin initial_deposit = 2;
}
```

#### Validation and Processing

**Stateless Validation**:
- Signer address must be valid
- Initial deposit amount must be non-negative

**Stateful Processing**:
1. Generate unique escrow_id
2. Create EscrowAccount with sequence = 0
3. If initial_deposit > 0, transfer funds and update balances
4. Emit EventCreateEscrow

### MsgDepositToEscrow

Deposits funds to an existing escrow account.

```proto
message MsgDepositToEscrow {
  // signer is the bech32 encoded signer address
  string signer = 1;
  // escrow_id is the escrow account to deposit to
  uint64 escrow_id = 2;
  // amount is the amount to deposit
  cosmos.base.v1beta1.Coin amount = 3;
}
```

### MsgRequestWithdrawal

Requests withdrawal from an escrow account. Funds become available after the withdrawal delay.

```proto
message MsgRequestWithdrawal {
  // signer is the bech32 encoded signer address
  string signer = 1;
  // escrow_id is the escrow account to withdraw from
  uint64 escrow_id = 2;
  // amount is the amount to withdraw
  cosmos.base.v1beta1.Coin amount = 3;
}
```

#### Validation and Processing

**Stateful Processing**:
1. Verify signer owns escrow account
2. Verify sufficient available balance
3. Decrease available_balance immediately
4. Create PendingWithdrawal with available_at = current_height + withdrawal_delay_blocks
5. Emit EventRequestWithdrawal

### MsgProcessWithdrawal

Processes a withdrawal request that has passed the delay period.

```proto
message MsgProcessWithdrawal {
  // signer is the bech32 encoded signer address (can be anyone)
  string signer = 1;
  // escrow_id is the escrow account
  uint64 escrow_id = 2;
  // requested_at is the block height when withdrawal was requested
  int64 requested_at = 3;
}
```

### MsgPayForFibre

Contains the original payment promise with validator signatures, submitted by the user. Successful inclusion of this message must also include the commitment included in the relevant namespace. The payment messages themselves are included in their own reserved namespace similar to PFBs.

```proto
message MsgPayForFibre {
  // signer is the bech32 encoded address submitting this message
  string signer = 1;
  // promise contains the original payment promise
  PaymentPromise promise = 2;
  // validator_signatures contains signatures from 2/3+ validators
  repeated ValidatorSignature validator_signatures = 3;
}

message PaymentPromise {
  // escrow_id is the escrow account to charge
  uint64 escrow_id = 1;
  // escrow_owner is the owner of the escrow account
  string escrow_owner = 2;
  // sequence must match the escrow account's current sequence
  uint64 sequence = 3;
  // namespace is the namespace the blob is associated with
  bytes namespace = 4;
  // blob_size is the size of the blob in bytes
  uint32 blob_size = 5;
  // commitment is the hash of the row root and the RLC root
  bytes commitment = 6;
  // share_version is the version of the share format
  uint32 share_version = 7;
  // created_at is the block height when this promise was created
  int64 created_at = 8;
}
```

#### Validation and Processing

**Stateless Validation**:
- All promise fields must be valid (similar to PFF validation)
- Must have at least one validator signature

**Stateful Processing**:
1. Verify promise signature by escrow owner
2. Verify escrow account exists and sequence matches
3. Verify sufficient available balance for gas cost
4. Verify validator signatures represent 2/3+ voting power
5. Calculate and consume gas, deduct from escrow available balance
6. Increment escrow sequence
7. Mark promise as processed via "pff"
8. Include commitment in data square
9. Emit EventPayForFibre

### MsgProcessPromiseTimeout

Processes a payment promise after the timeout period if no PFF was submitted.

```proto
message MsgProcessPromiseTimeout {
  // signer is the bech32 encoded address submitting this message (can be anyone)
  string signer = 1;
  // promise contains the original payment promise
  PaymentPromise promise = 2;
  // promise_signature is the escrow owner's signature over the promise
  bytes promise_signature = 3;
}
```

#### Validation and Processing

**Stateless Validation**:
- Promise fields must be valid
- Promise signature must be valid

**Stateful Processing**:
1. Verify promise.created_at + promise_timeout_blocks <= current_height
2. Verify promise hasn't been processed already
3. Verify escrow account exists and sequence matches
4. Verify sufficient available balance
5. Calculate and consume gas, deduct from escrow available balance
6. Increment escrow sequence
7. Mark promise as processed via "timeout"
8. DO NOT include commitment (since no validator consensus)
9. Emit EventProcessPromiseTimeout

## Transaction Flow

The Fibre payment channel mechanism follows this flow:

1. **Setup Phase**: User creates escrow account and deposits funds using `MsgCreateEscrow` and/or `MsgDepositToEscrow`.

2. **Promise Creation**: User creates a signed `PaymentPromise` containing escrow details, commitment, and sequence number.

3. **Data Distribution Phase**: User distributes data chunks to validators along with the signed promise.

4. **Validator Verification**: Validators verify the promise signature, check escrow has sufficient funds, and sign over the commitment if valid.

5. **Payment Confirmation (Happy Path)**: User collects 2/3+ validator signatures and submits `MsgPayForFibre` containing the promise and signatures. The commitment gets included in the data square.

6. **Timeout Processing (Fallback)**: If user doesn't submit PFF within `promise_timeout_blocks`, anyone can submit `MsgProcessPromiseTimeout` to process payment via gas mechanism. This prevents the user from getting free service.

7. **Withdrawal**: Users can request withdrawals via `MsgRequestWithdrawal` (decreases available balance immediately) and process them after the delay via `MsgProcessWithdrawal`.

## Events

### Payment Channel Events

#### `EventCreateEscrow`

| Attribute Key | Attribute Value                    |
|---------------|------------------------------------|
| owner         | {bech32 encoded owner address}     |
| escrow_id     | {escrow account ID}                |
| initial_deposit | {initial deposit amount}         |

#### `EventDepositToEscrow`

| Attribute Key | Attribute Value                    |
|---------------|------------------------------------|
| signer        | {bech32 encoded signer address}    |
| escrow_id     | {escrow account ID}                |
| amount        | {deposit amount}                   |

#### `EventRequestWithdrawal`

| Attribute Key | Attribute Value                    |
|---------------|------------------------------------|
| owner         | {bech32 encoded owner address}     |
| escrow_id     | {escrow account ID}                |
| amount        | {withdrawal amount}                |
| available_at  | {block height when available}     |

#### `EventProcessWithdrawal`

| Attribute Key | Attribute Value                    |
|---------------|------------------------------------|
| processor     | {bech32 encoded processor address} |
| escrow_id     | {escrow account ID}                |
| amount        | {withdrawal amount}                |

#### `EventPayForFibre`

| Attribute Key | Attribute Value                      |
|---------------|--------------------------------------|
| signer        | {bech32 encoded submitter address}   |
| escrow_owner  | {bech32 encoded escrow owner}        |
| escrow_id     | {escrow account ID}                  |
| namespace     | {namespace the blob is published to} |
| validator_count | {number of validator signatures}   |

#### `EventProcessPromiseTimeout`

| Attribute Key | Attribute Value                      |
|---------------|--------------------------------------|
| processor     | {bech32 encoded processor address}   |
| escrow_owner  | {bech32 encoded escrow owner}        |
| escrow_id     | {escrow account ID}                  |
| namespace     | {namespace the blob is published to} |

## Queries

### EscrowAccount

Queries an escrow account by ID.

**Request**:
```proto
message QueryEscrowAccountRequest {
  uint64 escrow_id = 1;
}
```

**Response**:
```proto
message QueryEscrowAccountResponse {
  EscrowAccount escrow_account = 1;
  bool found = 2;
}
```

### EscrowAccountsByOwner

Queries all escrow accounts owned by an address.

**Request**:
```proto
message QueryEscrowAccountsByOwnerRequest {
  string owner = 1;
  cosmos.base.query.v1beta1.PageRequest pagination = 2;
}
```

**Response**:
```proto
message QueryEscrowAccountsByOwnerResponse {
  repeated EscrowAccount escrow_accounts = 1;
  cosmos.base.query.v1beta1.PageResponse pagination = 2;
}
```

### PendingWithdrawals

Queries pending withdrawals for an escrow account.

**Request**:
```proto
message QueryPendingWithdrawalsRequest {
  uint64 escrow_id = 1;
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

Queries whether an promise has been processed.

**Request**:
```proto
message QueryProcessedPromiseRequest {
  bytes promise_hash = 1;
}
```

**Response**:
```proto
message QueryProcessedPromiseResponse {
  ProcessedPromise processed_promise = 1;
  bool found = 2;
}
```

## Parameters

| Key                 | Type   | Default | Description |
|---------------------|--------|---------|-------------|
| GasPerBlobByte      | uint32 | 8       | Gas cost per byte of blob data |
| WithdrawalDelayBlocks | uint64 | 144     | Blocks to wait before withdrawal (24 hours) |
| PromiseTimeoutBlocks | uint64 | 60      | Blocks before promise can be processed by timeout (1 hour) |

## Client

### CLI

#### Transactions

```shell
# Create new escrow account
celestia-appd tx fibre create-escrow [initial_deposit] [flags]

# Deposit to escrow account
celestia-appd tx fibre deposit-to-escrow <escrow_id> <amount> [flags]

# Request withdrawal from escrow
celestia-appd tx fibre request-withdrawal <escrow_id> <amount> [flags]

# Process withdrawal (after delay)
celestia-appd tx fibre process-withdrawal <escrow_id> <requested_at> [flags]

# Submit payment with validator signatures
celestia-appd tx fibre pay-for-fibre-channel <promise_json> <validator_signatures_json> [flags]

# Process promise timeout (fallback mechanism)
celestia-appd tx fibre process-promise-timeout <promise_json> <promise_signature> [flags]
```

#### Queries

```shell
# Query escrow account by ID
celestia-appd query fibre escrow-account <escrow_id>

# Query escrow accounts by owner
celestia-appd query fibre escrow-accounts-by-owner <owner_address>

# Query pending withdrawals
celestia-appd query fibre pending-withdrawals <escrow_id>

# Query if promise was processed
celestia-appd query fibre processed-promise <promise_hash>

# Query module parameters
celestia-appd query fibre params
```

#### Promise Signing

```shell
# Generate signed promise for validators
celestia-appd tx fibre create-promise <escrow_id> <namespace> <blob_size> <commitment> [flags]
```
