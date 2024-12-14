# Consensus Rules

<!-- toc -->

## System Parameters

### Units

| name | SI    | value   | description         |
|------|-------|---------|---------------------|
| `1u` | `1u`  | `10**0` | `1` unit.           |
| `2u` | `k1u` | `10**3` | `1000` units.       |
| `3u` | `M1u` | `10**6` | `1000000` units.    |
| `4u` | `G1u` | `10**9` | `1000000000` units. |

### Constants

| name                                    | type     | value        | unit    | description                                                                                                                                                              |
|-----------------------------------------|----------|--------------|---------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `AVAILABLE_DATA_ORIGINAL_SQUARE_MAX`    | `uint64` |              | `share` | Maximum number of rows/columns of the original data [shares](data_structures.md#share) in [square layout](data_structures.md#arranging-available-data-into-shares).      |
| `AVAILABLE_DATA_ORIGINAL_SQUARE_TARGET` | `uint64` |              | `share` | Target number of rows/columns of the original data [shares](data_structures.md#share) in [square layout](data_structures.md#arranging-available-data-into-shares).       |
| `BLOCK_TIME`                            | `uint64` |              | second  | Block time, in seconds.                                                                                                                                                  |
| `CHAIN_ID`                              | `string` | `"Celestia"` |         | Chain ID. Each chain assigns itself a (unique) ID.                                                                                                                       |
| `GENESIS_COIN_COUNT`                    | `uint64` | `10**8`      | `4u`    | `(= 100000000)` Number of coins at genesis.                                                                                                                              |
| `MAX_GRAFFITI_BYTES`                    | `uint64` | `32`         | `byte`  | Maximum size of transaction graffiti, in bytes.                                                                                                                          |
| `MAX_VALIDATORS`                        | `uint16` | `64`         |         | Maximum number of active validators.                                                                                                                                     |
| `NAMESPACE_VERSION_SIZE`                | `int`    | `1`          | `byte`  | Size of namespace version in bytes.                                                                                                                                      |
| `NAMESPACE_ID_SIZE`                     | `int`    | `28`         | `byte`  | Size of namespace ID in bytes.                                                                                                                                           |
| `NAMESPACE_SIZE`                        | `int`    | `29`         | `byte`  | Size of namespace in bytes.                                                                                                                                              |
| `NAMESPACE_ID_MAX_RESERVED`             | `uint64` | `255`        |         | Value of maximum reserved namespace (inclusive). 1 byte worth of IDs.                                                                                                    |
| `SEQUENCE_BYTES`                        | `uint64` | `4`          | `byte`  | The number of bytes used to store the sequence length in the first share of a sequence                                                                                   |
| `SHARE_INFO_BYTES`                      | `uint64` | `1`          | `byte`  | The number of bytes used for [share](data_structures.md#share) information                                                                                               |
| `SHARE_RESERVED_BYTES`                  | `uint64` | `4`          | `byte`  | The number of bytes used to store the index of the first transaction in a transaction share. Must be able to represent any integer up to and including `SHARE_SIZE - 1`. |
| `SHARE_SIZE`                            | `uint64` | `512`        | `byte`  | Size of transaction and blob [shares](data_structures.md#share), in bytes.                                                                                               |
| `SignerSize`                            | `int`    | `20`         | `byte`  | The number of bytes used to store the signer in a [share](data_structures.md#share).                                                                                     |
| `STATE_SUBTREE_RESERVED_BYTES`          | `uint64` | `1`          | `byte`  | Number of bytes reserved to identify state subtrees.                                                                                                                     |
| `UNBONDING_DURATION`                    | `uint32` |              | `block` | Duration, in blocks, for unbonding a validator or delegation.                                                                                                            |
| `v1.Version`                            | `uint64` | `1`          |         | First version of the application. Breaking changes (hard forks) must update this parameter.                                                                              |
| `v2.Version`                            | `uint64` | `2`          |         | Second version of the application. Breaking changes (hard forks) must update this parameter.                                                                             |
| `VERSION_BLOCK`                         | `uint64` | `1`          |         | Version of the Celestia chain. Breaking changes (hard forks) must update this parameter.                                                                                 |

### Rewards and Penalties

| name                     | type     | value       | unit   | description                                             |
|--------------------------|----------|-------------|--------|---------------------------------------------------------|
| `SECONDS_PER_YEAR`       | `uint64` | `31536000`  | second | Seconds per year. Omit leap seconds.                    |
| `TARGET_ANNUAL_ISSUANCE` | `uint64` | `2 * 10**6` | `4u`   | `(= 2000000)` Target number of coins to issue per year. |

## Leader Selection

Refer to the CometBFT specifications for [proposer selection procedure](https://docs.cometbft.com/v0.34/spec/consensus/proposer-selection).

## Fork Choice

The Tendermint consensus protocol is fork-free by construction under an honest majority of stake assumption.

If a block has a [valid commit](#blocklastcommit), it is part of the canonical chain. If equivocation evidence is detected for more than 1/3 of voting power, the node must halt. See [proof of fork accountability](https://docs.cometbft.com/v0.34/spec/consensus/consensus#proof-of-fork-accountability).

## Block Validity

The validity of a newly-seen block, `block`, is determined by two components, detailed in subsequent sections:

1. [Block structure](#block-structure): whether the block header is valid, and data in a block is arranged into a valid and matching data root (i.e. syntax).
1. [State transition](#state-transitions): whether the application of transactions in the block produces a matching and valid state root (i.e. semantics).

Pseudocode in this section is not in any specific language and should be interpreted as being in a neutral and sane language.

## Block Structure

Before executing [state transitions](#state-transitions), the structure of the [block](./data_structures.md#block) must be verified.

The following block fields are acquired from the network and parsed (i.e. [deserialized](./data_structures.md#serialization)). If they cannot be parsed, the block is ignored but is not explicitly considered invalid by consensus rules. Further implications of ignoring a block are found in the [networking spec](./networking.md).

1. [block.header](./data_structures.md#header)
1. [block.availableDataHeader](./data_structures.md#availabledataheader)
1. [block.lastCommit](./data_structures.md#commit)

If the above fields are parsed successfully, the available data `block.availableData` is acquired in erasure-coded form as [a list of share rows](./networking.md#availabledata), then parsed. If it cannot be parsed, the block is ignored but not explicitly invalid, as above.

### `block.header`

The [block header](./data_structures.md#header) `block.header` (`header` for short) is the first thing that is downloaded from the new block, and commits to everything inside the block in some way. For previous block `prev` (if `prev` is not known, then the block is ignored), and previous block header `prev.header`, the following checks must be `true`:

`availableDataOriginalSquareSize` is computed as described [here](./data_structures.md#header).

1. `header.height` == `prev.header.height + 1`.
1. `header.timestamp` > `prev.header.timestamp`.
1. `header.lastHeaderHash` == the [header hash](./data_structures.md#header) of `prev`.
1. `header.lastCommitHash` == the [hash](./data_structures.md#hashing) of `lastCommit`.
1. `header.consensusHash` == the value computed [here](./data_structures.md#consensus-parameters).
1. `header.stateCommitment` == the root of the state, computed [with the application of all state transitions in this block](#state-transitions).
1. `availableDataOriginalSquareSize` <= [`AVAILABLE_DATA_ORIGINAL_SQUARE_MAX`](#constants).
1. `header.availableDataRoot` == the [Merkle root](./data_structures.md#binary-merkle-tree) of the tree with the row and column roots of `block.availableDataHeader` as leaves.
1. `header.proposerAddress` == the [leader](#leader-selection) for `header.height`.

### `block.availableDataHeader`

The [available data header](./data_structures.md#availabledataheader) `block.availableDataHeader` (`availableDataHeader` for short) is then processed. This commits to the available data, which is only downloaded after the [consensus commit](#blocklastcommit) is processed. The following checks must be `true`:

1. Length of `availableDataHeader.rowRoots` == `availableDataOriginalSquareSize * 2`.
1. Length of `availableDataHeader.colRoots` == `availableDataOriginalSquareSize * 2`.
1. The length of each element in `availableDataHeader.rowRoots` and `availableDataHeader.colRoots` must be [`32`](./data_structures.md#hashing).

### `block.lastCommit`

The last [commit](./data_structures.md#commit) `block.lastCommit` (`lastCommit` for short) is processed next. This is the Tendermint commit (i.e. polka of votes) _for the previous block_. For previous block `prev` and previous block header `prev.header`, the following checks must be `true`:

1. `lastCommit.height` == `prev.header.height`.
1. `lastCommit.round` >= `1`.
1. `lastCommit.headerHash` == the [header hash](./data_structures.md#header) of `prev`.
1. Length of `lastCommit.signatures` <= [`MAX_VALIDATORS`](#constants).
1. Each of `lastCommit.signatures` must be a valid [CommitSig](./data_structures.md#commitsig)
1. The sum of the votes for `prev` in `lastCommit` must be at least 2/3 (rounded up) of the voting power of `prev`'s next validator set.

### `block.availableData`

The block's [available data](./data_structures.md#availabledata) (analogous to transactions in contemporary blockchain designs) `block.availableData` (`availableData` for short) is finally processed. The [list of share rows](./networking.md#availabledata) is parsed into the [actual data structures](./data_structures.md#availabledata) using the reverse of [the process to encode available data into shares](./data_structures.md#arranging-available-data-into-shares); if parsing fails here, the block is invalid.

Once parsed, the following checks must be `true`:

1. The commitments of the [erasure-coded extended](./data_structures.md#2d-reed-solomon-encoding-scheme) `availableData` must match those in `header.availableDataHeader`. Implicitly, this means that both rows and columns must be ordered lexicographically by namespace since they are committed to in a [Namespace Merkle Tree](data_structures.md#namespace-merkle-tree).
1. Length of `availableData.intermediateStateRootData` == length of `availableData.transactionData` + length of `availableData.payForBlobData` + 2. (Two additional state transitions are the [begin](#begin-block) and [end block](#end-block) implicit transitions.)

## State Transitions

Once the basic structure of the block [has been validated](#block-structure), state transitions must be applied to compute the new state and state root.

For this section, the variable `state` represents the [state tree](./data_structures.md#state), with `state.accounts[k]`, `state.inactiveValidatorSet[k]`, `state.activeValidatorSet[k]`, and `state.delegationSet[k]` being shorthand for the leaf in the state tree in the [accounts, inactive validator set, active validator set, and delegation set subtrees](./data_structures.md#state) with [pre-hashed key](./data_structures.md#state) `k`. E.g. `state.accounts[a]` is shorthand for `state[(ACCOUNTS_SUBTREE_ID << 8*(32-STATE_SUBTREE_RESERVED_BYTES)) | ((-1 >> 8*STATE_SUBTREE_RESERVED_BYTES) & hash(a))]`.

State transitions are applied in the following order:

1. [Begin block](#begin-block).
1. [Transactions](#blockavailabledatatransactiondata).
1. [End block](#end-block).

### `block.availableData.transactionData`

Transactions are applied to the state. Note that _transactions_ mutate the state (essentially, the validator set and minimal balances), while _blobs_ do not.

`block.availableData.transactionData` is simply a list of [WrappedTransaction](./data_structures.md#wrappedtransaction)s. For each wrapped transaction in this list, `wrappedTransaction`, with index `i` (starting from `0`), the following checks must be `true`:

1. `wrappedTransaction.index` == `i`.

For `wrappedTransaction`'s [transaction](./data_structures.md#transaction) `transaction`, the following checks must be `true`:

1. `transaction.signature` must be a [valid signature](./data_structures.md#public-key-cryptography) over `transaction.signedTransactionData`.

Finally, each `wrappedTransaction` is processed depending on [its transaction type](./data_structures.md#signedtransactiondata). These are specified in the next subsections, where `tx` is short for `transaction.signedTransactionData`, and `sender` is the recovered signing [address](./data_structures.md#address). We will define a few helper functions:

```py
tipCost(y, z) = y * z
totalCost(x, y, z) = x + tipCost(y, z)
```

where `x` above is the amount of coins sent by the transaction authorizer, `y` above is the tip rate set in the transaction, and `z` above is the measure of the block space used by the transaction (i.e. size in bytes).

Four additional helper functions are defined to manage the [validator queue](./data_structures.md#validator):

1. `findFromQueue(power)`, which returns the address of the last validator in the [validator queue](./data_structures.md#validator) with voting power greater than or equal to `power`, or `0` if the queue is empty or no validators in the queue have at least `power` voting power.
1. `parentFromQueue(address)`, which returns the address of the parent in the validator queue of the validator with address `address`, or `0` if `address` is not in the queue or is the head of the queue.
1. `validatorQueueInsert`, defined as

```py
function validatorQueueInsert(validator)
    # Insert the new validator into the linked list
    parent = findFromQueue(validator.votingPower)
    if parent != 0
        if state.accounts[parent].status == AccountStatus.ValidatorBonded
            validator.next = state.activeValidatorSet[parent].next
            state.activeValidatorSet[parent].next = sender
        else
            validator.next = state.inactiveValidatorSet[parent].next
            state.inactiveValidatorSet[parent].next = sender
    else
        validator.next = state.validatorQueueHead
        state.validatorQueueHead = sender
```

<!-- markdownlint-disable-next-line MD029 -->
4. `validatorQueueRemove`, defined as

```py
function validatorQueueRemove(validator, sender)
    # Remove existing validator from the linked list
    parent = parentFromQueue(sender)
    if parent != 0
        if state.accounts[parent].status == AccountStatus.ValidatorBonded
            state.activeValidatorSet[parent].next = validator.next
            validator.next = 0
        else
            state.inactiveValidatorSet[parent].next = validator.next
            validator.next = 0
    else
        state.validatorQueueHead = validator.next
        validator.next = 0
```

Note that light clients cannot perform a linear search through a linked list, and are instead provided logarithmic proofs (e.g. in the case of `parentFromQueue`, a proof to the parent is provided, which should have `address` as its next validator).

In addition, three helper functions to manage the [blob paid list](./data_structures.md#blobpaid):

1. `findFromBlobPaidList(start)`, which returns the transaction ID of the last transaction in the [blob paid list](./data_structures.md#blobpaid) with `finish` greater than `start`, or `0` if the list is empty or no transactions in the list have at least `start` `finish`.
1. `parentFromBlobPaidList(txid)`, which returns the transaction ID of the parent in the blob paid list of the transaction with ID `txid`, or `0` if `txid` is not in the list or is the head of the list.
1. `blobPaidListInsert`, defined as

```py
function blobPaidListInsert(tx, txid)
    # Insert the new transaction into the linked list
    parent = findFromBlobPaidList(tx.blobStartIndex)
    state.blobsPaid[txid].start = tx.blobStartIndex
    numShares = ceil(tx.blobSize / SHARE_SIZE)
    state.blobsPaid[txid].finish = tx.blobStartIndex + numShares - 1
    if parent != 0
        state.blobsPaid[txid].next = state.blobsPaid[parent].next
        state.blobsPaid[parent].next = txid
    else
        state.blobsPaid[txid].next = state.blobPaidHead
        state.blobPaidHead = txid
```

We define a helper function to compute F1 entries:

```py
function compute_new_entry(reward, power)
    if power == 0
        return 0
    return reward // power
```

After applying a transaction, the new state root is computed.

#### SignedTransactionDataTransfer

```py
bytesPaid = len(tx)
```

The following checks must be `true`:

1. `tx.type` == [`TransactionType.Transfer`](./data_structures.md#signedtransactiondata).
1. `totalCost(tx.amount, tx.fee.tipRate, bytesPaid)` <= `state.accounts[sender].balance`.
1. `tx.nonce` == `state.accounts[sender].nonce + 1`.

Apply the following to the state:

```py
state.accounts[sender].nonce += 1

state.accounts[sender].balance -= totalCost(tx.amount, tx.fee.tipRate, bytesPaid)
state.accounts[tx.to].balance += tx.amount

state.activeValidatorSet.proposerBlockReward += tipCost(bytesPaid)
```

#### SignedTransactionDataMsgPayForData

```py
bytesPaid = len(tx) + tx.blobSize
currentStartFinish = state.blobsPaid[findFromBlobPaidList(tx.blobStartIndex)]
parentStartFinish = state.blobsPaid[parentFromBlobPaidList(findFromBlobPaidList(tx.blobStartIndex))]
```

The following checks must be `true`:

1. `tx.type` == [`TransactionType.MsgPayForData`](./data_structures.md#signedtransactiondata).
1. `totalCost(0, tx.fee.tipRate, bytesPaid)` <= `state.accounts[sender].balance`.
1. `tx.nonce` == `state.accounts[sender].nonce + 1`.
1. The `ceil(tx.blobSize / SHARE_SIZE)` shares starting at index `tx.blobStartIndex` must:
    1. Have namespace `tx.blobNamespace`.
1. `tx.blobShareCommitment` == computed as described [here](./data_structures.md#signedtransactiondatamsgpayfordata).
1. `parentStartFinish.finish` < `tx.blobStartIndex`.
1. `currentStartFinish.start` == `0` or `currentStartFinish.start` > `tx.blobStartIndex + ceil(tx.blobSize / SHARE_SIZE)`.

Apply the following to the state:

```py
state.accounts[sender].nonce += 1
state.accounts[sender].balance -= totalCost(tx.amount, tx.fee.tipRate, bytesPaid)

blobPaidListInsert(tx, id(tx))

state.activeValidatorSet.proposerBlockReward += tipCost(tx.fee.tipRate, bytesPaid)
```

#### SignedTransactionDataCreateValidator

```py
bytesPaid = len(tx)
```

The following checks must be `true`:

1. `tx.type` == [`TransactionType.CreateValidator`](./data_structures.md#signedtransactiondata).
1. `totalCost(0, tx.fee.tipRate, bytesPaid)` <= `state.accounts[sender].balance`.
1. `tx.nonce` == `state.accounts[sender].nonce + 1`.
1. `tx.commissionRate.denominator > 0`.
1. `tx.commissionRate.numerator <= tx.commissionRate.denominator`.
1. `state.accounts[sender].status` == `AccountStatus.None`.

Apply the following to the state:

```py
state.accounts[sender].nonce += 1
state.accounts[sender].balance -= totalCost(0, tx.fee.tipRate, bytesPaid)
state.accounts[sender].status = AccountStatus.ValidatorQueued

validator = new Validator
validator.commissionRate = tx.commissionRate
validator.delegatedCount = 0
validator.votingPower = 0
validator.pendingRewards = 0
validator.latestEntry = PeriodEntry(0)
validator.unbondingHeight = 0
validator.isSlashed = false

validatorQueueInsert(validator)

state.inactiveValidatorSet[sender] = validator

state.activeValidatorSet.proposerBlockReward += tipCost(tx.fee.tipRate, bytesPaid)
```

#### SignedTransactionDataBeginUnbondingValidator

```py
bytesPaid = len(tx)
```

The following checks must be `true`:

1. `tx.type` == [`TransactionType.BeginUnbondingValidator`](./data_structures.md#signedtransactiondata).
1. `totalCost(0, tx.fee.tipRate, bytesPaid)` <= `state.accounts[sender].balance`.
1. `tx.nonce` == `state.accounts[sender].nonce + 1`.
1. `state.accounts[sender].status` == `AccountStatus.ValidatorQueued` or `state.accounts[sender].status` == `AccountStatus.ValidatorBonded`.

Apply the following to the state:

```py
state.accounts[sender].nonce += 1
state.accounts[sender].balance -= totalCost(0, tx.fee.tipRate, bytesPaid)
state.accounts[sender].status = ValidatorStatus.Unbonding

if state.accounts[sender].status == AccountStatus.ValidatorQueued
    validator = state.inactiveValidatorSet[sender]
else if state.accounts[sender].status == AccountStatus.ValidatorBonded
    validator = state.activeValidatorSet[sender]
    delete state.activeValidatorSet[sender]

validator.unbondingHeight = block.height + 1
validator.latestEntry += compute_new_entry(validator.pendingRewards, validator.votingPower)
validator.pendingRewards = 0

validatorQueueRemove(validator, sender)

state.inactiveValidatorSet[sender] = validator

state.activeValidatorSet.activeVotingPower -= validator.votingPower

state.activeValidatorSet.proposerBlockReward += tipCost(tx.fee.tipRate, bytesPaid)
```

#### SignedTransactionDataUnbondValidator

```py
bytesPaid = len(tx)
```

The following checks must be `true`:

1. `tx.type` == [`TransactionType.UnbondValidator`](./data_structures.md#signedtransactiondata).
1. `totalCost(0, tx.fee.tipRate, bytesPaid)` <= `state.accounts[sender].balance`.
1. `tx.nonce` == `state.accounts[sender].nonce + 1`.
1. `state.accounts[sender].status` == `AccountStatus.ValidatorUnbonding`.
1. `state.inactiveValidatorSet[sender].unbondingHeight + UNBONDING_DURATION` < `block.height`.

Apply the following to the state:

```py
validator = state.inactiveValidatorSet[sender]

state.accounts[sender].nonce += 1
state.accounts[sender].balance -= totalCost(0, tx.fee.tipRate, bytesPaid)
state.accounts[sender].status = AccountStatus.ValidatorUnbonded

state.accounts[sender].balance += validator.commissionRewards

state.inactiveValidatorSet[sender] = validator

if validator.delegatedCount == 0
    state.accounts[sender].status = AccountStatus.None
    delete state.inactiveValidatorSet[sender]

state.activeValidatorSet.proposerBlockReward += tipCost(tx.fee.tipRate, bytesPaid)
```

#### SignedTransactionDataCreateDelegation

```py
bytesPaid = len(tx)
```

The following checks must be `true`:

1. `tx.type` == [`TransactionType.CreateDelegation`](./data_structures.md#signedtransactiondata).
1. `totalCost(tx.amount, tx.fee.tipRate, bytesPaid)` <= `state.accounts[sender].balance`.
1. `state.accounts[tx.to].status` == `AccountStatus.ValidatorQueued` or `state.accounts[tx.to].status` == `AccountStatus.ValidatorBonded`.
1. `tx.nonce` == `state.accounts[sender].nonce + 1`.
1. `state.accounts[sender].status` == `AccountStatus.None`.

Apply the following to the state:

```py
state.accounts[sender].nonce += 1
state.accounts[sender].balance -= totalCost(tx.amount, tx.fee.tipRate, bytesPaid)
state.accounts[sender].status = AccountStatus.DelegationBonded

if state.accounts[tx.to].status == AccountStatus.ValidatorQueued
    validator = state.inactiveValidatorSet[tx.to]
else if state.accounts[tx.to].status == AccountStatus.ValidatorBonded
    validator = state.activeValidatorSet[tx.to]

delegation = new Delegation
delegation.status = DelegationStatus.Bonded
delegation.validator = tx.to
delegation.stakedBalance = tx.amount
delegation.beginEntry = validator.latestEntry
delegation.endEntry = PeriodEntry(0)
delegation.unbondingHeight = 0

validator.latestEntry += compute_new_entry(validator.pendingRewards, validator.votingPower)
validator.pendingRewards = 0
validator.delegatedCount += 1
validator.votingPower += tx.amount

# Update the validator in the linked list by first removing then inserting
validatorQueueRemove(validator, delegation.validator)
validatorQueueInsert(validator)

state.delegationSet[sender] = delegation

if state.accounts[tx.to].status == AccountStatus.ValidatorQueued
    state.inactiveValidatorSet[tx.to] = validator
else if state.accounts[tx.to].status == AccountStatus.ValidatorBonded
    state.activeValidatorSet[tx.to] = validator
    state.activeValidatorSet.activeVotingPower += tx.amount

state.activeValidatorSet.proposerBlockReward += tipCost(tx.fee.tipRate, bytesPaid)
```

#### SignedTransactionDataBeginUnbondingDelegation

```py
bytesPaid = len(tx)
```

The following checks must be `true`:

1. `tx.type` == [`TransactionType.BeginUnbondingDelegation`](./data_structures.md#signedtransactiondata).
1. `totalCost(0, tx.fee.tipRate, bytesPaid)` <= `state.accounts[sender].balance`.
1. `tx.nonce` == `state.accounts[sender].nonce + 1`.
1. `state.accounts[sender].status` == `AccountStatus.DelegationBonded`.

Apply the following to the state:

```py
state.accounts[sender].nonce += 1
state.accounts[sender].balance -= totalCost(0, tx.fee.tipRate, bytesPaid)
state.accounts[sender].status = AccountStatus.DelegationUnbonding

delegation = state.delegationSet[sender]

if state.accounts[delegation.validator].status == AccountStatus.ValidatorQueued ||
      state.accounts[delegation.validator].status == AccountStatus.ValidatorUnbonding ||
      state.accounts[delegation.validator].status == AccountStatus.ValidatorUnbonded
    validator = state.inactiveValidatorSet[delegation.validator]
else if state.accounts[delegation.validator].status == AccountStatus.ValidatorBonded
    validator = state.activeValidatorSet[delegation.validator]

delegation.status = DelegationStatus.Unbonding
delegation.endEntry = validator.latestEntry
delegation.unbondingHeight = block.height + 1

validator.latestEntry += compute_new_entry(validator.pendingRewards, validator.votingPower)
validator.pendingRewards = 0
validator.delegatedCount -= 1
validator.votingPower -= delegation.stakedBalance

# Update the validator in the linked list by first removing then inserting
# Only do this if the validator is actually in the queue (i.e. bonded or queued)
if state.accounts[delegation.validator].status == AccountStatus.ValidatorBonded ||
      state.accounts[delegation.validator].status == AccountStatus.ValidatorQueued
    validatorQueueRemove(validator, delegation.validator)
    validatorQueueInsert(validator)

state.delegationSet[sender] = delegation

if state.accounts[delegation.validator].status == AccountStatus.ValidatorQueued ||
      state.accounts[delegation.validator].status == AccountStatus.ValidatorUnbonding ||
      state.accounts[delegation.validator].status == AccountStatus.ValidatorUnbonded
    state.inactiveValidatorSet[delegation.validator] = validator
else if state.accounts[delegation.validator].status == AccountStatus.ValidatorBonded
    state.activeValidatorSet[delegation.validator] = validator
    state.activeValidatorSet.activeVotingPower -= delegation.stakedBalance

state.activeValidatorSet.proposerBlockReward += tipCost(tx.fee.tipRate, bytesPaid)
```

#### SignedTransactionDataUnbondDelegation

```py
bytesPaid = len(tx)
```

The following checks must be `true`:

1. `tx.type` == [`TransactionType.UnbondDelegation`](./data_structures.md#signedtransactiondata).
1. `totalCost(0, bytesPaid)` <= `state.accounts[sender].balance`.
1. `tx.nonce` == `state.accounts[sender].nonce + 1`.
1. `state.accounts[sender].status` == `AccountStatus.DelegationUnbonding`.
1. `state.delegationSet[sender].unbondingHeight + UNBONDING_DURATION` < `block.height`.

Apply the following to the state:

```py
delegation = state.accounts[sender].delegationInfo

state.accounts[sender].nonce += 1
state.accounts[sender].balance -= totalCost(0, tx.fee.tipRate, bytesPaid)
state.accounts[sender].status = None

# Return the delegated stake
state.accounts[sender].balance += delegation.stakedBalance
# Also disperse rewards (commission has already been levied)
state.accounts[sender].balance += delegation.stakedBalance * (delegation.endEntry - delegation.beginEntry)

if state.accounts[delegation.validator].status == AccountStatus.ValidatorQueued ||
      state.accounts[delegation.validator].status == AccountStatus.ValidatorUnbonding
      state.accounts[delegation.validator].status == AccountStatus.ValidatorUnbonded
    validator = state.inactiveValidatorSet[delegation.validator]
else if state.accounts[delegation.validator].status == AccountStatus.ValidatorBonded
    validator = state.activeValidatorSet[delegation.validator]

if validator.delegatedCount == 0 &&
      state.accounts[delegation.validator].status == AccountStatus.ValidatorUnbonded
    state.accounts[delegation.validator].status = AccountStatus.None
    delete state.inactiveValidatorSet[delegation.validator]

delete state.accounts[sender].delegationInfo

state.activeValidatorSet.proposerBlockReward += tipCost(tx.fee.tipRate, bytesPaid)
```

#### SignedTransactionDataBurn

```py
bytesPaid = len(tx)
```

The following checks must be `true`:

1. `tx.type` == [`TransactionType.Burn`](./data_structures.md#signedtransactiondata).
1. `totalCost(tx.amount, bytesPaid)` <= `state.accounts[sender].balance`.
1. `tx.nonce` == `state.accounts[sender].nonce + 1`.

Apply the following to the state:

```py
state.accounts[sender].nonce += 1
state.accounts[sender].balance -= totalCost(tx.amount, tx.fee.tipRate, bytesPaid)

state.activeValidatorSet.proposerBlockReward += tipCost(tx.fee.tipRate, bytesPaid)
```

#### SignedTransactionRedelegateCommission

```py
bytesPaid = len(tx)
```

The following checks must be `true`:

1. `tx.type` == [`TransactionType.RedelegateCommission`](./data_structures.md#signedtransactiondata).
1. `totalCost(0, tx.fee.tipRate, bytesPaid)` <= `state.accounts[sender].balance`.
1. `tx.nonce` == `state.accounts[sender].nonce + 1`.
1. `state.accounts[tx.to].status` == `AccountStatus.DelegationBonded`.
1. `state.accounts[sender].status` == `AccountStatus.ValidatorBonded`.

Apply the following to the state:

```py
state.accounts[sender].nonce += 1
state.accounts[sender].balance -= totalCost(0, tx.fee.tipRate, bytesPaid)

delegation = state.delegationSet[tx.to]
validator = state.activeValidatorSet[delegation.validator]

# Force-redelegate pending rewards for delegation
pendingRewards = delegation.stakedBalance * (validator.latestEntry - delegation.beginEntry)
delegation.stakedBalance += pendingRewards
delegation.beginEntry = validator.latestEntry

validator.latestEntry += compute_new_entry(validator.pendingRewards, validator.votingPower)
validator.pendingRewards = 0

# Assign pending commission rewards to delegation
commissionRewards = validator.commissionRewards
delegation.stakedBalance += commissionRewards
validator.commissionRewards = 0

# Update voting power
validator.votingPower += pendingRewards + commissionRewards
state.activeValidatorSet.activeVotingPower += pendingRewards + commissionRewards

state.delegationSet[tx.to] = delegation
state.activeValidatorSet[delegation.validator] = validator

state.activeValidatorSet.proposerBlockReward += tipCost(tx.fee.tipRate, bytesPaid)
```

#### SignedTransactionRedelegateReward

```py
bytesPaid = len(tx)
```

The following checks must be `true`:

1. `tx.type` == [`TransactionType.RedelegateReward`](./data_structures.md#signedtransactiondata).
1. `totalCost(0, tx.fee.tipRate, bytesPaid)` <= `state.accounts[sender].balance`.
1. `tx.nonce` == `state.accounts[sender].nonce + 1`.
1. `state.accounts[sender].status` == `AccountStatus.DelegationBonded`.
1. `state.accounts[state.delegationSet[sender].validator].status` == `AccountStatus.ValidatorBonded`.

Apply the following to the state:

```py
state.accounts[sender].nonce += 1
state.accounts[sender].balance -= totalCost(0, tx.fee.tipRate, bytesPaid)

delegation = state.delegationSet[sender]
validator = state.activeValidatorSet[delegation.validator]

# Redelegate pending rewards for delegation
pendingRewards = delegation.stakedBalance * (validator.latestEntry - delegation.beginEntry)
delegation.stakedBalance += pendingRewards
delegation.beginEntry = validator.latestEntry

validator.latestEntry += compute_new_entry(validator.pendingRewards, validator.votingPower)
validator.pendingRewards = 0

# Update voting power
validator.votingPower += pendingRewards
state.activeValidatorSet.activeVotingPower += pendingRewards

state.delegationSet[sender] = delegation
state.activeValidatorSet[delegation.validator] = validator

state.activeValidatorSet.proposerBlockReward += tipCost(tx.fee.tipRate, bytesPaid)
```

#### Begin Block

At the beginning of the block, rewards are distributed to the block proposer.

Apply the following to the state:

```py
proposer = state.activeValidatorSet[block.header.proposerAddress]

# Compute block subsidy and save to state for use in end block.
rewardFactor = (TARGET_ANNUAL_ISSUANCE * BLOCK_TIME) / (SECONDS_PER_YEAR * sqrt(GENESIS_COIN_COUNT))
blockReward = rewardFactor * sqrt(state.activeValidatorSet.activeVotingPower)
state.activeValidatorSet.proposerBlockReward = blockReward

# Save proposer's initial voting power to state for use in end block.
state.activeValidatorSet.proposerInitialVotingPower = proposer.votingPower

state.activeValidatorSet[block.header.proposerAddress] = proposer
```

#### End Block

Apply the following to the state:

```py
account = state.accounts[block.header.proposerAddress]

if account.status == AccountStatus.ValidatorUnbonding
      account.status == AccountStatus.ValidatorUnbonded
    proposer = state.inactiveValidatorSet[block.header.proposerAddress]
else if account.status == AccountStatus.ValidatorBonded
    proposer = state.activeValidatorSet[block.header.proposerAddress]

# Flush the outstanding pending rewards.
proposer.latestEntry += compute_new_entry(proposer.pendingRewards, proposer.votingPower)
proposer.pendingRewards = 0

blockReward = state.activeValidatorSet.proposerBlockReward
commissionReward = proposer.commissionRate.numerator * blockReward // proposer.commissionRate.denominator
proposer.commissionRewards += commissionReward
proposer.pendingRewards += blockReward - commissionReward

# Even though the voting power hasn't changed yet, we consider this a period change.
proposer.latestEntry += compute_new_entry(proposer.pendingRewards, state.activeValidatorSet.proposerInitialVotingPower)
proposer.pendingRewards = 0

if account.status == AccountStatus.ValidatorUnbonding
      account.status == AccountStatus.ValidatorUnbonded
    state.inactiveValidatorSet[block.header.proposerAddress] = proposer
else if account.status == AccountStatus.ValidatorBonded
    state.activeValidatorSet[block.header.proposerAddress] = proposer
```

At the end of a block, the top `MAX_VALIDATORS` validators by voting power with voting power _greater than_ zero are or become active (bonded). For newly-bonded validators, the entire validator object is moved to the active validators subtree and their status is changed to bonded. For previously-bonded validators that are no longer in the top `MAX_VALIDATORS` validators begin unbonding.

Bonding validators is simply setting their status to `AccountStatus.ValidatorBonded`. The logic for validator unbonding is found [here](#signedtransactiondatabeginunbondingvalidator), minus transaction sender updates (nonce, balance, and fee).

This end block implicit state transition is a single state transition, and [only has a single intermediate state root](#blockavailabledata) associated with it.
