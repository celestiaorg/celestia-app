# RSEMA1D Codec Specification v1.0

## Table of Contents

1. [Overview](#1-overview)
2. [Mathematical Foundations](#2-mathematical-foundations)
   - 2.1 [Field Definitions](#21-field-definitions)
   - 2.2 [Data Layout](#22-data-layout)
3. [Codec Specification](#3-codec-specification)
   - 3.1 [Parameters](#31-parameters)
   - 3.2 [Data Extension](#32-data-extension)
   - 3.3 [Commitment Generation](#33-commitment-generation)
   - 3.4 [Proof Generation](#34-proof-generation)
   - 3.5 [Proof Verification](#35-proof-verification)
4. [Security Considerations](#4-security-considerations)
   - 4.1 [Soundness](#41-soundness)
   - 4.2 [Proximity Gap](#42-proximity-gap)
   - 4.3 [Commitment Binding](#43-commitment-binding)
5. [Implementation Requirements](#5-implementation-requirements)
   - 5.1 [Cryptographic Primitives](#51-cryptographic-primitives)
   - 5.2 [Arithmetic Operations](#52-arithmetic-operations)
   - 5.3 [Memory Requirements](#53-memory-requirements)
6. [Test Vectors](#6-test-vectors)
   - 6.1 [Test Vector 1: K=4, N=4, rowSize=64](#61-test-vector-1-k4-n4-rowsize64)
   - 6.2 [Test Vector 2: K=3, N=9, rowSize=256](#62-test-vector-2-k3-n9-rowsize256)
   - 6.3 [Verification Test Cases](#63-verification-test-cases)
7. [References](#7-references)

**Appendices:**

- [Appendix A: Leopard-Specific Operations](#appendix-a-leopard-specific-operations)

  - A.1 [Symbol Extraction from Leopard Share](#a1-symbol-extraction-from-leopard-share)
  - A.2 [Visual Representation of Leopard Format](#a2-visual-representation-of-leopard-format)
- [Appendix B: Serialization Formats](#appendix-b-serialization-formats)

  - B.1 [GF128 Serialization](#b1-gf128-serialization)
  - B.2 [GF128 Packing for Leopard Extension](#b2-gf128-packing-for-leopard-extension)
  - B.3 [Merkle Tree Construction](#b3-merkle-tree-construction)
  - B.4 [Proof Serialization](#b4-proof-serialization)
- [Appendix C: Bulk Data Read Paths](#appendix-c-bulk-data-read-paths)

  - C.1 [Single Row Reading](#c1-single-row-reading-random-sampling)
  - C.2 [Full Original Data Reading](#c2-full-original-data-reading-bulk-download)
  - C.3 [Left Subtree Proof Functions](#c3-left-subtree-proof-functions)

## 1. Overview

RSEMA1D (Reed-Solomon Evans-Mohnblatt-Angeris 1D) is a data availability codec that provides efficient commitment, proof generation, and verification for vertically-extended data matrices. The codec uses random linear combinations (RLCs) to ensure soundness of the encoding.

### Key Properties

- **Vertical-only extension**: Reed-Solomon encoding applied only along columns using Leopard codec
- **RLC-based verification**: Uses random linear combinations for soundness
- **Efficient verification**: O(K) operations for verifying extended rows, O(log K) for original rows
- **128-bit security**: Using GF(2^128) for random linear combinations against RLC forgery

### Notation Conventions

**Iterator Ranges**: All iterators in this specification use exclusive notation. Ranges like `0..K` imply that the target (`K`) is excluded and the initial value (`0`) is included. For example:

- `0..K` iterates over values 0, 1, 2, ..., K-1
- `K..K_padded` iterates over values K, K+1, ..., K_padded-1

## 2. Mathematical Foundations

### 2.1 Field Definitions

**GF(2^16)**: The base field for Reed-Solomon encoding

- Irreducible polynomial: x^16 + x^12 + x^3 + x + 1 (0x1002D)
- Used by Leopard RS codec for efficient encoding/decoding

**Leopard Codec**:

- Uses systematic Reed-Solomon encoding
- Each row is treated as a single Leopard shard
- Shards must be at least 64 bytes and must be a multiple of 64 bytes
- **Internal format**: Leopard processes data in 64-byte chunks with interleaved GF(2^16) format:

  - Each 64-byte chunk: bytes 0-31 contain low bytes, bytes 32-63 contain high bytes of 32 GF(2^16) symbols
  - Symbol_i = (byte[32+i] << 8) | byte[i] for i ∈ [0,32)
  - Rows > 64 bytes are treated as concatenated 64-byte chunks

**GF(2^128)**: The field for random linear combinations

- Represented as 8-dimensional vector space over GF(2^16)
- Type representation: array of 8 uint16 values (little-endian)
- Operations:

  - Addition: component-wise XOR
  - Scalar multiplication (GF16 × GF128): multiply each component by scalar

### 2.2 Data Layout

Data is arranged as a tall matrix:

```text
Original Data:     Extended Data:
┌───────────┐      ┌───────────┐
│  K rows   │      │  K rows   │ (original)
│           │      │           │
└───────────┘      ├───────────┤
                   │  N rows   │ (parity)
                   │           │
                   └───────────┘
```

Each row contains `rowSize` bytes, where:

- `rowSize` must be a multiple of 64 (Leopard constraint)
- When processing RLCs, each row is interpreted as `rowSize/2` GF(2^16) symbols (since each GF(2^16) symbol is 2 bytes)

## 3. Codec Specification

### 3.1 Parameters

```text
K:       Number of original rows (1 ≤ K ≤ 2^16)
N:       Number of parity rows (1 ≤ N ≤ 2^16, K+N ≤ 2^16)
rowSize: Size of each row in bytes (multiple of 64)
```

**Parameter Constraints:**

- K can be any positive integer up to 2^16
- N can be any positive integer such that K+N ≤ 2^16
- rowSize must be at least 64 and a multiple of 64 (Leopard constraint)

**Tree Padding Strategy:**
For Merkle tree construction, padding is applied to achieve power-of-2 sizes:

- Let K_padded = next power of 2 ≥ K
- Let total_padded = next power of 2 ≥ (K_padded + N)
- Padding consists of zero-filled rows of size rowSize
- Tree layout:

  - Positions [0, K): Original data rows
  - Positions [K, K_padded): Zero padding
  - Positions [K_padded, K_padded+N): Extended/parity rows
  - Positions [K_padded+N, total_padded): Zero padding

### 3.2 Data Extension

**Input**: K rows of `rowSize` bytes each

**Process**:

1. Treat each row as a Leopard shard (rowSize bytes)
2. Apply Leopard RS encoding:
   - Input: K shards (original rows)
   - Output: N parity shards (parity rows)
   - This is done in a single Leopard encoding call
3. Result: K+N total rows

**Output**: K+N rows of `rowSize` bytes each

### 3.3 Commitment Generation

**Helper Functions:**

```text
// BuildPaddedRowTree creates a padded tree for extended row data
BuildPaddedRowTree(rowExtended, K, N):
   zeroRow = new byte[RowSize]
   paddedRows = new array[totalPadded]
   # Fill paddedRows with: [original | padding | extended | padding]

   # 1. Original rows
   for i in 0 .. K:
      paddedRows[i] = extended[i]

   # 2. Padding after original rows, up to K_padded
   for i in K .. K_padded:
      paddedRows[i] = zeroRow

   # 3. Extended rows
   for i in K_padded .. K_padded + N:
      indexIntoExtended = K + (i - K_padded)
      paddedRows[i] = extended[indexIntoExtended]

   # 4. Final padding
   for i in K_padded + N .. totalPadded:
      paddedRows[i] = zeroRow

   return MerkleTree(paddedRows, WorkerCount)

// BuildPaddedRLCTree creates a padded tree from RLC data
BuildPaddedRLCTree(rlcOrig, K):
   K_padded     = nextPowerOfTwo(K)
   paddedRLCLeaves = new array[K_padded]
   zeroRLC         = new byte[16]

   # 1. Original RLC rows
   for i in 0 .. K:
      paddedRLCLeaves[i] = Serialize(rlcOrig[i])

   # 2. Padding up to K_padded
   for i in K .. K_padded:
      paddedRLCLeaves[i] = zeroRLC

return MerkleTree(paddedRLCLeaves, WorkerCount)


// MapIndexToTreePosition maps actual index to padded tree position
MapIndexToTreePosition(index, K):
    K_padded = nextPowerOfTwo(K)
    if index < K:
        return index
    else:
        return K_padded + (index - K)
```

**Input**: Extended data (K+N rows)

**Process**:

1. **Compute Row Root**

    ```text
    rowTree = BuildPaddedRowTree(rows, K, N)
    rowRoot = rowTree.root()
    ```

1. **Derive RLC Coefficients**

   ```text
   seed = SHA256(rowRoot)
   numSymbols = rowSize / 2  // Each GF16 symbol is 2 bytes
   for i in 0..numSymbols:
       coeffs[i] = HashToGF128(SHA256(seed || i))
   ```

   Where HashToGF128 converts a 32-byte hash to a GF128 element by:

   - Taking bytes 0-15 as 8 little-endian uint16 values
   - Taking bytes 16-31 as 8 little-endian uint16 values
   - XORing corresponding pairs to produce final 8 GF(2^16) values

1. **Compute RLC Results (Original Rows Only)**

   ```text
   for i in 0..K:
       rlc[i] = 0  // Initialize as zero in GF(2^128)
       for c in 0..numChunks:
           chunk = row[i][c*64..(c+1)*64]
           symbols = ExtractSymbols(chunk)  // See Appendix A.1
           for j in 0..32:
               symbolIndex = c*32 + j  // Overall symbol index in the row
               rlc[i] += symbols[j] * coeffs[symbolIndex]  // GF16 × GF128
   ```

1. **Compute RLC Root**

   ```text
   rlcOrigTree = BuildPaddedRLCTree(rlcOrig, K)
   rlcOrigRoot = rlcOrigTree.root()
   ```

1. **Final Commitment**

   ```text
   commitment = SHA256(rowRoot || rlcOrigRoot)
   ```

**Output**:

- `commitment`: 32-byte commitment
- `rowRoot`: 32-byte Merkle root of rows (tree built directly over row data)
- `rlcOrigRoot`: 32-byte Merkle root of RLC results

### 3.4 Proof Generation

**Input**: Row index i, extended data, commitment

**Process**:

1. **Include Row Data**

   ```text
   proof.row = row[i]
   proof.index = i
   ```

2. **Generate Row Merkle Proof**

   ```text
   treeIndex = MapIndexToTreePosition(i, K)
   proof.rowProof = rowTree.generateProof(treeIndex)
   ```

3. **For Extended Rows (i ≥ K):**
   - **Include Original RLC Results**

     ```text
     for j in 0..K:
         proof.rlcOrig[j] = rlc[j]  // Original K RLC values
     ```

   - **Note**: No additional proof needed. The verifier will extend these K values
     to K+N values if needed, and verify the original K values against the committed `rlcOrigRoot`.

4. **For Original Rows (i < K):**
   - **Generate RLC Merkle Proof**

     ```text
     proof.rlcProof = rlcOrigTree.generateProof(i)  // Same position as in padded tree
     ```

**Output**: Proof containing:

- `index`: Row index
- `row`: Row data (rowSize bytes)
- `rowProof`: Merkle proof for row (log2(K+N) × 32 bytes)
- For extended rows (i ≥ K), where verifier must extend themselves:

  - `rlcOrig`: Original RLC results (K × 16 bytes)
- For original rows (i < K):

  - `rlcProof`: Merkle proof for original RLC result (log2(K) × 32 bytes)

**Implementor's Note on Proof Optimization:**

In practice, proof transmission can be optimized based on the verification context:

1. **When verifier already has `rlcOrig`** (e.g., data availability sampling where verifier downloads `rlcOrig` once and verifies multiple rows):
   - For extended rows: No need to send `rlcOrig` in each proof
   - For original rows: No need to send `rlcProof` (verifier can compute `rlcOrigRoot` from their copy of `rlcOrig`)
   - Only send: `index`, `row`, and `rowProof`

2. **When verifier doesn't have `rlcOrig`** (e.g., single original row read by a rollup):
   - In practice, this case only applies to original rows (rollups read original data, not parity)
   - For original rows: Must include `rlcProof`
   - Send: `index`, `row`, `rowProof`, and `rlcProof`
   - Note: Extended rows cannot be verified without `rlcOrig`, but original data readers don't need them anyway

This optimization can significantly reduce proof sizes, especially for extended rows where `rlcOrig` is K × 16 bytes. Implementations should provide separate proof generation/verification functions for these two contexts to maximize efficiency.

### 3.5 Proof Verification

**Input**: Proof, commitment (32 bytes), parameters

**Process**:

1. **Compute Row Root from Proof**

   ```text
   treeIndex = MapIndexToTreePosition(proof.index, K)
   rowRoot = ComputeRootFromProof(proof.row, treeIndex, proof.rowProof)
   ```

2. **Recompute RLC**

   ```text
   coeffs = DeriveCoefficients(rowRoot, params)  // Same as in 3.3.2
   rlcI = 0
   for c in 0..numChunks:
       chunk = proof.row[c*64..(c+1)*64]
       symbols = ExtractSymbols(chunk)  // See Appendix A.1
       for j in 0..32:
           symbolIndex = c*32 + j
           rlcI += symbols[j] * coeffs[symbolIndex]
   ```

3. **For Original Rows (proof.index < K):**

   ```text
   // Compute RLC root from proof
   rlcBytes = Serialize(rlcI)  // Convert to 16 bytes
   rlcOrigRoot = ComputeRootFromProof(rlcBytes, proof.index, proof.rlcProof)
   ```

4. **For Extended Rows (proof.index ≥ K):**

   ```text
   // Extend original RLC values to verify extended row
   rlcExtended = ExtendRLCResults(proof.rlcOrig, N)
   
   // Verify computed RLC matches extended value
   assert rlcI == rlcExtended[proof.index]
   
   // Compute RLC root from original values
   rlcOrigTree = BuildPaddedRLCTree(proof.rlcOrig, K)
   rlcOrigRoot = rlcOrigTree.root()
   ```

5. **Verify Final Commitment**

   ```text
   // Verify the commitment matches SHA256(rowRoot || rlcOrigRoot)
   computedCommitment = SHA256(rowRoot || rlcOrigRoot)
   assert computedCommitment == commitment
   ```

**Output**: Accept/Reject

## 4. Security Considerations

### 4.1 Soundness

The codec provides:

- **RLC Forgery Resistance**: 128-bit security against forging invalid RLC values (GF(2^128) field size)
- **Commitment Binding**: SHA-256 collision resistance ensures commitment uniqueness
- **Encoding Soundness**: The overall encoding soundness depends on the number of random samples verified by the application layer (out of scope for this library)

### 4.2 Proximity Gap

The Reed-Solomon code provides distance properties:

- Minimum distance: N+1 symbols
- Any K rows can reconstruct the original data
- Detection of up to N errors

### 4.3 Commitment Binding

The commitment is binding due to:

- Collision-resistant hash functions
- Merkle tree structure
- Deterministic coefficient derivation

## 5. Implementation Requirements

### 5.1 Cryptographic Primitives

- SHA-256 hash function
- Binary Merkle tree implementation (RFC 6962 compatible with leaf/inner prefixes)
- Leopard Reed-Solomon codec (or compatible)

### 5.2 Arithmetic Operations

- GF(2^16) multiplication and addition
- GF(2^128) as vector space operations
- Efficient polynomial evaluation

### 5.3 Memory Requirements

- Prover: O(K × rowSize) for data storage
- Verifier for original rows: O(rowSize) for row data + O(log(K+N)) for proofs
- Verifier for extended rows: O(K × 16) for RLC results + O(rowSize) for row data + O(K+N × 16) temporary for tree building
- Proof size for original rows: rowSize + O(log(K+N) × 32) bytes
- Proof size for extended rows: rowSize + K × 16 + O(log(K+N) × 32) bytes

## 6. Test Vectors

### 6.1 Test Vector 1: K=4, N=4, rowSize=64

**Input data** (4 rows × 64 bytes each, all zeros except last byte):

```text
Row 0: 0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000001
Row 1: 0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000002
Row 2: 0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000003
Row 3: 0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000004
```

**Expected commitment**:

```text
0xf57fdff87d54f71bc0c860808b046356c8d4850e67b923e08411208df08cb5ab
```

### 6.2 Test Vector 2: K=3, N=9, rowSize=256

**Input data** (3 rows × 256 bytes each, all zeros except last byte):

```text
Row 0: 0x00...(255 zero bytes)...01
Row 1: 0x00...(255 zero bytes)...02
Row 2: 0x00...(255 zero bytes)...03
```

**Expected commitment**:

```text
0x0e0b5f2a0b8e9ef09fbd70256b4c346291450ef239fd98894d177b5f32c579ab
```

### 6.3 Verification Test Cases

1. **Valid Original Row**: Proof for row 0 should verify
2. **Valid Parity Row**: Proof for row 4 should verify
3. **Invalid Row Data**: Modified row should fail verification
4. **Invalid RLC Result**: Modified rlcOrig should fail verification
5. **Wrong Commitment**: Different commitment should fail

## 7. References

- Leopard Reed-Solomon: <https://github.com/catid/leopard>
- EMA Paper: <https://eprint.iacr.org/2025/034>
- GF(2^16) arithmetic: Leopard field specification
- SHA-256: FIPS 180-4
- Merkle Trees: RFC 6962 (adapted for binary trees)

## Appendix A: Leopard-Specific Operations

### A.1 Symbol Extraction from Leopard Share

Leopard uses an interleaved format for GF(2^16) symbols within 64-byte chunks:

```text
ExtractSymbols(chunk[64]): // Returns 32 GF(2^16) symbols
    for i in 0..32:
        symbol[i] = (chunk[32+i] << 8) | chunk[i]
    return symbol[0..32]
```

Each 64-byte chunk independently contains 32 symbols. For rows larger than 64 bytes, each chunk is processed separately.

### A.2 Visual Representation of Leopard Format

**64-byte chunk format:**

```text
┌─────────────────── 64-byte Leopard chunk ─────────────────────┐
│                                                               │
│  Bytes 0-31: Low bytes of 32 GF(2^16) symbols                 │
│  [L₀][L₁][L₂]...[L₃₁]                                         │
│                                                               │
│  Bytes 32-63: High bytes of 32 GF(2^16) symbols               │
│  [H₀][H₁][H₂]...[H₃₁]                                         │
│                                                               │
└───────────────────────────────────────────────────────────────┘

Each GF(2^16) symbol i = (Hᵢ << 8) | Lᵢ
```

**256-byte row (4 chunks):**

```text
┌─────────────────── 256-byte Leopard row ──────────────────────┐
│                                                               │
│  Chunk 0 (bytes 0-63):                                        │
│    ├─ Bytes 0-31:   Low bytes of symbols 0-31                 │
│    └─ Bytes 32-63:  High bytes of symbols 0-31                │
│                                                               │
│  Chunk 1 (bytes 64-127):                                      │
│    ├─ Bytes 64-95:  Low bytes of symbols 32-63                │
│    └─ Bytes 96-127: High bytes of symbols 32-63               │
│                                                               │
│  Chunk 2 (bytes 128-191):                                     │
│    ├─ Bytes 128-159: Low bytes of symbols 64-95               │
│    └─ Bytes 160-191: High bytes of symbols 64-95              │
│                                                               │
│  Chunk 3 (bytes 192-255):                                     │
│    ├─ Bytes 192-223: Low bytes of symbols 96-127              │
│    └─ Bytes 224-255: High bytes of symbols 96-127             │
│                                                               │
└───────────────────────────────────────────────────────────────┘
```

## Appendix B: Serialization Formats

### B.1 GF128 Serialization

16 bytes, little-endian encoding of 8 uint16 limbs:

```text
bytes[0:2]   = limb[0] (little-endian uint16)
bytes[2:4]   = limb[1] (little-endian uint16)
...
bytes[14:16] = limb[7] (little-endian uint16)
```

### B.2 GF128 Packing for Leopard Extension

When extending RLC results using Reed-Solomon, GF128 values must be packed into Leopard's interleaved format:

**PackGF128ToLeopard**: Converts GF128 to 64-byte Leopard shard

```text
Input: GF128 value (8 GF16 symbols)
Output: 64-byte Leopard-formatted shard

// Place 8 GF16 symbols in first 8 positions, zero-pad remaining 24
for i in 0..8:
    shard[i] = symbol[i] & 0xFF         // Low byte
    shard[32+i] = symbol[i] >> 8        // High byte
for i in 8..32:
    shard[i] = 0                        // Zero padding (low bytes)
    shard[32+i] = 0                     // Zero padding (high bytes)
```

**UnpackGF128FromLeopard**: Extracts GF128 from 64-byte Leopard shard

```text
Input: 64-byte Leopard-formatted shard
Output: GF128 value (8 GF16 symbols)

for i in 0..8:
    symbol[i] = (shard[32+i] << 8) | shard[i]
```

This packing ensures proper Reed-Solomon encoding of RLC values while respecting Leopard's interleaved symbol format.

### B.3 Merkle Tree Construction

- Binary tree with power-of-2 leaves (RFC 6962 compatible)
- Leaf nodes: SHA256(0x00 || leafData) - prefix byte ensures domain separation
- Internal nodes: SHA256(0x01 || left || right) - different prefix for internal nodes
- This format matches RFC 6962 and is compatible with CometBFT/Celestia-core

### B.4 Proof Serialization

Recommended format (implementers may choose alternatives):

For original rows (index < K):

```text
[4 bytes]    index (uint32, little-endian)
[4 bytes]    rowSize (uint32, little-endian)
[rowSize]    row data
[4 bytes]    rowProofLen (uint32, little-endian)
[variable]   rowProof (concatenated 32-byte hashes)
[4 bytes]    rlcProofLen (uint32, little-endian)
[variable]   rlcProof (concatenated 32-byte hashes)
```

For extended rows (index ≥ K):

```text
[4 bytes]    index (uint32, little-endian)
[4 bytes]    rowSize (uint32, little-endian)
[rowSize]    row data
[4 bytes]    rowProofLen (uint32, little-endian)
[variable]   rowProof (concatenated 32-byte hashes)
[K × 16]     rlcOrig (serialized GF128 values)
```

## Appendix C: Bulk Data Read Paths

The codec supports two primary read patterns for committed data:

### C.1 Single Row Reading (Random Sampling)

This is the standard proof generation and verification as described in sections 3.4 and 3.5. Used for:

- Light client sampling
- Individual row verification
- Spot checking data availability

Each row is proven independently with its own Merkle proofs.

### C.2 Full Original Data Reading (Bulk Download)

For applications that need to retrieve all K original rows (e.g., rollups downloading the entire block), a more efficient approach uses subtree proofs:

#### C.2.1 Bulk Proof Generation

**Input**: Extended data, commitment

**Process**:

1. **Include All Original Row Data**

   ```text
   bulkProof.rowsOrig = rows[0..K]  // All K original rows
   ```

1. **Generate Row Original Subtree Proof**
   Since K is a power of 2, the first K rows form a complete binary subtree when building the merkle tree.
   The proof contains sibling subtree roots needed to verify that the K-row subtree
   is part of the full (K+N)-row tree.

   ```text
   bulkProof.rowOrigProof = GenerateLeftSubtreeProof(rowTree, K)
   ```

**Output**: Bulk proof containing:

- `rowsOrig`: All K original rows (K × rowSize bytes)
- `rowOrigProof`: Sibling roots to prove K-row subtree is in (K+N)-row tree (≤ log2(K+N) × 32 bytes)

#### C.2.2 Bulk Proof Verification

**Input**: Bulk proof, commitment, parameters

**Process**:

1. **Compute Row Original Subtree Root**

   ```text
   rowOrigTree = BuildPaddedRowTree(rowOrig, K)
   rowOrigRoot = rowOrigTree.root()
   ```

2. **Verify Row Original Subtree is Part of Full Tree**

   ```text
   rowRoot = ComputeRootFromLeftSubtreeProof(rowOrigRoot, bulkProof.rowOrigProof)
   ```

3. **Derive Coefficients and Compute All Original RLCs**

   ```text
   coeffs = DeriveCoefficients(rowRoot, params)
   for i in 0..K:
       rlcOrig[i] = ComputeRLC(bulkProof.rowsOrig[i], coeffs)
   ```

4. **Compute Original RLC Root**

   ```text
   rlcOrigTree = BuildPaddedRLCTree(rlcOrig, K)
   rlcOrigRoot = rlcOrigTree.root()
   ```

5. **Verify Final Commitment**

   ```text
   computedCommitment = SHA256(rowRoot || rlcOrigRoot)
   assert computedCommitment == commitment
   ```

**Output**: Accept/Reject + all K original rows if accepted

#### C.2.3 Efficiency Comparison

**Single Row Proofs (K times):**

- Data transmitted: K × rowSize (row data) + K × 2 × log2(K+N) × 32 (Merkle proofs)
- Verification ops: K × (2 × log2(K+N) hash operations + 1 RLC computation)

**Bulk Proof (once):**

- Data transmitted: K × rowSize (row data) + 2 × log2(N) × 32 (subtree proofs)
- Verification ops: 2K hashes (tree building) + K RLC computations + 2 × log2(N) hash operations

For typical parameters (K=32768, N=32768):

- Single proofs: ~32768 × 32 × 32 = 32MB of proof data
- Bulk proof: ~512 bytes of proof data only

The bulk approach is significantly more efficient for downloading all original data, which is common for rollup operators and full nodes.

### C.3 Left Subtree Proof Functions

These functions support the bulk data reading operations described above.

#### C.3.1 GenerateLeftSubtreeProof

Generates a proof that the leftmost K leaves are part of a larger padded tree.

**Input**:

- `tree`: Padded Merkle tree with total_padded leaves
- `K`: Number of actual data leaves in left subtree (can be arbitrary)

**Algorithm**:

```text
GenerateLeftSubtreeProof(tree, K):
    K_padded = nextPowerOfTwo(K)
    proof = []
    currentSize = K_padded  // Start from padded K

    while currentSize < totalLeaves:
        // Compute root of sibling subtree
        siblingRoot = tree.getSubtreeRoot(currentSize, currentSize*2)
        proof.append(siblingRoot)
        currentSize = currentSize * 2

    return proof
```

Note: Since the tree is padded to K_padded, the proof starts from the K_padded subtree boundary.

### Example 1: K=4, N=4 (8 total leaves)

```text
                     Root_0-7
                    /        \
                   /          \
             Root_0-3        Root_4-7  ← Include in proof
             /      \        /      \
           /          \    /          \
       Root_0-1  Root_2-3 Root_4-5  Root_6-7
        /   \     /   \    /   \     /   \
      L0    L1  L2    L3  L4   L5   L6   L7
      └──────┬──────┘    └──────┬──────┘
         K original         N extended
```

Returns: [Root_4-7]

### Example 2: K=4, N=12 (16 total leaves)

```text
                              Root_0-15
                            /            \
                          /                \
                    Root_0-7              Root_8-15  ← Include in proof[1]
                   /        \              /        \
                 /            \          /            \
           Root_0-3        Root_4-7    Root_8-11   Root_12-15
                           ↑ Include in proof[0]
           /      \        /      \
         /          \    /          \
     Root_0-1  Root_2-3 Root_4-5  Root_6-7
      /   \     /   \
    L0    L1  L2    L3
    └──────┬──────┘
       K original
```

Returns: [Root_4-7, Root_8-15]

#### C.3.2 ComputeRootFromLeftSubtreeProof

Computes the full tree root given a left subtree root and sibling subtree roots.

**Input**:

- `leftSubtreeRoot`: Root of the leftmost K leaves
- `siblingRoots`: Array of sibling subtree roots from GenerateLeftSubtreeProof

**Algorithm**:

```text
ComputeRootFromLeftSubtreeProof(leftSubtreeRoot, siblingRoots):
    currentRoot = leftSubtreeRoot

    for siblingRoot in siblingRoots:
        // Left subtree is always on the left, sibling on the right
        currentRoot = SHA256(0x01 || currentRoot || siblingRoot)

    return currentRoot
```

**Visual Example (K=4, N=12)**:

```text
Initial state:
    leftSubtreeRoot = Root_0-3
    siblingRoots = [Root_4-7, Root_8-15]

Step 1: Combine Root_0-3 with Root_4-7
                Root_0-7
               /        \
         Root_0-3    Root_4-7
         (given)     (proof[0])

Step 2: Combine Root_0-7 with Root_8-15
                Root_0-15
               /          \
         Root_0-7      Root_8-15
         (step 1)      (proof[1])

Final: Root_0-15
```
