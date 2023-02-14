# ADR 14: Versioned Namespaces

## Changelog

- 2023/2/14: initial draft

## Context

The specs-staging branch contains the current schema for a [sparse share](https://github.com/celestiaorg/celestia-app/blob/0dc7d17636efa535efca42af58229ee0c3c21261/specs/src/specs/data_structures.md#sparse-share). A brief reminder of the schema for the first blob share in a sequence:

![first-sparse-share](./assets/first-sparse-share.svg)

| Field | Number of bytes | Description |
| --- | --- | --- |
| namespace id | 8 | namespace ID of the share |
| info byte | 1 | the first 7 bits determine the share version. The last 1 bit determines if this is the first share in a sequence |
| sequence length | 4 | the number of bytes in the sequence encoded as a big-endian uint32 |
| msg1 | 499 | blob1's raw data |

```go
currentBlobShare := []byte{
  1, 2, 3, 4, 5, 6, 7, 8, // namespace ID
  1,          // info byte (sequence start indicator = true)
  0, 0, 0, 3, // sequence length
  1, 2, 3, // blob data
  // 0 padding until share is full
 }
```

The current schema poses a challenge for the following scenarios:

1. Consider increasing the namespace id size [celestia-app#1308](https://github.com/celestiaorg/celestia-app/issues/1308)
    - Since there is no prefix to the namespace ID, it isn't possible to distinguish between a share with an 8 byte namespace ID and a larger namespace ID (say 16 bytes).
2. Deterministic namespace ID based on blob content [celestia-app#1377](https://github.com/celestiaorg/celestia-app/issues/1377)
    - Assuming we use SHA-256 and the deterministic namespace ID has a length of 32 bytes, it isn't possible to distinguish between an 8 byte namespace ID and a 32 byte namespace ID.
3. Changes to the non-interactive default rules that don't break backwards compatibility with existing namespaces [celestia-app#1282](https://github.com/celestiaorg/celestia-app/issues/1282), [celestia-app#1161](https://github.com/celestiaorg/celestia-app/pull/1161)
    - After mainnet launch, if we want to change the non-interactive default rules but retain the previous non-interactive default rules for backwards compatibility, it isn't possible to differentiate the namespaces that want to use the old rules vs the new rules.

## Proposal

An approach that addresses these issues is to prefix the namespace ID with version metadata.

### Option A: `Namespace Version | Namespace ID`

| Field | Number of bytes | Description |
| --- | --- | --- |
| Namespace Version | 1 | the version of the namespace ID |
| Namespace ID | 8 if Namespace Version=0, 32 if Namespace Version=1 | namespace ID of the share |

For example, consider the scenario where at mainnet launch namespace IDs have a length of 8 bytes. In this scenario, the only supported namespace ID is `0`. At some point in the future,  we want to increase the length of namespace IDs from 8 bytes to 32 bytes. We can expand the range of available namespaces to include namespaces that start with a leading `0` or `1` byte.

- When the namespace starts with `0`, the namespace occupies 8 bytes.
- When a namespace starts with `1`, the namespace occupies 32 bytes.

```go
optionA0 := []byte{
  0,                      // namespace version
  1, 2, 3, 4, 5, 6, 7, 8, // namespace ID
  1,          // info byte (sequence start indicator = true)
  0, 0, 0, 3, // sequence length
  1, 2, 3, // blob data
  // 0 padding until share is full
 }

 optionA1 := []byte{
  1,                                                                                                                     // namespace version
  1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, // namespace ID
  1,          // info byte (sequence start indicator = true)
  0, 0, 0, 3, // sequence length
  1, 2, 3, // blob data
  // 0 padding until share is full
 }
```

Pros

- This proposal preserves backwards compatibility with roll-ups that want to continue reading/writing to their 8 byte namespace ID.
- Unlike Option C, roll-ups don’t need to migrate to a new namespace ID in order to adopt new share version formats.

Cons

- Old nodes won’t be able to parse the namespaces with a leading `1` byte so this is a breaking change that forces a hard-fork at an upgrade height.

Notes

- Additional changes are necessary to support shares with different namespace ID lengths in the same data square. In particular, it may be necessary to restrict a row in the NMT to contain only namespaces of the same length.

### Option B: `Namespace Version | Namespace Length | Namespace ID`

Another approach is to prefix a namespace ID with the version and length of the namespace ID. This schema has the benefit that parsers can support the use case in the previous scenario (changing from a namespace length of 8 bytes to 32 bytes) without a version bump (and corresponding implementation change).

| Field | Number of bytes | Description |
| --- | --- | --- |
| Namespace Version | 1 | the version of the namespace ID |
| Namespace Length | 1 | the number of bytes that the namespace occupies encoded as a big-endian uint8 |
| Namespace ID | * (based on the previous field) | namespace ID of the share |

In this scenario namespace version may be incremented to signal a breaking change (e.g. a change to the non-interactive default rules where we preserve old rules for version `0` namespaces)

```go
optionB := []byte{
  0,                                                     // namespace version
  16,                                                    // namespace length
  1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, // namespace ID
  1,          // info byte (sequence start indicator = true)
  0, 0, 0, 3, // sequence length
  1, 2, 3, // blob data
  // 0 padding until share is full
 }
```

Pros

- Supports variable length namespaces without a namespace version bump

Cons

- 2 bytes of metadata per share

Notes

- Same as Option A: “Additional changes are necessary…”
- Option B is implementable at a later time if we adopt Option A.

### Option C: `Share Version | Namespace ID`

Inspired by [CIDs](https://github.com/multiformats/cid#design-considerations), consider encoding a share version **at the start of the share** to ensure the share format itself can evolve. In other words, the share version moves from after the namespace to before the namespace. The share version can then be used to version changes to the namespace and universal share prefix.

```go
optionC := []byte{
  0,                      // share version
  1, 2, 3, 4, 5, 6, 7, 8, // namespace ID
  1,          // info byte (only contains sequence start indicator = true)
  0, 0, 0, 3, // sequence length
  1, 2, 3, // blob data
  // 0 padding until share is full
 }
```

Pros

- Fewer fields need to be specified. In options A and B, an end-user needs to specify `shareVersion=0` and `namespaceVersion=0`. With this option, they only need to specify  `shareVersion=0`.
- The existing info byte can be renamed to the sequence start indicator for clarity. Alternatively it can remain an info byte with 7 available bits for future metadata.

Cons

- Since the namespace and the universal share prefix are versioned via the same field, a breaking change to either mandates a share version bump.
- Roll-ups can’t adopt a new share version without also moving the namespace that they read/write from.

Notes

- With this option, the share version is prefixed to the namespace prior to pushing to the NMT. When a user queries a namespace, they must also specify a share version.

## Implementation Details

Regardless of the option chosen, end-users must explicitly specify the namespace ID version (and/or) share version they intend on using in their PFB transaction.

Since the NMT needs to be aware of the version byte, Option A is equivalent to increasing the namespace ID size to `9` bytes and then constraining the namespaces available for use to namespaces that have a leading `0` byte. In other words:

- MinReservedNamespace: []byte{0, 0, 0, 0, 0, 0, 0, 0, 1}
- MaxReservedNamespace: []byte{0, 0, 0, 0, 0, 0, 0, 0, 255}
- MinBlobNamespace: []byte{0, 0, 0, 0, 0, 0, 0, 1, 0}
- MaxBlobNamespace: []byte{0, 255, 255, 255, 255, 255, 255, 255, 255}

When a user creates a PFB, concatenate the namespace version with the namespace ID to derive the namespace that is pushed to the NMT. Option C is similar to Option A, if we constrain the available share versions at mainnet to `0`.

## Open Questions

1. Is the scenario where we modify non-interactive default rules for a subset of namespaces contrived? When would we want to support a data square with two different non-interactive default rules? IMO roll-ups shouldn’t make assumptions about the padding in-between blobs so we shouldn’t need to support backwards compatibility with respect to padding changes.
2. When do we expect to increment the existing share version?
    1. Option 1: when there are changes to the universal share prefix
    2. Option 2: when there are changes to any part of the remaining data in a share
3. When do we expect to increment the namespace version?
    1. During a backwards incompatable non-interactive default rule change
    2. If we change the format of a padding share (e.g. a namespace padding share) instead of `0` bytes, pad with something else like. We may need to preserve backwards compatibility for padding shares that use old namespaces. Note this scenario likely implies a namespace version and share version increase.
    3. Change the format of PFB tx serialization. This scenario likely implies duplicating the PFB txs in a data square, one with the old namespace version and one with the new namespace version.
4. Inspired by [type-length-value](https://en.wikipedia.org/wiki/Type%E2%80%93length%E2%80%93value), should we consider prefixing optional fields (sequence length and reserved bytes) with a type and a length? This would enable us to modify those fields without introducing new share versions.
5. [Requires investigation] what changes need to be made to NMT in order to support namespaces of a different length (e.g. 16 bytes)?
6. [Requires investigation] what changes need to be made to support variable length namespace IDs? In other words, roll-ups may use a namespace ID of 8 - 32 bytes. Are variable length namespace IDs something we are considering supporting?

## Decision

Option A with the assumption that we're doing <https://github.com/celestiaorg/celestia-app/issues/1308>

## References

- [https://github.com/celestiaorg/celestia-app/issues/1282](https://github.com/celestiaorg/celestia-app/issues/1282)
- IPFS CIDs
  - [https://docs.ipfs.tech/concepts/content-addressing/#what-is-a-cid](https://docs.ipfs.tech/concepts/content-addressing/#what-is-a-cid)
  - [https://proto.school/anatomy-of-a-cid](https://proto.school/anatomy-of-a-cid)
  - IPFS v0 CIDs don’t contain a version prefix. IPFS v1 CIDs do contain a version prefix.
  - [https://cid.ipfs.tech/#QmcRD4wkPPi6dig81r5sLj9Zm1gDCL4zgpEj9CfuRrGbzF](https://cid.ipfs.tech/#QmcRD4wkPPi6dig81r5sLj9Zm1gDCL4zgpEj9CfuRrGbzF) is a helpful tool to analyze the components of a CID.
