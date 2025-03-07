# ADR 018: Network Upgrades

## Status

Proposed

## Changelog

- 2023/03/29: Initial draft
- 2023/08/29: Modified to reflect the proposed decision and the detailed design
- 2023/10/14: Update ADR to reflect changes in implementation
- 2024/05/14: Update ADR to reflect the lack of hardcoded upgrade heights

## Context

There are three requirements in Celestia's upgrading mechanism that diverges from how the standard Cosmos SDK currently operates:

- The ability for non-validators to easily exit the system.
- Minimal downtime. As a core piece of infrastructure that the rollup community depends on, Celestia needs to be highly available. This means reducing the downtime for upgrades through effective coordination and minimal migrations.
- Full chain backwards compatibility. The latest software version should be able to correctly process all transactions of the chain since genesis in accordance with the version of the app that was in use at the time of each transaction's block.

### Rolling vs Stopping Upgrades

A rolling upgrade involves nodes upgrading to the binary ahead of time, and that new binary will automatically switch to the new consensus logic at a provided height. Stopping upgrades require all nodes in the network to halt at a provided height, and collectively switch binaries. All upgrades can occur in a rolling or stopping fashion, but doing so in a rolling fashion requires more work. Fortunately, since we're already supporting single binary syncs, the vast majority of upgrades will be able to roll with very little additional changes.

There are however still a very small percentage of changes that require significantly more work to become rolling. It can still be done, it simply requires more work per each of those types of upgrades.

### Balancing Hardfork and Halting Risk

One of the main difficulties of social upgrades when using tendermint consensus is finding a balance between risking a halt and ending up in a situation where the community must hardfork. If social consensus is reached, but validators do not incorporate the upgrade, then a hardfork must be performed. This involves changing the chain-id and the validator set, which would force the governance of connected IBC chains to recognize the changes in order to preserve the funds of Celestia token holders that have bridged to one of those chains.

One mechanism that has been proposed is to add some halt height for light clients and consensus nodes. This halt height could be determined before the upgrade binary is released, or it could be incorporated to the upgraded binary. The important feature of such mechanisms is to set a deadline for validators to upgrade. If a solution cannot be agreed upon by all parties offchain by that point, then a fork will be created by the community.

## Decision

The following decisions have been made:

All upgrades (barring social hard forks) are to be rolling upgrades. That is node operators will be able to restart their node ahead of the upgrade height. The node will continue running the current version of the upgrade but will be capable of validating and executing transactions of the version being upgraded to. This makes sense given the decision to have single binary syncs (i.e. support for all prior versions). As validators are likely to be running nodes all around the world, it reduces the burden of coordinating a single time for all operators to be online. It also reduces the likelihood of failed upgrade and automates the process meaning generally less downtime between versions.

## Detailed Design

The design depends on a versioned state machine whereby the app version displayed in each block and agreed upon by all validators is the version that the transactions are both validated and executed against. If the celestia state machine is given a block at version 1, it will execute it with the v1 state machine if consensus provides a v2 block, all the transactions will be executed against the v2 state machine.

Given this, a node can at any time spin up a v2 binary, which will immediately be able to continue validating and executing v1 blocks as if it were a v1 machine.

### Configured Upgrade Height

The height of the v1 -> v2 upgrade will initially be supplied via CLI flag (i.e. `--v2-upgrade-height`). There are a few considerations that shape how this system will work:

- Upgrading needs to support state migrations. These must happen to all nodes at the same moment between heights. Ideally all migrations that affect state would correspond at the height of the new app version i.e. after `Commit` and before processing of the transactions at that height. `BeginBlock` seems like an ideal area to perform these upgrades however these might affect the way that `PrepareProposal` and `ProcessProposal` is conducted thus they must be performed even prior to these ABCI calls. A simpler implementation would have been for the proposer to immediately propose a block with the next version i.e. v2. However that would require the proposer to first migrate state (taking an unknown length of time) and for the validators receiving that proposal to first migrate before validating and given that the upgrade is not certain, there would need to be a mechanism to migrate back to v1 (NOTE: this remains the case if we wish to support downgrading which is discussed later). To overcome these requirements, the proposer must signal in the prior height the intention to upgrade to a new version. This is done with a new message type, `MsgVersionChange`, which must be put as the first transaction in the block. Validators read this and if they are in agreement to supporting the version change they vote on the block accordingly. If the block reaches consensus then all validators will update the app version at `EndBlock`. CometBFT will then propose the next block using that version. Nodes that have not upgraded and don't support the binary will error and exit. Given that the previous block was approved by more than 2/3 of the network we have a strong guarantee that this block will be accepted by the network. However, it's worth noting that given a security model that must withstand 1/3 byzantine nodes, even a single byzantine node that voted for the upgrade yet doesn't vote for the following block can stall the network until > 2/3 nodes upgrade and vote on the following block.
- Given the uncertainty in scheduling, the system must be able to handle changes to the upgrade height that most commonly would come in the form of delays. Embedding the upgrade schedule in the binary is convenient for node operators and avoids the possibility for user errors. However, binaries are static. If the community wished to push back the upgrade by two weeks there is the possibility that some nodes would not rerun the new binary thus we'd get a split between nodes running the old schedule and nodes running the new schedule. To overcome this, proposers will only propose a version change in the first round of each height, thus allowing transactions to still be committed even under circumstances where there is no consensus on upgrading. Secondly, we define a range in which nodes will attempt to upgrade the app version and failing this will continue to run the current version. Lastly, the binary will have the ability to manually specify the app version height mapping and override the built-in values either through a flag or in the `app.toml` config. This is expected to be used in testing and in emergency situations only. Another example to keep in mind is if a quorum outright rejects an upgrade. If some of the validators are for the change they should have some way to continue participating in the network. Therefore we employ a range that nodes will attempt to upgrade and afterwards will continue on normally with the new binary however running the older version.
- The system needs to be tolerant of unexpected faults in the upgrade process. This can be:
  - The community/contributors realizes there is a bug in the new version after the binary has been released. Node operators will need to downgrade back to the previous version and restart their node.
  - There is a halting bug in the migration or in processing of the first transactions. This most likely would be in the form of an apphash mismatch. This becomes more problematic with delayed execution as the block (with v2 transactions) has already been committed. Immediate execution has the advantage of the apphash mismatch being realized before the data is committed. It's still however feasible to overcome this but it involves nodes rolling back the previous state and re-executing the transactions using the v1 state machine (which will skip over the v2 transactions). This means node operators should be able to manually override the app version that the proposer will propose with. Lastly, if state migrations occurred between v2 and v1, a reverse migration would need to be performed which would make things especially difficult. If we are unable to fallback to the previous version and continue then the other option is to remain halted until the bug is patched and the network can update and continue
  - There is a bug that is detected that could halt the chain but hasn't yet. There are other things we can develop to combat such scenarios. One thing we can do is develop a circuit breaker similar to the designs proposed in [Cosmos SDK](https://github.com/cosmos/cosmos-sdk/tree/main/x/circuit). This can disable certain message types or modules either in `CheckTx` or `ProcessProposal`. This violates the consistency property between `PrepareProposal` and `ProcessProposal` but so long as a quorum are the same, will still allow the chain to progress (inconsistency here can be interpreted as byzantine).

### Future Work: Signaled Upgrade Height

Preconfigured upgrade paths are vulnerable to halts. There is no indication that a quorum has in fact upgraded and that when the proposer proposes a block with the message to change version, that consensus will be reached. To mitigate this risk, the upgrade height can instead be signaled by validators. A version of `VoteExtension`s may be the most effective at ensuring this. Validators upon start-up will automatically signal a version upgrade when they go to vote (i.e. `ExtendedVote`), so long as the latest supported version differs from the current network version. In `VerifyVoteExtension`, the version will be parsed and persisted (although not part of state). There is no verification. Upon a certain threshold which must be at least 2/3+ but could possibly be greater, the next proposer, who can support this version will propose a block with the `MsgVersionChange` that the quorum have agreed to. The rest works as before.

For better performance, `VoteExtensions` should be modified such that empty messages don't require a signature (which is currently the case for v0.38 of [CometBFT](https://github.com/cometbft/cometbft/blob/91ffbf9e45afb49d34a4af91b031e14653ee5bd8/privval/file.go#L324))

#### Alternatives

There are two alternative approaches that were considered:

- **Off-chain**: A new p2p reactor is introduced whereby validators sign a message indicating they are now running a new binary and are ready to switch. Once a proposer has received a quorum plus some predefined grace period, they will propose a block with the new version and the rest of the network will vote accordingly. This approach means that the application doesn't have control but rather has to listen for changes in the app version. This also requires a change to the `PrivValidator` interface to be able to sign the new message.
- **On-chain**: Upon upgrading to a new binary, the node will submit a transaction signaling its ability to switch version. Again after a quorum is reached and some grace period, the `upgrade` module would trigger the app version change in `EndBlock`. The drawback with this approach is that this would probably require gas to submit in order to avoid spamming the network and wouldn't necessarily be automatic i.e. nodes could upgrade and forget to signal.

### Future Work: Downgrading

A more resilient system will have the option for a coordinated downgrade. This doesn't necessarily need to be because of a liveness or safety bug but could simply arise because of a degradation in service. Coordination can either be done through the configured upgrade heights or through the aforementioned signalling mechanism. The downgrade process is similar to the upgrade process. The proposer will propose a block with the `MsgVersionChange` to downgrade to the previous version. The validators will vote on this so long as it matches their local configured upgrade schedule and if consensus is reached, the app version will be downgraded at `EndBlock`. The next proposer will propose a block with the new version and the network will continue on as normal.

Similarly, if a migration occurred between the two versions, a reverse migration will need to be performed at the end of `Commit`. This obviously requires extra work and testing in supporting this functionality.

## References

- [EPIC: Social Upgrades](https://github.com/celestiaorg/celestia-app/issues/1014)
