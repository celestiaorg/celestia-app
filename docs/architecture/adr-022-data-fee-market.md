# ADR 022: DA Fee Market

## Status

Proposed

## Changelog

- 2023/06/28: Initial draft

## Introduction

At it's core, the Celestia network can be described as a market for the commodity of published data. By outsourcing the publication of data to Celestia, a rollup can mitigate the possibility of equivocation that may cause different machines to execute on a different set and order of transactions resulting in different states. In the field of distributed systems, this is known as split-brain. Thus, the market encompasses buyers who are willing to pay various amounts on the publication of their data, and sellers who are willing to supply the machines to perform the computation, storage and networking as defined by the protocol. This document explores the various dynamics of this market. It attempts to describe the desirable outcomes and to form discussion around how the many components that make up the system should be designed with those outcomes in mind.

## Context

There are several imporant considerations regarding Celestia's fee market model:

- **A Single Decentralized Seller**: While there are multiple competing buyers, the group of "validators" acting as sellers must collectively perform the computation, storage and networking that is required to produce the service. They may incur different costs in running their machines and receive different slices of the overall revenue generated (see non-linear distribution) but nonetheless must perform mostly the equivalent service. More importantly, the quality of the service is proportional to the level of decentralization of the network. More evenly weighted nodes makes the network more resilient to attacks and censorship and improves the reliability to accessing the published data. In summary, the force to continually lower costs to buyers and to remain a competitive market may reduce the amount of sellers, yet counter to this, the security and thus the quality of the service is dependent on a healthy amount of sellers. The market needs to balance this.
  - **Single Decider** Currently the price for publishing data is set by a single proposer, which gets rotated per height. The decider, however, earns a small fraction in return. This means that it may be more profitable to turn to secondary markets (explored later in this document), and to look at other revenue streams (MEV). A clear example is that it would cost a fraction of the price to convince a profit-driven validator to censor a transaction than the underlying fee that the transaction contains (as the proposer themself may get less than 1% of the fee).
  - **Highly Non-linear Distribution**: Celestia is secured using a nominated proof of stake system. Particpants are incentivised to stake or delegate their tokens through rewards which are generated through a combination of fees and minted tokens. The rewards are then distributed in proportion to the amount staked. In practice there is a significant amount of inequality in distribution. It is common for networks of 100 to 200 validators to have 1/3 of the revenue earned by the top 6 or 7 validators.
- **Bounded Supply**: The supply of block space can be seen as a bounded commodity. At a point, buyers begin to compete for space as they hit the upper bound on what the network can provide. Crossing this line could result in price wars with rollups bidding against each other. Before that point, all data could be published so long as they are seen by the validators to cover the costs. Improvements in scaling of the protocol can naturally shift that upper bound. As will be discussed in greater detail, this isn't problematic.
- **Different Denominations**: The denomination of the transaction between the buyer and seller, TIA, differs to the denomination of the costs that the seller incurs (i.e. USD, EUR which server providers like AWS or Hetzner charge in). The buyer may also need to bridge price differences between TIA and the currency that they receive from their users. As the relative prices may fluctuate due to market forces beyond the purview of the Celestia network, Celestia in turn may incorrectly price the cost of publishing data.
- **Secondary Markets and MEV**: If Celestia is able to offer value to it's users but is not able to correctly capture that value, it risks being captured on secondary markets that auction block space off chain and use other currencies. Moving payment offchain reduces the utility of the TIA token that secures the network. Actors such as validators who wish to extract as much value as possible (MEV) may make selfish decisions that harm the integrity of the network as a whole.
- **Price Elasticity and Competition**: Price elasticity refers to how much a buyers demand for a commodity changes as the price changes. Given a Rollups strong dependence on Celestia for liveness (their service stops if they are unable to submit data), the commodity is rather inelastic. The eventual increase in competition and fallback methods that allow a rollup to switch providers may reduce the elasticity. Inelastic goods generally gives greater power to the seller.

## Decision

Celestia's fee market should fall between the bounds of being cost-effective for both suppliers (validators) and buyers (rollups). It should endeavour to provide the greatest price certainty and use the resource of the network in the most efficient manner. To this extent the following decisions were made:

- **Global Min Gas Price**: A new block validity rule has been introduced constraining the minimum gas price (in other words the fee divided by the gas limit) that a transaction can have. This is designed to mitigate the ability for payment to be settled off-chain. It also resolves the incongruence of nodes with different minimum limits. This value can be modified via on-chain governance. This therefore means there is a dependency on the community interested in the value of TIA to ensure that a minimum price does not lead to an uncompetitive price.
- 

## Detailed Design

## References

- [Consider a global minimum gas price #1621](https://github.com/celestiaorg/celestia-app/issues/1621)
- [staking: introduce total delegation to self-delegation threshold #1480](https://github.com/celestiaorg/celestia-app/issues/1480)
