# ADR 022: Streamline system architecture

## Changelog

- 2023/10/19: Initial draft

## Status

Proposed

## Context

The Celestia network is divided into two subsystems owing largely to two different networking implementations. Some of the justification around this decision is as follows:

- Tendermint consensus was battle tested and widely used yet tightly coupled to the networking layer. Using it as close to out of the box as possible meant a massive reduction in engineering resources to reach product launch.
- Tendermint networking layer was largely unfamiliar to the team and had known frailties. LibP2P was more familiar, had more documentation and community support. It also provided functionality that was not available in Tendermint.

This decision led to the creation of `celestia-node`, a new repository, responsible for data availability sampling and data retrieval. As a consequence of this decision:

- A bridge node was introduced which had to be run side by side with the consensus node, relaying data from the consensus node to the nodes in the DA network (i.e. using LibP2P). This meant:
  - Extra complexity for node operators
  - Duplicate storage of squares
  - Redundant computation of the extended data square from the block
  - Extra round trip in retrieving the data and pushing it (latency costs).
- A secondary implementation of a storage engine which largely overlaps with the KV based storage engine in the consensus node.
- A secondary implementation for exchanging and verifying data: `go-header`
- Two binaries that have different usage patterns: commands, config files, flags etc. This makes it more confusing for users.

Having duplication requires both a broader knowledge base and more maintenance load for engineers. There is a larger surface area for bugs and gains from optimizations are not captured as efficiently as they could.

Outside of the aforementioned tech debt, there is untapped potential in taking the benefits of LibP2P and porting it across to the Tendermint consensus algorithm. LibP2P has a more sophisticated and efficient gossiping protocol than Tendermint's. It also has a more robust peer discovery and peer management system.

## Decision

The decision is to converge to a LibP2P based network and to shape out reusable components that can be used by all node types: consensus, data and light. This is to be gradually done across four phases, each phase likely requiring it's own in-depth ADR:

### Phase 1: Removal of the bridge node

The first step involves porting `Init` and `Start` methods from `celestia-app` and allowing consensus nodes to be initialized and run through the `celestia` binary. This will allow for a arguemnt to be passed in `Start` that enables the consensus nodes to directly push the extended data square (EDS) that is finalized in `FinalizeBlock` to the LibP2P based networks. Thus consensus nodes will temporarily be a hybrid of both networking systems and replace the need for a separate bridge node. Having a single binary will greatly simplify both the release process and end-to-end testing.

### Phase 2: Migration of storage, syncing and fraud

The next step migrates first the storage engine and then the syncing and fraud protocols (`go-header` and `go-fraud`) to `celestia-app`. This replaces the existing `BlockStore` and blocksync and evidence reactors. Streamlining the storage will require careful migration but will eliminate the duplication of storage and unify many of the data exchange protocols and the public APIs. Here, the consensus node can become more integrated with the `nodebuilder` package as it begins to be split up into a series of modules. This phase may also try to align configuration files and the general file directory.

### Phase 3: Rewiring consensus, txpool and statesync

The third phase consists of replacing the interface between the consensus, mempool and statesync protocols to Tendermint's p2p layer with LibP2P. This can be done in tandem with phase 2 and may be coupled with other refactors and performance improvements to those components. This will manifest itself as four new modules to `celestia-node`: consensus, mempool, statesync, and execution (a standalone Celestia state machine. This is somewhat similar to `app.App`).

### Phase 4: Unification of Public APIs

Lastly, legacy APIs like the RPC layer will be deprecated in favour of a unified public API. Every node should offer the same collection of endpoints and be capable of verifying all retrieved data.

## Considerations

This is a project of considerable duration (roughly 18 months) and thus will coincide with other feature developments. Careful coordination can be extremely helpful in reducing the engineering resources required to complete the work and avoid unnecessary complexity.

- Phase 1 has no moving pieces it has to compete with and is relatively low risk.
- Phase 2 needs to be aware of the introduction of pruning and partial nodes. It also needs to be aware of potential improvements to the header verification algorithm. Phase 2 will also require a lazy migration of data types. Both legacy and new syncing and fraud protocols will need to be run side by side.
- Phase 3 will likely coincide with bandwidth and latency improvements to both the consensus and mempool protocols such as: [Compact Blocks](https://github.com/celestiaorg/celestia-core/issues/883), Tx Client, Intermediate Blocks, Transaction Sharding (i.e. Narwhal), [BlobTx Pipe](https://github.com/celestiaorg/celestia-app/issues/2297), and Consensus Pipelining. Statesync will likely remain unchanged and be the easiest to support. Giving rolling upgrades, there will be one version that will support both Tendermint P2P and LibP2P for all components.

### Risk Mitigation

Replacing the networking layer is a significant undertaking and vulnerable to a number of risks. Each component will need to be benchmarked to ensure no performance degradation and be sufficiently stress tested. Having both versions present, while adding complexity, has the advantage of enabling the option to fallback in case of unexpected failure.
