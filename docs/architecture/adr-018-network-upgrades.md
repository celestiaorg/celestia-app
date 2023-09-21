# ADR 018: Network Upgrades

## Status

Proposed

## Changelog

- 2023/03/29: Initial draft
- 2023/08/29: Modified to reflect the proposed decision and the detailed design

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

### Deciding And Relaying Upgrade Height

Minor upgrades can be performed by the node operator at any time, as they wish. Major upgrades (with state machine breaking changes) must be coordinated at the same height. There are three ways this coordination can be achieved:

- **Hardcoded**: The upgrade height can be hardcoded into the binary. When that upgrade height is reached, the relevant logic is routed appropriately. This approach is simple but not flexible. We run many different testnets, including countless ephemeral ones, and each will have a different upgrade height. Not only do all the upgrade heights for each network have to be handled, but it requires a new major release to change. This means that if for some reason social consensus decides to postpone the upgrade, then a new release must be created. The main risk is that this approach is prone to errors that could halt the network if social consensus is not reached.
- **Configured**: To fix the lack of flexibility of the hardcoded upgrade height approach, we could use a configurable approach. This would involve some config that indicates which version of the application should be used at which height. Since each height is configurable, then the heights can be changed without changing the binary. This way we could change the heights per each testnet. However, this flexibility also has the downside of confused validators or users accidently halting the chain by changing the default config.
- **Signaled**: The upgrade height could also be signaled in protocol. This would involve each validator using some signaling mechanism to indicate that they are ready to upgrade. After some threshold has signaled that an upgrade will occur, then the upgrade height is determined. Unlike the hardcoded approach, signaling does not risk halting the network, but it moves the decision to upgrade closer to the validators. Even if social consensus is reached, it's possible for the validators to simply never signal that they are ready to upgrade. As discussed above, to mitigate this risk, some halt height could be added.

## Decision

The following decisions have been made:

1. All upgrades (barring social hard forks) are to be rolling upgrades. That is node operators will be able to restart their node ahead of the upgrade height. The node will continue running the current version of the upgrade but will be capable of validating and executing transactions of the version being upgraded to. This makes sense given the decision to have single binary syncs (i.e. support for all prior versions). As validators are likely to be running nodes all around the world, it reduces the burden of coordinating a single time for all operators to be online. It also reduces the likelihood of failed upgrade and automates the process meaning generally less downtime between versions.
2. Upgrade coordination will be rolled out in two phases. The first (v1 -> v2) will rely on a configured height to move from one version to the next. The binary will be released with a default height which can be modified later by validators in the event that it needs to be pushed back (or forward). The second phase (v2 -> v3) will use a signalling mechanism whereby validators who are now running on the latest binary will signal that they are ready to shift to the next version.

## Detailed Design

The design depends on a versioned state machine whereby the app version displayed in each block and agreed upon by all validators is the version that the transactions are both validated and executed against. If the celestia state machine is given a block at version 1 it will execute it with the v1 state machine if consensus provides a v2 block, all the transactions will be executed against the v2 state machine.

Given this, a node can at any time spin up a v2 binary which will immediately be able to continue validating and executing v1 blocks as if it were a v1 machine.

The mechanism that dictates which versioned block to agree upon, begins with the app in `EndBlock` of the previous height. There, as a `VersionParams`, the application indicates the version they expect the network to have agreed upon. The proposer of the following height then proposes a block with the new app version. If a validator has the same app version and everything else is correct, they will vote for it else if they are on a different version they will PREVOTE and PRECOMMIT nil, signalling to move to the next round. If less than 2/3+ validators have upgraded, the network will be unable to reach consensus. If the upgrade has failed, then the validators that upgraded can simply downgrade and continue to produce blocks on the original version (even if they are still running the binary of the latest version).

### Phase 1: Configured Upgrade Height

The height of the upgrades will initially be coordinated via the `app.toml` config file under a seprate upgrades section. This will consist of a mapping from chain ID to height to app version that will be loaded by the application into working memory whenever the node begins. The `upgrades` module will simply take in this map and a reference to the `ParamStore` which it can use to set the new app version at the appropriate height. For safety, users will not be able to specify an app version that is greater than what the binary supports (i.e. 10 for v8). There is no rule preventing users from specifying a downgrade to an older version or a version that skips values. For convenience to node operators, a default mapping can be included in the binary such that the node operators simply need to stop the node, download the appropriate binary and restart the node.

### Phase 2: Signaled Upgrade Height

Preconfigured upgrade paths are vulnerable to halts. There is no indication that a quorum has in fact upgraded and that when the proposer proposes the block with the latest version, that consensus will be reached. To mitigate this risk, the upgrade height can instead be signaled by validators. Vote Extensions may appear as a good tool for this but it is inefficient to continually signal every height. Validators should only need to signal once. There are two possible approaches:

- **Off-chain**: A new p2p reactor is introduced whereby validators sign a message indicating they are now running a new binary and are ready to switch. Once a proposer has received a quorum plus some predefined grace period, they will propose a block with the new version and the rest of the network will vote accordingly. This approach means that the application doesn't have control but rather has to listen for changes in the app version. This also requires a change to the `PrivValidator` interface to be able to sign the new message.
- **On-chain**: Upon upgrading to a new binary, the node will submit a transaction signalling it's ability to switch version. Again after a quorum is reached and some grace period, the `upgrade` module would trigger the app version change in `EndBlock`. The drawback with this approach is that this would probably require gas to submit in order to avoid spamming the network and wouldn't necessarily be automatic i.e. nodes could upgrade and forget to signal.

## References

- [EPIC: Social Upgrades](https://github.com/celestiaorg/celestia-app/issues/1014)
