# ADR 018: Social Upgrades

## Status

Proposed

## Changelog

- 2023/03/29: Initial draft

## Context

One of celestia's core principles is social consensus, this is why the standard cosmos-sdk upgrade mechanism that relies on token voting has been removed. We still need a mechanism to upgrade after social consensus has been reached, and that is what this document aims to clarify.

### Rolling vs Stopping Upgrades

A rolling upgrade involves nodes upgrading to the binary ahead of time, and that new binary will automatically switch to the new consensus logic at a provided height. Stopping upgrades require all nodes in the network to halt at a provided height, and collectively switch binaries. All upgrades can occur in a rolling or stopping fashion, but doing so in a rolling fashion requires more work. Fortunately, since we're already supporting single binary syncs, the vast majority of upgrades will be able to roll with very little additional changes.

There are however still a very small percentage of changes that require significantly more work to become rolling. It can still be done, it simply requires more work per each of those types of upgrades.

### Balancing Hardfork and Halting Risk

One of the main difficulties of social upgrades when using tendermint consensus is finding a balance between risking a halt and ending up in a situation where the community must hardfork. If social consensus is reached, but validators do not incorporate the upgrade, then a hardfork must be performed. This involves changing the chain-id and the validator set, which would force the governance of connected IBC chains to recognize the changes in order to preserve the funds of Celestia token holders that have bridged to one of those chains.

One mechanism that has been proposed is to add some halt height for light clients and consensus nodes. This halt height could be determined before the upgrade binary is released, or it could be incorporated to the upgraded binary. The important feature of such mechanisms is to set a deadline for validators to upgrade. If a solution cannot be agreed upon by all parties offchain by that point, then a fork will be created by the community.

### Deciding And Relaying Upgrade Height

One of the main decisions that needs to be made is how to decide and convey the upgrade height to the application. Each has trade offs on the halting vs hardfork risk.

#### Hardcoded

The upgrade height can be hardcoded into the binary. When that upgrade height is reached, the relevant logic is routed appropriately. This approach is simple but not flexible. We run many different testnets, including countless ephemeral ones, and each will have a different upgrade height. Not only do all the upgrade heights for each network have to be handled, but it requires a new major release to change. This means that if for some reason social consensus decides to postpone the upgrade, then a new release must be created. The main risk is that this approach is prone to errors that could halt the network if social consensus is not reached.

#### Configured

To fix the lack of flexibility of the hardcoded upgrade height approach, we could use a configurable approach. This would involve some config that indicates which version of the application should be used at which height. Since each height is configurable, then the heights can be changed without changing the binary. This way we could change the heights per each testnet. However, this flexibility also has the downside of confused validators or users accidently halting the chain by changing the default config.

#### Signaled

The upgrade height could also be signaled in protocol. This would involve each validator using some signaling mechanism to indicate that they are ready to upgrade. After some threshold has signaled that an upgrade will occur, then the upgrade height is determined. Unlike the hardcoded approach, signaling does not risk halting the network, but it moves the decision to upgrade closer to the validators. Even if social consensus is reached, it's possible for the validators to simply never signal that they are ready to upgrade. As discussed above, to mitigate this risk, some halt height could be added.

The actual mechanism to signal could involve signaling in the state machine (not entirely unlike the current upgrade module), via vote extensions, or simply gossiped.

## Decision

We need to make three different decisions:

### Rolling vs Stopping

It seems that the majority of readers are in favor of rolling, and since the vast majority of upgrades can be rolling with no additional work to single binary syncs, this seems like an obvious decision. However, whether we commit to doing 100% of upgrades to be rolling is tbd.

### Determining Upgrade Height

Per synchronous discussions, it seems that we are in favor of using a signaled approach given this eliminates the risk of a halt.

### Add a halting height

As discussed above, we could add a halt height to the upgrade binary. This would set a deadline for the upgrade. If the community reaches social consensus, then they will use the upgraded binary. If the validators do not upgrade by that time, then the network will halt. If the deadline is distant enough in the future, and the validators refuse to upgrade, then all participants, including connected IBC chains, could prepare for the fork.

## Detailed Design

Adding a halt height to the header is described in the removal of token voting ADR, and the other mechanisms will be described in further detail after deciding upon them.

## References


