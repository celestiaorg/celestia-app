# ADR 14: Versioned Namespaces

## Status

Implemented in <https://github.com/celestiaorg/celestia-app/pull/1557>

## Changelog

- 2023/2/14: Initial draft
- 2023/5/30: Update status
- 2023/10/10: Remove reference to deleted git branch

## Context

A brief reminder of the schema for the first blob share in a sequence:

![first-sparse-share](./assets/adr014/first-sparse-share.svg)

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

1. Changes to the non-interactive default rules that don't break backwards compatibility with existing namespaces [celestia-app#1282](https://github.com/celestiaorg/celestia-app/issues/1282), [celestia-app#1161](https://github.com/celestiaorg/celestia-app/pull/1161)
    - After mainnet launch, if we want to change the non-interactive default rules but retain the previous non-interactive default rules for backwards compatibility, it isn't possible to differentiate the namespaces that want to use the old rules vs the new rules.
1. Changes to the format of a padding share.
1. Changes to the format of PFB transaction serialization.

## Proposal

An approach that addresses these issues is to prefix the namespace ID with version metadata.

### Option A: `Namespace Version | Namespace ID`

| Field | Number of bytes | Description |
| --- | --- | --- |
| Namespace Version | 1 | the version of the namespace ID |
| Namespace ID | 8 if Namespace Version=0, 32 if Namespace Version=1 | namespace ID of the share |

For example, consider the scenario where at mainnet launch blobs are laid out according to the existing non-interactive default rules. In this scenario, blobs always start at an index aligned with the `BlobMinSquareSize`. The only supported namespace ID is `0`. At some point in the future, if we introduce new non-interactive default rules (e.g. [celestia-app#1161](https://github.com/celestiaorg/celestia-app/pull/1161)), we may also expand the range of available namespaces to include namespaces that start with a leading `0` or `1` byte. Users may opt in to using the new non-interactive default rules by submitting PFB transactions with a namespace ID version of `1`.

- When the namespace starts with `0`, all blobs in the namespace conform to the previous set of non-interactive default rules.
- When a namespace starts with `1`, all blobs in the namespace conform to the new set of non-interactive default rules.

```go
optionA := []byte{
  0,                      // namespace version
  1, 2, 3, 4, 5, 6, 7, 8, // namespace ID
  1,          // info byte (sequence start indicator = true)
  0, 0, 0, 3, // sequence length
  1, 2, 3, // blob data
  // 0 padding until share is full
 }
```

Pros

- This proposal preserves backwards compatibility with roll-ups that want to continue using the previous set of non-interactive default rules.

Cons

- Old nodes won’t be able to parse the namespaces with a leading `1` byte so this is a breaking change that forces a hard-fork at an upgrade height.

### Option B: `Namespace Version | Namespace Length | Namespace ID`

Another approach is to prefix a namespace ID with the version and length of the namespace ID. This schema has the benefit that parsers can support the use case of changing from a namespace length of 8 bytes to 32 bytes without a version bump (and corresponding implementation change).

| Field | Number of bytes | Description |
| --- | --- | --- |
| Namespace Version | 1 | the version of the namespace ID |
| Namespace Length | 1 | the number of bytes that the namespace occupies encoded as a big-endian uint8 |
| Namespace ID | * (based on the previous field) | namespace ID of the share |

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

- Option B is implementable at a later time if we adopt Option A.
- It isn't immediately clear how NMT can support variable length namespaces.

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

- MinReservedNamespace: []byte{0, 0, 0, 0, 0, 0, 0, 1}
- MaxReservedNamespace: []byte{0, 0, 0, 0, 0, 0, 0, 255}
- MinBlobNamespace: []byte{0, 0, 0, 0, 0, 0, 1, 0}
- MaxBlobNamespace: []byte{0, 255, 255, 255, 255, 255, 255, 255}

When a user creates a PFB, concatenate the namespace version with the namespace ID to derive the namespace that is pushed to the NMT. Option C is similar to Option A, if we constrain the available share versions at mainnet to `0`.

## Open Questions

1. Is the scenario where we modify non-interactive default rules for a subset of namespaces contrived? When would we want to support a data square with two different non-interactive default rules? IMO roll-ups shouldn’t make assumptions about the padding in-between blobs so we shouldn’t need to support backwards compatibility with respect to padding changes.
2. When do we expect to increment the existing share version?
    1. Option 1: when there are changes to the universal share prefix
    2. Option 2: when there are changes to any part of the remaining data in a share
3. When do we expect to increment the namespace version?
    1. During a backwards incompatible non-interactive default rule change
    2. If we change the format of a padding share (e.g. a namespace padding share) instead of `0` bytes, pad with something else like. We may need to preserve backwards compatibility for padding shares that use old namespaces. Note this scenario likely implies a namespace version and share version increase.
    3. Change the format of PFB tx serialization. This scenario likely implies duplicating the PFB txs in a data square, one with the old namespace version and one with the new namespace version.
4. Inspired by [type-length-value](https://en.wikipedia.org/wiki/Type%E2%80%93length%E2%80%93value), should we consider prefixing optional fields (sequence length and reserved bytes) with a type and a length? This would enable us to modify those fields without introducing new share versions.
5. What changes need to be made to support variable length namespace IDs? In other words, roll-ups may use a namespace ID of 8 - 32 bytes. Are variable length namespace IDs something we are considering supporting?
6. What namespace ID version should we use for tail padding shares and parity shares?
    1. Option 6.1: `0`
      - Pros: Enables us to revise and modify the list of reserved namespaces IDs every time a new namespace ID version is introduced.
      - Cons: Leads to multiple namespace version + namespace IDs that have the same semantic meaning. In other words, the following two shares would likely both represent the same thing (a tail padding share):

          ```go
          namespaceVersion := 0
          namespaceID := namespace.ID{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFE}

          namespaceVersion := 1
          namespaceID := namespace.ID{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFE}
          ```

        but they differ in the location where they would be placed in the NMT.
    1. Option 6.2: `255`
      - Pros: The format of a tail padding share or parity share doesn't need to change when a future namespace version bump occurs.
7. How can we increase the namespace ID post mainnet?
      - Increment the namespace version. Construct two data squares and two NMTs. Data square 1 uses NMT 1 with namespace version 0 (namespace ID size 8 bytes). Data square 2 uses NMT 2 with namespace version 1 (namespace ID size of 32 bytes). Would celestia-nodes sample two separate data squares or is there a clever way to combine both data squares?

## Decision

Option A with a prerequisite of [celestia-app#1308](https://github.com/celestiaorg/celestia-app/issues/1308)

## References

- [celestia-app#1282](https://github.com/celestiaorg/celestia-app/issues/1282)
- IPFS CIDs
  - [https://docs.ipfs.tech/concepts/content-addressing/#what-is-a-cid](https://docs.ipfs.tech/concepts/content-addressing/#what-is-a-cid)
  - [https://proto.school/anatomy-of-a-cid](https://proto.school/anatomy-of-a-cid)
  - IPFS v0 CIDs don’t contain a version prefix. IPFS v1 CIDs do contain a version prefix.
  - [https://cid.ipfs.tech/#QmcRD4wkPPi6dig81r5sLj9Zm1gDCL4zgpEj9CfuRrGbzF](https://cid.ipfs.tech/#QmcRD4wkPPi6dig81r5sLj9Zm1gDCL4zgpEj9CfuRrGbzF) is a helpful tool to analyze the components of a CID.
