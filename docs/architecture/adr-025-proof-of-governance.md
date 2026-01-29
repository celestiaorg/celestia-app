# ADR 025: Proof of Governance

## Status

Draft

## Changelog

- 2025-01-28: Initial draft

## Context

Currently validators are included in the active set based on stake. The top 100 validators based on stake are part of the active set and the remainder are part of the inactive set. PoG eliminates the selection based on stake. Instead a committee (initially 10 individuals) selects the validator set.

## Decision

Implement a Proof of Governance (PoG) module for Celestia, which replaces stake-weighted validator selection with committee-controlled validator set management.

### Motivation

- Proof of Governance (PoG) replaces capital-weighted authority with accountable, explicit decision-making focused on validator quality rather than stake size.
- By removing staking altogether and introducing a new validator selection mechanism, PoG enables a substantial reduction in inflation, bringing it closer to a level that primarily covers validator operational costs.
- Under PoG, validator set changes are decided through a unanimous committee vote, ensuring that all governance entities explicitly approve high-impact decisions. This is combined with a veto mechanism with a dynamic timeout, which allows token holders to delay or halt decisions they strongly disagree with. If a veto reaches the freeze threshold, the system requires a hardfork and reverts to social consensus to determine whether the veto was malicious (in which case vetoed funds are slashed) or whether governance acted maliciously and must be replaced.

## Detailed Design

### Design Constraints

1. Separation of concerns: PoG module handles committee operations; staking module only handles internal validator set state.

### Module Parameters

```go
type Params struct {
    // ActiveValidatorCount is the number of validators that are actively participating in the validator set.
    ActiveValidatorCount  uint64 // 100
    // ReserveValidatorCount is the number of validators that are in the reserve for the validator set.
    ReserveValidatorCount uint64 // 20

    // MinVetoDeposit is the minimum amount of TIA to deposit to veto a proposal
    MinVetoDeposit math.Int // 100 TIA
    // InitialVetoDuration is the initial duration that a proposal can be vetoed
    InitialVetoDuration time.Duration // Default 1 week on Mainnet, 1 day on Mocha
    // FirstDelayThreshold is the threshold at which the veto duration is increased
    FirstDelayThreshold math.LegacyDec // 1%
    // FreezeThreshold is the threshold at which the proposal is frozen
    FreezeThreshold     math.LegacyDec // 10%

    // ThresholdToModifyCommitteeMember is the threshold needed to modify a committee member
    ThresholdToModifyCommitteeMember math.LegacyDec // (2/3) + 1
    // ThresholdToModifyValidatorSet is the threshold needed to modify the validator set
    ThresholdToModifyValidatorSet math.LegacyDec // 100%

    // RequiredBondedAmount is the minimum amount of TIA that a validator must bond to be considered for the validator set
    RequiredBondedAmount math.Int // 50,000 TIA
    // RequiredLiquidAmount is the minimum amount of TIA that a validator must have liquid to be considered for the validator set
    RequiredLiquidAmount math.Int // 50,000 TIA
}
```

### Committee Members

The PoG module defines committee members - addresses with permissions to update validator set state.

```go
type CommitteeMember struct {
    Address sdk.AccountI `json:"address"`
    Name    string       `json:"name"`
    AddedAt time.Time    `json:"added_at"`
}
```

### CRUD Operations on Committee Members

Committee members can be added, updated, or removed through threshold-based voting among existing committee members.

```go
type CommitteeMemberChangeProposal struct {
    ProposalID uint64           `json:"proposal_id"`
    ChangeType EntityChangeType `json:"change_type"`
    Entity     CommitteeMember  `json:"entity"`
    Approvals  []string         `json:"approvals"`
    Status     ProposalStatus   `json:"status"`
}

type EntityChangeType int32

const (
    EntityAdd    EntityChangeType = 1
    EntityUpdate EntityChangeType = 2
    EntityRemove EntityChangeType = 3
)
```

### Unanimous Consortium Vote + Veto with Dynamic Timeout

#### Proposal Creation

Committee members can create proposals to modify the validator set.

```go
type ValidatorSetProposal struct {
    ProposalID         uint64          `json:"proposal_id"`
    Proposer           string          `json:"proposer"`
    Title              string          `json:"title"`
    Description        string          `json:"description"`
    VetoDeposits       []sdk.Coins     `json:"veto_deposits"`
    ValidatorsToAdd    []ValidatorInfo `json:"validators_to_add"`
    ValidatorsToUpdate []ValidatorInfo `json:"validators_to_update"`
    ValidatorsToRemove []ValidatorInfo `json:"validators_to_remove"`
    Approvals          []sdk.AccountI  `json:"approvals"`
    Status             ProposalStatus  `json:"status"`
    SubmittedAt        time.Time       `json:"submitted_at"`
    ApprovedAt         time.Time       `json:"approved_at"`
    ExecutableAt       time.Time       `json:"executable_at"`
}

type ProposalStatus int32

const (
    StatusPending   ProposalStatus = 1
    StatusApproved  ProposalStatus = 2
    StatusExecuted  ProposalStatus = 3
    StatusWithdrawn ProposalStatus = 4
    StatusVetoed    ProposalStatus = 5
)
```

### Proposal Lifecycle

#### Step 1: Creation

- Only committee members can submit validator modification proposals
- Proposal enters PENDING status

#### Step 2: Voting (Unanimous Required)

- All committee members must approve
- Any member can vote: Approve
- Once ALL members approve → APPROVED status
- Timeout period begins

#### Step 3: Timeout Period (Subject to Veto)

- Base timeout: 1 week
- Token holders can veto proposals
- Veto amount extends timeout dynamically
- If veto < 10%: Continue to execution
- If veto >= 10%: GOVERNANCE FROZEN

#### Step 4: Execution

- Timeout expires & no freeze
- Any member triggers execution
- Validator set updated in state
- Proposal marked EXECUTED

### Validator Set Update Execution

When a proposal passes the timeout period, it can be executed to update the validator set.

## Veto Mechanism

### Dynamic Timeout Calculation

The governance vote can be vetoed, where the amount of veto delays the vote proportionally.

| Veto % (of unlocked supply) | Effect                                                         |
|-----------------------------|----------------------------------------------------------------|
| 0%                          | Base timeout (1 week)                                          |
| 1%                          | First delay increase triggered. Gives time to unwind positions |
| 1% - 10%                    | Dynamic delay proportional to veto                             |
| 10%                         | GOVERNANCE FROZEN. No new proposals allowed                    |

## Resolution Through Hardfork

When governance is frozen (10% veto threshold reached), the only resolution is a hardfork with one of two outcomes:

### Option A: Veto was malicious

- All veto funds are slashed
- Governance proposal proceeds to execution
- Demonstrates consequences for bad-faith vetoes
- Governance unfrozen

### Option B: Governance was malicious

- Committee members must be replaced
- Proposal is rejected permanently
- New committee election/appointment triggered
- Veto funds returned

### Freeze State

```go
type GovernanceFreezeState struct {
    IsFrozen         bool       `json:"is_frozen"`
    FrozenAt         time.Time  `json:"frozen_at"`
    FrozenByProposal uint64     `json:"frozen_by_proposal"`
    TotalVetoAmount  math.Int   `json:"total_veto_amount"`
    VetoReasons      []VetoVote `json:"veto_reasons"`
}

type FreezeResolution int32

const (
    ResolutionSlashVetoers      FreezeResolution = 1
    ResolutionReplaceGovernance FreezeResolution = 2
)
```

## Validator Set Management

### Score-Based Automatic Removal

Validators are automatically removed based on objective metrics:

1. Double signing: validator gets tombstoned
2. Missed proposals: if validator misses N proposals in M blocks, it gets jailed

### Validator Lifecycle

1. Submit a `MsgValidatorApplication` with required bonded and liquid amounts
2. Application is either rejected with comments or approved as a candidate
3. Approved candidates participate on testnet
4. After participating on testnet, validators can be elevated to mainnet validator status through a committee proposal
5. Validators that underperform or violate rules are automatically jailed or tombstoned

## Staking Module Modifications

### Changes Required

The existing staking module should be modified to:

1. Remove external CRUD operations: No more `MsgCreateValidator`, `MsgEditValidator`, `MsgDelegate`, `MsgUndelegate`, `MsgBeginRedelegate`
2. Keep internal functions: Validator state management, power calculation, ABCI updates
3. PoG module calls internal functions: PoG module uses staking keeper's internal methods

```go
// REMOVE these message handlers from staking module:
// - MsgCreateValidator
// - MsgEditValidator
// - MsgDelegate
// - MsgUndelegate
// - MsgBeginRedelegate

// KEEP these internal keeper methods (used by PoG module):
// - SetValidator
// - RemoveValidator
// - GetValidator
// - GetAllValidators
// - GetValidatorByConsAddr
// - ValidatorsPowerStoreIterator
```

## Message Types

### Committee Member Messages

```go
type MsgProposeCommitteeChange struct {
    Proposer   string           `json:"proposer"`
    ChangeType EntityChangeType `json:"change_type"`
    Member     CommitteeMember  `json:"entity"`
}

type MsgApproveCommitteeChange struct {
    Approver   string `json:"approver"`
    ProposalID uint64 `json:"proposal_id"`
}
```

### Validator Set Proposal Messages

```go
type MsgApproveProposal struct {
    Approver   sdk.AccountI `json:"approver"`
    ProposalID uint64       `json:"proposal_id"`
}

type MsgWithdrawProposal struct {
    Proposer   string `json:"proposer"`
    ProposalID uint64 `json:"proposal_id"`
    Reason     string `json:"reason"`
}
```

### Veto Messages

```go
type MsgVoteVeto struct {
    ProposalID uint64    `json:"proposal_id"`
    Amount     sdk.Coins `json:"amount"`
    Reason     string    `json:"reason"`
}
```

### Validator Application Messages

```go
type MsgValidatorApplication struct {
    Applicant    string `json:"applicant"`
    Description  string `json:"description"`
    BondedAmount math.Int `json:"bonded_amount"`
    LiquidAmount math.Int `json:"liquid_amount"`
}
```

## Open Questions

1. Should the veto delay be linear or exponential?
2. Should committee members be modified through committee based voting (2/3) + 1?
3. How much should the committee members be paid in protocol?

## References

- [Proof of Governance as the Endgame for LSTs](https://forum.celestia.org/t/proof-of-governance-as-the-endgame-for-lsts/2036)
- [Endgame: Proof of Governance](https://paragraph.com/@jon-dba/endgame-proof-of-governance)
