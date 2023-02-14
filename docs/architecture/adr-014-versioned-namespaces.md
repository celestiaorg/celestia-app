# ADR 14: Versioned Namespaces

## Changelog

- 2023/02/13: Initial draft

## Context

The specs-staging branch contains the current schema for a [sparse share](https://github.com/celestiaorg/celestia-app/blob/0dc7d17636efa535efca42af58229ee0c3c21261/specs/src/specs/data_structures.md#sparse-share). A brief reminder of the schema for the first blob share in a sequence:

![first-sparse-share.svg](./assets/first-sparse-share.svg)

Field | Number of bytes | Description
--- | --- | ---
namespace id | 8 | namespace ID of the share
info byte | 1 | the first 7 bits describe the share version. The last 1 bit determines if this is the first share in a sequence
sequence length | 4 | the number of bytes in the sequence encoded as a big-endian uint32
msg1 | 499 | blob1's raw data

The current schema poses a challenge for the following scenarios:

1. Consider increasing the namespace id size [celestia-app#1308](https://github.com/celestiaorg/celestia-app/issues/1308)
    - Wince there is no prefix to the namespace ID, it isn't possible to distinguish between a share with an 8 byte namespace ID and a share with a 16 byte namespace ID.
1. Determinstic namespace ID based on blob content [celestia-app#1377](https://github.com/celestiaorg/celestia-app/issues/1377)
    - Assuming the determinstic namespace ID has a length of 32 bytes, it isn't possible to easily increase the length of the namespace ID from 8 to 32 bytes.
1. Changes to the non-interactive default rules that don't break backwards compatability with existing namespaces [celestia-app#1282](https://github.com/celestiaorg/celestia-app/issues/1282), [celestia-app#1161](https://github.com/celestiaorg/celestia-app/pull/1161)
    - After mainnet launch, if we want to change the non-interactive default rules but retain the previous non-interactive default rules for backwards compatability, it isn't possible to differentiate the namespaces that want to use the new rules vs the namespaces that want to use the old rules. Question:

## Proposal

An approach that addresses these issues is to prefix the namespace ID with a version.

## Options

### Option A: `Namespace Version | Namespace ID`

For example, consider the scenario where at mainnet launch namespace IDs occupy 8 bytes. Constrain the namespaces available for use to namespaces that have a leading `0` byte. In other words:

- MinReservedNamespace: []byte{0, 0, 0, 0, 0, 0, 0, 1}
- MaxReservedNamespace: []byte{0, 0, 0, 0, 0, 0, 0, 255}
- MinBlobNamespace: []byte{0, 0, 0, 0, 0, 0, 1, 0}
- MaxBlobNamespace: []byte{0, 255, 255, 255, 255, 255, 255, 255}

At some point in the future, assume we want to increase the length of namespace IDs from 8 bytes to 32 bytes. We can expand the range of available namespaces to include namespaces that start with a leading `0` or `1` byte.

- When the namespace starts with `0`, the namespace occupies 8 bytes.
- When a namespace starts with `1`, the namespace occupies 32 bytes.

Old nodes won’t be able to parse the namespaces with a leading `1` byte so this is a breaking change that forces a hard-fork at an upgrade height. However, it preserves backwards compatibility with roll-ups that want to continue reading/writing to their 8 byte namespace ID.

> **Note**
> Additional changes are necessary to support shares with different namespace ID lengths in the same data square. In particular, it may be necessary to restrict a row in the NMT to contain only namespaces of the same length.

Field | Number of bytes | Description
--- | --- | ---
Namespace Version | 1 | the version of the namespace ID
Namespace ID | 8 if Namespace Version=0, 32 if Namespace Version=1 | namespace ID of the share

### Option B: `Type | Length | Namespace ID`

Inspired by [type-length-value](https://en.wikipedia.org/wiki/Type%E2%80%93length%E2%80%93value), another approach is to prefix a namespace ID with the version and length of the namespace ID. This schema has the benefit that parsers can support the use case in the previous example (changing from a namespace lengh of 8 bytes to 32 bytes) without an implementation change.

Field | Number of bytes | Description
--- | --- | ---
Type | 1 | the type of field that follows
Length | 4 | the number of bytes that the type occupies encoded as a big-endian uint32
Namespace ID | * (based on the previous field) | namespace ID of the share

## Decision

> This section records the decision that was made.
> It is best to record as much info as possible from the discussion that happened. This aids in not having to go back to the Pull Request to get the needed information.

## Detailed Design

> This section does not need to be filled in at the start of the ADR but must be completed prior to the merging of the implementation.
>
> Here are some common questions that get answered as part of the detailed design:
>
> - What are the user requirements?
>
> - What systems will be affected?
>
> - What new data structures are needed, and what data structures will be changed?
>
> - What new APIs will be needed, and what APIs will be changed?
>
> - What are the efficiency considerations (time/space)?
>
> - What are the expected access patterns (load/throughput)?
>
> - Are there any logging, monitoring, or observability needs?
>
> - Are there any security considerations?
>
> - Are there any privacy considerations?
>
> - How will the changes be tested?
>
> - If the change is large, how will the changes be broken up for ease of review?
>
> - Will these changes require a breaking (major) release?
>
> - Does this change require coordination with the SDK or others?

## Status

Proposed

## Consequences

> This section describes the consequences, after applying the decision. All consequences should be summarized here, not just the "positive" ones.

### Positive

### Negative

### Neutral

## References

- <https://github.com/celestiaorg/celestia-app/issues/1282>
