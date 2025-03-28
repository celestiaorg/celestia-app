# ADR 15: Namespace ID Size

## Status

Implemented in <https://github.com/celestiaorg/celestia-app/pull/1419> and revised in <https://github.com/celestiaorg/celestia-app/pull/1771>

## Changelog

- 2023/2/17: Initial draft
- 2023/2/22: Discussion notes
- 2023/2/23: Reorder content
- 2023/2/28: NMT proof size
- 2023/3/1: Blob inclusion proof size
- 2023/3/2: Accepted
- 2023/5/10: Add section on SHA256 performance
- 2023/5/11: Revise decision 33 bytes => 29 bytes
- 2023/5/30: Update status

## Context

Namespace ID is currently an 8 byte slice. 8 bytes provides a maximum of 2^64 possible namespace IDs. In practice some namespace IDs are reserved for protocol use so the number of namespace IDs available to users is 2^56 - 2. Modifying the size of a namespace ID post-launch is a breaking change and has implications for the NMT, share encoding, etc. so we'd like to carefully consider the size of the namespace ID pre-launch.

## Decision

Increase the total namespace size to 29 bytes with the following format: version (1 byte) + ID (28 bytes) = namespace (29 bytes)

- At launch the only supported version is `0`.
- For version `0`, the ID is 28 bytes where:
  - The most-significant 18 bytes are `0`
  - The least-significant 10 bytes are unreserved namespace bytes. In other words, a user can specify 10 bytes of data to be used as the namespace ID.

The motivation for reserving the first 18 bytes of the ID as `0` is to enable future bandwidth optimizations. In particular, the ID may be run length encoded to reduce the size of NMT proofs.

The ID size is 28 bytes so that a future version can be introduced to expand the ID address space without a backwards incompatible change from the perspective of NMT.

The version will be prefixed to the ID prior to pushing data to the NMT so NMT should be constructed with a namespace size of 29 bytes.

Users will specify a version (1 byte) and a ID (28 bytes) in their PFB. Additionally we should strive to make it clear to users that namespaces with different versions will be interpreted as distinct namespaces. For example, `namespaceA` and `namespaceB` will be interpreted as distinct namespaces:

  ```go
	id := bytes.Repeat([]byte{0}, 28)
	namespaceA := append([]byte{0}, id...)
	namespaceB := append([]byte{1}, id...)
  ```

## Desirable criteria

1. A user should be able to randomly generate a namespace that hasn't been used before[^1]
2. There should exist a large enough namespace ID space for all rollups that may exist in the foreseeable future (e.g. 100 years)

### Criteria 1

The namespace ID must provide at least 72 bits of randomness to satisfy criteria 1. Since an 8 byte namespace ID can only provide 64 bits of randomness, it fail to meet this criteria.

| Namespace ID size (bytes) | Criteria 1 |
|---------------------------|------------|
| 8                         | ❌          |
| 16                        | ✅          |
| 20                        | ✅          |
| 32                        | ✅          |

Another way to analyze this criteria is to determine the probability of duplicates if there exist N randomly generated namespaces. Columns in the table below represent the approximate probability that a collision would occur if N (e.g. 1 billion) random namespaces are generated.[^3]

| Namespace ID size (bytes) | 1 billion (10^9) | 1 trillion (10^12) | 1 quadrillion (10^15) | 1 quintillion (10^18) |
|---------------------------|------------------|--------------------|-----------------------|-----------------------|
| 8                         | ~0.02674         | 1                  | 1                     | 1                     |
| 16                        | 0                | ~1.4432e-15        | ~1.4693e-9            | ~0.00147              |
| 20                        | 0                | 0                  | 0                     | ~3.4205e-13           |
| 32                        | 0                | 0                  | 0                     | 0                     |

> As a rule of thumb, a hash function with range of size N can hash on the order of sqrt(N) values before running into collisions.[^4]

| Namespace ID size (bytes) | Hash function range | Can hash this many items before running into collision |
|---------------------------|---------------------|--------------------------------------------------------|
| 8                         | 2^64                | 2^32 = ~4 billion items                                |
| 16                        | 2^128               | 2^64 = ~1 quintillion items                            |
| 20                        | 2^160               | 2^80 = ~1 septillion items                             |
| 32                        | 2^256               | 2^128 = ~3.4 quintillion items                         |

### Criteria 2

We must make some assumptions for the number of rollups that will exist. Ethereum has 223 million unique addresses with a yearly growth rate of 18%.[^5] If the growth rate remains constant for the next 100 years, Ethereum would have ~4 quadrillion unique addresses[^6] which is inconceivably small relative to the 20 byte address space.[^7] ~4 quadrillion unique addresses is 0.0002%[^8] of the 8 byte namespace id space so one can assume that any namespace ID size >= 8 bytes will be large enough for all rollups that may exist in the next 100 years.

| Namespace ID size (bytes) | Criteria 2 |
|---------------------------|------------|
| 8                         | ✅          |
| 16                        | ✅          |
| 20                        | ✅          |
| 32                        | ✅          |

## Notes

- [SHA256](https://en.wikipedia.org/wiki/SHA-2) has a digest size of 32 bytes so using a namespace ID size of 32 bytes would enable users to generate stable namespace IDs (e.g. `sha256('sov-labs')`) or unique namespace IDs (e.g. `sha256(blob)`) assuming the blob is unique.
- [IPv6](https://en.wikipedia.org/wiki/IPv6) has an address space of 16 bytes and "the address space is deemed large enough for the foreseeable future"[^9].
- [UUIDs](https://en.wikipedia.org/wiki/Universally_unique_identifier) have slightly less than 16 bytes of randomness  and are considered "unique enough for practical purposes"[^10].
- The size of the Ethereum[^11] and Bitcoin[^12] address space is 20 bytes (2^160).
- The size of Fuel's address space is 32 bytes[^13].

## Tradeoffs

There are some tradeoffs to consider when choosing a namespace ID size.

### NMT node size

The namespace is prefixed to each NMT data leaf. Two namespaces are prefixed to each NMT non-leaf hash. Therefore, the nodes of an NMT will be larger based on the namespace size. Assuming shares are 512 bytes:

| Namespace size (bytes) | NMT data leaf size (bytes) | NMT inner node size (bytes) |
|------------------------|----------------------------|-----------------------------|
| 8                      | 8 + 512 = 520              | 2*8 + 32 = 48               |
| 16                     | 16 + 512 = 528             | 2*16 + 32 = 64              |
| 20                     | 20 + 512 = 532             | 2*20 + 32 = 72              |
| 32                     | 32 + 512 = 544             | 2*32 + 32 = 96              |

### NMT proof size

Increasing the size of NMT nodes will increase the size of the NMT proof. Assuming shares are 512 bytes, square size is 128, the NMT for a row will contain 2 * 128 leaves. If the NMT proof is for a single leaf:

| Namespace size (bytes) | Unencoded NMT proof size (bytes) | Protobuf encoded NMT proof size (bytes) | Protobuf encoded NMT proof with [gzip](https://pkg.go.dev/compress/gzip) (bytes) |
|------------------------|----------------------------------|-----------------------------------------|----------------------------------------------------------------------------------|
| 8                      | 336                              | 354                                     | 382                                                                              |
| 16                     | 448                              | 466                                     | 408                                                                              |
| 20                     | 504                              | 522                                     | 466                                                                              |
| 32                     | 672                              | 690                                     | 630                                                                              |

Note: if the NMT proof is an absence proof, an additional leaf node is included in the proof.

### Blob inclusion proof size

Blob inclusion proofs haven't yet been implemented so this proposal can't precisely determine the impact on blob inclusion proofs. A naive implementation of blob inclusion proofs may return NMT proofs for all shares that a blob occupies, in other words one NMT proof per row that a blob spans. Assuming shares are 512 bytes, square size is 128, and a blob is less than 128 shares, a blob would occupy a maximum of 2 rows. Therefore, the namespace size's impact on blob inclusion proofs would be approximately 2 * the impact on NMT proofs. A [blob size independent inclusion proof](https://github.com/celestiaorg/celestia-app/blob/6d27b78aa64a749a808e84ea682352b8b551fbd7/docs/architecture/adr-011-optimistic-blob-size-independent-inclusion-proofs-and-pfb-fraud-proofs.md?plain=1#L19) is likely smaller than this naive implementation because it depends on the number of shares that a PFB transaction spans (likely significantly fewer than 2 rows).

### Share size

Another tradeoff to consider is the size of the namespace in the share. Since a share is a fixed 512 bytes, a share's capacity for blob data decreases as the namespace increases.

| Namespace size (bytes) | Namespace size (bytes) / 512 (bytes) |
|------------------------|--------------------------------------|
| 8                      | 1.5%                                 |
| 16                     | 3.1%                                 |
| 20                     | 3.9%                                 |
| 32                     | 6.2%                                 |

### Maximum blob size

If the namespace size is increased, the maximum possible blob will decrease. Given the maximum possible blob is bounded by the number of bytes available for blob space in a data square, if a 32 byte namespace size is adopted, the maximum blob size will decrease by an upper bound of `appconsts.MaxSquareSize * appconsts.MaxSquareSize * (32-8)`. Note this is an upper bound because not all shares in the data square can be used for blob data (i.e. at least one share must contain the associated PayForBlob transaction).

### SHA256 performance

If the namespace size is increased, whenever a SHA256 invocation takes place (e.g. NMT's [HashNode](https://github.com/celestiaorg/nmt/blob/fd00c52175c48bad64d03444689162fb9c6bee41/hasher.go#L265)), more data needs to be SHA256'ed. For example:

| Namespace size (bytes) | [HashNode](https://github.com/celestiaorg/nmt/blob/fd00c52175c48bad64d03444689162fb9c6bee41/hasher.go#L302)'s SHA256 max data (bytes) |
|------------------------|---------------------------------------------------------------------------------------------------------------------------------------|
| 8                      | 97                                                                                                                                    |
| 16                     | 129                                                                                                                                   |
| 32                     | 193                                                                                                                                   |
| 33                     | 197                                                                                                                                   |

The data size is calculated as follows:
`SHA256(domain_sep || left_min || left_max || left_data_hash || right_min || right_max || right_data_hash)` = 1 + namespaceSize + namespaceSize + 32 + namespaceSize + namespaceSize + 32 which simplifies to (4 \* namespaceSIze) + (2 \* 32) + 1.

Hashing has a high performance impact in ZK contexts. Since a larger namespace size results in a larger data size for the SHA256 operation, increasing the namespace size has a negative performance impact on the cost to perform the SHA256 operation in a ZK context.

Based on this StackOverflow [post](https://crypto.stackexchange.com/questions/54852/what-happens-if-a-sha-256-input-is-too-long-longer-than-512-bits), when the data provided to SHA256 exceeds 56 bytes, the data must be chunked into [64 byte blocks](https://cs.opensource.google/go/go/+/refs/tags/go1.20.4:src/crypto/sha256/sha256.go;l=29;drc=995c0f310c087c9cbc49112ecc48459a96310451) with a trailing 56 byte block + 8 bytes to store the original data length as a `uint64`.

| Namespace size (bytes) | [HashNode](https://github.com/celestiaorg/nmt/blob/fd00c52175c48bad64d03444689162fb9c6bee41/hasher.go#L302)'s SHA256 max data (bytes) | SHA256 compression invocations |
|------------------------|---------------------------------------------------------------------------------------------------------------------------------------|--------------------------------|
| N/A                    | 56                                                                                                                                    | 1                              |
| 0 to 13                | 64 + 56 = 120                                                                                                                         | 2                              |
| 14 to 29               | 64 + 64 + 56 = 184                                                                                                                    | 3                              |
| 30 to 45               | 64 + 64 + 64 + 56 = 248                                                                                                               | 4                              |

Note: to verify the number of SHA256 compression invocations, we analyzed the number of loop executions inside the Golang SHA256 implementation [here](https://github.com/golang/go/blob/96add980ad27faed627f26ef1ab09e8fe45d6bd1/src/crypto/sha256/sha256block.go#L83) and it matches the expected number of invocations in the table above. See raw data [here](https://gist.github.com/rootulp/4cfc10c1c80a15cc57f0b35f330ac542).

## Open questions

1. What are the performance implications on celestia-node for a larger namespace ID size?
1. Is it possible to mitigate some tradeoffs when adopting a large namespace ID size?
    1. It may be possible to decrease the bandwidth requirements for NMT proofs by using lossless compression (proposed by @evan-forbes) and explored above.
    1. It may be possible to avoid writing the namespace ID to continuation blob shares (proposed by @nashqueue)
        1. Note this introduces complexity for erasure reconstruction. A share in row B may have its namespace in row A so to reconstruct a data square, we must refactor the process to two steps:
            1. Reconstruct all shares from the erasure coding
            1. Reconstruct the NMT

1. Is it possible to preserve backwards compatibility if we increase namespace ID size in the future?
    1. One challenge with backwards compatibility is that the NMT proof verification logic for old clients will not be able to verify the new larger namespace ID. Since the namespace ID is prefixed to each NMT data leaf and two namespace IDs are prefixed to each NMT inner node, an NMT constructed with two different size namespace IDs will result in different size nodes. An NMT proof contains the field [`nodes`](https://github.com/celestiaorg/nmt/blob/1bc0bb0099e01b30e37ddb56642734ae875917cd/proof.go#L20-L25) which would have different size nodes for different namespace ID sizes. An old client would not be able to split the namespace IDs from the hash digest unless the old client was written in a brittle way.
    1. Another challenge with backwards compatibility is how to determine the min/max namespace ID for a parent node with one child of namespace ID size 16 and one child of namespace ID size 32. The naive approach of padding the 16 byte namespace ID to 32 bytes with leading or trailing zeroes does not work because the hash of the unpadded namespace ID != the hash of the padded namespace ID.
1. If we start with a namespace ID size of 32 bytes, is it possible to mitigate the tradeoffs in subsequent namespace versions?
    - No for share size because all 32 bytes of the namespace ID would need to be present in the share in order to not break share commitments.
    - Potentially for NMT proof size via an in-protocol compression mechanism. From the NMT's perspective, all data pushed to the NMT would have namespace ID size 32 bytes. But we may introduce a new share version that enables clients to specify a namespace ID with fewer than 32 bytes (e.g. 8 bytes). One could view this optimization as a run-length encoding scheme where `namespaceVersion=1` is a run of 24 bytes of 0s followed by 8 bytes of significant namespace ID. In other words:
      - `namespaceVersion=0`: 32 bytes of significant namespace ID.
      - `namespaceVersion=1`: is interpreted as 24 bytes of leading 0s and 8 bytes of significant namespace ID.
      - The optimization would require changes to celestia-app's `nmt_wrapper.go` and nmt's `Hasher` to interpolate the 24 bytes of leading zeros when presented with `namespaceVersion=1`. This would enable clients to compress the 24 bytes of leading zeros in NMT proofs.
1. What changes need to be made to in order to support namespaces of a different length (e.g. 16 bytes)?
    - celestia-app
      - [x] Stop using the namespace ID defined by NMT [celestia-app#1385](https://github.com/celestiaorg/celestia-app/pull/1385)
      - [ ] Increase `appconsts.NamespaceSize` to 16 [celestia-app#1419](https://github.com/celestiaorg/celestia-app/pull/1419)
    - celestia-core
      - [ ] Modify `TxNamespaceID`
    - nmt
      - N/A
    - celestia-node
      - TBD

## Discussion notes

- Do we care about collisions created by an adversary?
  - If so, an adversary can look at previously used namespace IDs and perform a woods attack on an existing namespace ID so increasing the namespace ID size doesn't resolve this threat.
  - We should be careful in our documentation to not encourage users to assume that a randomly generated namespace ID is completely unique because:
    - They could have generated a namespace ID without sufficient entropy
    - An adversary can front-run a user's transaction and preemptively post to that namespace ID
- For the non-adversarial use-case, we want to avoid users having to check if a random namespace has already been used.
- Use case for larger namespace ID size: rollups may have multiple namespaces (e.g. Twitter) where a roll-up may give each user a namespace within a namespace range.
- Is it possible to make the namespace ID a parameter, so that the namespace ID is a parameter to proof verification for roll-ups?
  - Assumes that a block height may have a different namespace ID
- There are talks in the Ethereum community about a potential address range increase.
  - Address space extension is on the Ethereum roadmap under "The Purge" phase.[^14] There doesn't appear to be alignment on how to implement such an address space extension but the discussion is leaning towards increasing from 20 bytes to 32 bytes.[^15]
- 20 bytes gives us Ethereum address compatibility so Ethereum addresses could be mapped to a Celestia namespace ID.
- Other option: increase size to 32 bytes with an optimization that reserves the first N bytes. The first N bytes wouldn't be sent over the wire.
- Solution to woods attack
  - Rollups can't assume that all blobs in a namespace are honest
  - Rollups shouldn't scan a namespace directly. Instead they should gossip block headers and light clients should only request blobs of interest.
- Why no dynamic namespace ID length?
  - Disagreement on serialization
  - Implementation complexity of parsing a varint
- Desirable property: first 40 fixed bytes to be metadata
  - IPV6 packet header is fixed

## References

- <https://github.com/celestiaorg/celestia-app/issues/1308>

[^1]: This assumes a user uses sufficient entropy to generate the namespace ID and isn't front-run by an adversary prior to actually using the namespace.
[^3]: <https://kevingal.com/apps/collision.html>
[^4]: <https://www.johndcook.com/blog/2017/01/10/probability-of-secure-hash-collisions/>
<!-- markdown-link-check-disable -->
[^5]: <https://ycharts.com/indicators/ethereum_cumulative_unique_addresses>
<!-- markdown-link-check-enable -->
[^6]: <https://docs.google.com/spreadsheets/d/1vrRM4gAsmC142KrdUI1aCBS5IVFdJeU0q6gwwnM3Ekc/edit?usp=sharing>
[^7]: <https://www.wolframalpha.com/input?i=4.05871E%2B15+%2F+2%5E160>
[^8]: <https://www.wolframalpha.com/input?i=4.05871E%2B15+%2F+2%5E64>
