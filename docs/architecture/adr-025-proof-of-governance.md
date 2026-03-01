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
    Address string    `json:"address"` // Bech32 address
    Name    string    `json:"name"`
    AddedAt time.Time `json:"added_at"`
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
    Approvals          []string        `json:"approvals"` // Bech32 addresses
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
    Approver   string `json:"approver"` // Bech32 address
    ProposalID uint64 `json:"proposal_id"`
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
    Voter      string    `json:"voter"` // Bech32 address
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

## Module Changes Summary

### New PoG Module Functionality

The PoG module introduces the following functionality:

1. **Committee Member Management**: CRUD operations for committee members with threshold-based voting
2. **Validator Set Proposals**: Create, approve, and execute proposals to modify the validator set
3. **Veto Mechanism**: Token holder veto with dynamic timeout based on veto percentage
4. **Governance Freeze Handling**: Detect freeze conditions and track freeze state
5. **Validator Application Processing**: Accept and track validator applications

### Disabled Staking Module Messages

The following staking module message handlers must be disabled:

- `MsgCreateValidator`: Validators are added through PoG proposals, not self-registration
- `MsgEditValidator`: Validator metadata changes require PoG approval
- `MsgDelegate`: No delegation to validators (no stake-weighted selection)
- `MsgUndelegate`: No undelegation (follows from no delegation)
- `MsgBeginRedelegate`: No redelegation (follows from no delegation)

### Retained Staking Keeper Methods

The following internal keeper methods are retained for use by the PoG module:

- `SetValidator`: Add/update validator in state
- `RemoveValidator`: Remove validator from state
- `GetValidator`: Query validator by operator address
- `GetAllValidators`: Query all validators
- `GetValidatorByConsAddr`: Query validator by consensus address
- `ValidatorsPowerStoreIterator`: Iterate validators by power

### ICA Allowlist Update

The ICA host allowlist in `/app/ica_host.go` must be updated to remove the disabled staking messages.

## Query Endpoints

The PoG module exposes the following gRPC query endpoints:

```protobuf
service Query {
    // Params returns the module parameters
    rpc Params(QueryParamsRequest) returns (QueryParamsResponse);

    // CommitteeMembers returns all committee members
    rpc CommitteeMembers(QueryCommitteeMembersRequest) returns (QueryCommitteeMembersResponse);

    // CommitteeMember returns a specific committee member by address
    rpc CommitteeMember(QueryCommitteeMemberRequest) returns (QueryCommitteeMemberResponse);

    // ValidatorSetProposal returns a specific proposal by ID
    rpc ValidatorSetProposal(QueryValidatorSetProposalRequest) returns (QueryValidatorSetProposalResponse);

    // ValidatorSetProposals returns all proposals with optional status filter
    rpc ValidatorSetProposals(QueryValidatorSetProposalsRequest) returns (QueryValidatorSetProposalsResponse);

    // VetoStatus returns the veto status for a specific proposal
    rpc VetoStatus(QueryVetoStatusRequest) returns (QueryVetoStatusResponse);

    // GovernanceState returns the current governance state (frozen or active)
    rpc GovernanceState(QueryGovernanceStateRequest) returns (QueryGovernanceStateResponse);

    // ValidatorApplications returns all validator applications with optional status filter
    rpc ValidatorApplications(QueryValidatorApplicationsRequest) returns (QueryValidatorApplicationsResponse);

    // ValidatorApplication returns a specific validator application
    rpc ValidatorApplication(QueryValidatorApplicationRequest) returns (QueryValidatorApplicationResponse);
}
```

## Events

The PoG module emits the following events for observability:

```go
// Committee member events
const (
    EventTypeCommitteeMemberAdded   = "committee_member_added"
    EventTypeCommitteeMemberUpdated = "committee_member_updated"
    EventTypeCommitteeMemberRemoved = "committee_member_removed"
)

// Proposal events
const (
    EventTypeProposalCreated   = "proposal_created"
    EventTypeProposalApproved  = "proposal_approved"   // All committee members approved
    EventTypeProposalExecuted  = "proposal_executed"   // Validator set updated
    EventTypeProposalWithdrawn = "proposal_withdrawn"
    EventTypeProposalVetoed    = "proposal_vetoed"     // Freeze threshold reached
)

// Veto events
const (
    EventTypeVetoDeposited = "veto_deposited"
    EventTypeVetoRefunded  = "veto_refunded"  // After proposal execution
    EventTypeVetoSlashed   = "veto_slashed"   // After hardfork resolution
)

// Governance state events
const (
    EventTypeGovernanceFrozen   = "governance_frozen"
    EventTypeGovernanceUnfrozen = "governance_unfrozen"  // After hardfork resolution
)

// Validator application events
const (
    EventTypeValidatorApplicationSubmitted = "validator_application_submitted"
    EventTypeValidatorApplicationApproved  = "validator_application_approved"
    EventTypeValidatorApplicationRejected  = "validator_application_rejected"
)
```

## Genesis State

The PoG module genesis state structure:

```go
type GenesisState struct {
    // Params defines the module parameters
    Params Params `json:"params"`

    // CommitteeMembers is the initial set of committee members
    CommitteeMembers []CommitteeMember `json:"committee_members"`

    // Validators is the initial validator set (mapped from existing PoS validators)
    Validators []ValidatorInfo `json:"validators"`

    // NextProposalID is the ID to assign to the next proposal
    NextProposalID uint64 `json:"next_proposal_id"`

    // GovernanceFreezeState tracks if governance is frozen
    GovernanceFreezeState GovernanceFreezeState `json:"governance_freeze_state"`
}

type ValidatorInfo struct {
    OperatorAddress string    `json:"operator_address"`
    ConsensusPubkey string    `json:"consensus_pubkey"`
    Description     string    `json:"description"`
    Status          string    `json:"status"` // "active" or "reserve"
    AddedAt         time.Time `json:"added_at"`
}
```

## Migration

### Upgrade Path

The transition from Proof of Stake to Proof of Governance requires a **hard fork**. There is no in-place upgrade path because:

1. The fundamental validator selection mechanism changes
2. Staking/delegation state becomes obsolete
3. New committee members must be seeded

### Migration Steps

1. **Coordinate upgrade height**: Announce the upgrade height and ensure all validators are prepared
2. **Export genesis at upgrade height**: Export the chain state at the designated height
3. **Transform genesis**:
   - Handle existing delegations (see Delegation Migration below)
   - Convert existing active validators to PoG validators
   - Add initial committee members (determined through social consensus)
   - Initialize PoG module parameters
4. **Import transformed genesis**: Start the new chain with the transformed genesis
5. **Verify validator set**: Confirm the validator set matches the exported state

### Validator Mapping

Existing validators are mapped to the new system as follows:

- Top N validators (by stake at export) become active validators
- Next M validators become reserve validators
- Remaining validators must re-apply through `MsgValidatorApplication`

### Delegation Migration

The migration must handle all existing delegations and their accrued staking rewards. There are several approaches with different tradeoffs:

#### Option A: Automatic Unstake + Automatic Claim

The migration automatically returns all staked tokens to delegators and claims all pending staking rewards.

**Mechanism:**

- Migration iterates through all delegations and unbonds them
- Migration iterates through all pending rewards and distributes them
- Delegators receive their principal + rewards in their accounts post-migration

**Pros:**

- Clean post-migration state with no legacy staking data
- No action required from delegators
- Simplest user experience

**Cons:**

- Potentially very large state change in a single block
- High computational cost during migration
- Delegators have no control over timing (tax implications in some jurisdictions)
- Must handle edge cases (e.g., validators with pending slashing)

#### Option B: Automatic Unstake + Manual Claim

The migration automatically returns staked tokens but requires delegators to manually claim rewards.

**Mechanism:**

- Migration unbonds all delegations and returns principal to delegators
- Rewards remain in the distribution module until claimed
- A legacy claim mechanism is preserved post-migration

**Pros:**

- Reduces migration block complexity compared to Option A
- Delegators control when they receive rewards (tax timing flexibility)
- Principal is immediately available

**Cons:**

- Must maintain legacy reward claim infrastructure indefinitely
- Dust amounts may never be claimed, leaving state bloat
- More complex post-migration module interactions

#### Option C: Manual Unstake + Manual Claim

Delegators must manually unstake and claim rewards post-migration.

**Mechanism:**

- Migration preserves delegation and reward state
- Legacy `MsgUndelegate` and reward claim messages remain functional
- Delegations can be unwound over time

**Pros:**

- Minimal migration complexity
- Full delegator control over timing
- Spreads state changes over time, reducing any single block's load
- Allows delegators to coordinate with tax advisors

**Cons:**

- Requires maintaining full legacy staking infrastructure
- Funds remain locked until delegators take action
- Some delegators may never claim (lost keys, dust amounts, inattention)
- Ongoing maintenance burden for deprecated functionality
- Complex to reason about system state (mix of PoG and legacy PoS)

#### Recommendation

**Option A (Automatic Unstake + Automatic Claim)** is recommended because:

1. It provides a clean break from PoS with no legacy state to maintain
2. The migration is a hard fork anyway, so a large state change is expected
3. Delegators are guaranteed to receive their funds without taking action
4. No ongoing maintenance of deprecated staking functionality
5. Simpler to audit and verify correctness

The migration should be thoroughly tested with mainnet state exports to ensure it completes within acceptable time bounds.

### Unbonding Period Considerations

In Proof of Stake, the unbonding period (14-21 days) serves critical security functions:

1. **Slashing window**: Allows time to detect and slash misbehavior that occurred before unbonding
2. **Long-range attack prevention**: Prevents validators from unbonding and then attacking historical blocks
3. **Economic security**: Ensures stake remains at risk during the detection period for misbehavior

**Post-migration, these concerns no longer apply:**

- Validators are no longer secured by stake, so there's nothing to slash
- The security model shifts from economic (stake-at-risk) to reputational (committee selection)
- Historical misbehavior detection is handled by the committee, not automated slashing

**Therefore, the unbonding period does not need to apply to post-migration unstaking.** If Option A is chosen, tokens are returned immediately. If Option C is chosen, a new `MsgLegacyUndelegate` could return tokens immediately without an unbonding period, since:

1. The security rationale for the unbonding period no longer exists
2. Delegators should not be penalized with a waiting period for a system change they didn't choose
3. Immediate liquidity allows delegators to participate in the new economic model sooner

## Alternative Approaches

### Use Existing Governance Module

The standard Cosmos SDK `x/gov` module could handle validator proposals through parameter changes or software upgrade proposals.

**Why not chosen**: The gov module lacks committee-specific controls, unanimous consent requirements, and the dynamic veto mechanism. It would require significant modifications that would diverge from upstream.

### Stake-Weighted Committee Selection

Committee members could be selected based on their stake, ensuring those with the most economic interest have governance power.

**Why not chosen**: This reintroduces the capital-weighted authority that PoG aims to eliminate. The goal is to separate governance power from capital concentration.

### Threshold Voting (e.g., 2/3 Majority)

Use a lower consensus threshold for validator set changes instead of requiring unanimous consent.

**Why not chosen**: Unanimous consent provides stronger guarantees for high-stakes decisions like validator set changes. A 2/3 threshold could allow a subset of the committee to force through changes over objections.

### No Veto Mechanism

Implement PoG without the token holder veto, simplifying the system significantly.

**Why not chosen**: The veto mechanism provides a critical check on committee power. Without it, token holders have no recourse if the committee acts against the network's interests. The veto ensures the committee remains accountable to the broader community.

## Consequences

### Positive

- **Reduced Inflation**: Removing staking rewards allows significant reduction in token inflation, benefiting all token holders
- **Quality-Focused Selection**: Validators are selected based on operational quality, reliability, and contribution to the network rather than capital
- **Accountable Governance**: Committee members are explicitly responsible for validator set decisions, creating clear accountability
- **Veto Safety Valve**: Token holders retain ultimate authority through the veto mechanism, preventing committee abuse
- **Simplified Economics**: Without complex staking dynamics, validator economics become more predictable

### Negative

- **Committee Centralization**: Governance power is concentrated in a small committee (initially 10 members), creating potential for coordination or capture
- **Reduced Permissionlessness**: Validators cannot self-select into the active set; they must be approved by the committee
- **Veto Mechanism Complexity**: The dynamic timeout and freeze resolution add complexity and potential edge cases
- **Hardfork Dependency**: Freeze resolution requires social consensus and a hardfork, which is a heavy coordination burden

### Neutral

- **Migration Effort**: Transitioning from PoS requires a hard fork and careful coordination, but this is a one-time cost
- **Different Validator Economics**: Validators no longer earn staking rewards proportional to delegations; compensation model changes
- **Committee Compensation**: The protocol must determine how to compensate committee members for their governance work

## Open Questions

1. Should the veto delay be linear or exponential?
2. Should committee members be modified through committee based voting (2/3) + 1?
3. How much should the committee members be paid in protocol?

## References

- [Proof of Governance as the Endgame for LSTs](https://forum.celestia.org/t/proof-of-governance-as-the-endgame-for-lsts/2036)
- [Endgame: Proof of Governance](https://paragraph.com/@jon-dba/endgame-proof-of-governance)
