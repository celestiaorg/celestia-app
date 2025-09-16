# Visual Representations in Share Tests

This document explains the ASCII art visual representations added to share splitting tests in response to [issue #1789](https://github.com/celestiaorg/celestia-app/issues/1789).

## Overview

Visual representations have been added to test files to illustrate how data is organized within shares, making the tests more understandable and maintainable. These diagrams follow a consistent format inspired by the documentation in the `specs/` directory.

## Locations of Visual Representations

### 1. `x/blob/types/payforblob_test.go`

#### TestNewMsgPayForBlobs
Shows how blobs are organized into shares for PayForBlobs transactions:

```
Share Structure for Small Blob (fits in single share):
┌──────────────────────────────────────────────────────────────────────────────────┐
│  0   │  1   │  29  │  30  │  34  │                    512                        │
├──────┼──────┼──────┼──────┼──────┼───────────────────────────────────────────────┤
│ ns_v │ ns_id       │ info │ len  │ blob_data... (padded with 0s if needed)        │
└──────┴──────┴──────┴──────┴──────┴───────────────────────────────────────────────┘
```

#### TestValidateBlobs
Demonstrates namespace restrictions and valid blob share layout:

```
Valid Blob Share Layout:
┌──────────────────────────────────────────────────────────────────────────────────┐
│     Namespace (29 bytes)      │Info│ Len │           Blob Data                   │
├─────────┬─────────────────────┼────┼─────┼───────────────────────────────────────┤
│ Version │     ID (28 bytes)   │0x01│ len │ user_data... (padded to 512 bytes)    │
└─────────┴─────────────────────┴────┴─────┴───────────────────────────────────────┘
```

### 2. `x/blob/types/blob_tx_test.go`

#### TestValidateBlobTx
Shows the BlobTx structure and data square layout:

```
Data Square Layout (example with 2x2 square):
┌─────────────────┬─────────────────┐
│   Tx Share      │   Blob Share 1  │  <- Original data
│ (PFB message)   │ (User namespace)│
├─────────────────┼─────────────────┤
│   Blob Share 2  │   Parity Data   │
│ (User namespace)│                 │
└─────────────────┴─────────────────┘
```

### 3. `x/blob/ante/blob_share_decorator_test.go`

#### TestBlobShareDecorator
Demonstrates share calculations and data square organization:

```
Share Calculation Example (64x64 square = 4096 total shares):
┌─────────────────────────────────────────────────────────────────────────────────┐
│                        64x64 Data Square (4096 shares)                         │
├─────────────────────────────────────────────────────────────────────────────────┤
│  Transaction Shares  │                  Blob Shares                            │
│  (compact format)    │              (sparse format)                           │
```

### 4. `pkg/proof/share_proof_test.go`

#### TestShareProofValidate
Illustrates share inclusion proof structure:

```
Share Proof Structure:
┌─────────────────────────────────────────────────────────────────────────────────┐
│                            Share Inclusion Proof                               │
├─────────────────────────────────────────────────────────────────────────────────┤
│  ┌─────────────────┐    ┌─────────────────┐    ┌─────────────────────────────┐ │
│  │   Share Data    │    │   Row Proof     │    │      Namespace Info         │ │
```

### 5. `pkg/da/data_availability_header_test.go`

#### TestExtendShares
Shows Reed-Solomon encoding process:

```
Extended 4x4 Data Square (after Reed-Solomon):
┌─────────────────┬─────────────────┬─────────────────┬─────────────────┐
│   Share 1       │   Share 2       │  Parity 1-1     │  Parity 1-2     │
│ (tx data)       │ (blob data)     │ (row parity)    │ (row parity)    │
├─────────────────┼─────────────────┼─────────────────┼─────────────────┤
│   Share 3       │   Share 4       │  Parity 2-1     │  Parity 2-2     │
│ (blob data)     │ (padding)       │ (row parity)    │ (row parity)    │
└─────────────────┴─────────────────┴─────────────────┴─────────────────┘
```

## Key Concepts Illustrated

### Share Format
- **Namespace Version**: 1 byte at position 0
- **Namespace ID**: 28 bytes at positions 1-28
- **Share Info**: 1 byte at position 29 (0x01 = start, 0x00 = continuation)
- **Sequence Length**: 4 bytes at positions 30-33 (only in start shares)
- **Data**: Remaining bytes (478 for first share, 482 for continuation)

### Share Splitting
- Small blobs (≤478 bytes) fit in a single share
- Large blobs span multiple shares with continuation format
- ShareVersion 1 includes signer information

### Data Square Layout
- Original data in top-left quadrant
- Reed-Solomon parity shares in remaining quadrants
- Allows recovery of any missing quadrant

### Reserved Namespaces
Visual representations highlight which namespaces are reserved and cannot be used for user blobs.

## Style Guidelines

The visual representations follow these conventions:

1. **Box Drawing Characters**: Use Unicode box drawing characters (┌┐└┘├┤┬┴┼─│)
2. **Consistent Width**: Maintain consistent column widths for readability
3. **Clear Labels**: Include descriptive labels for each section
4. **Byte Positions**: Show exact byte positions where relevant
5. **Explanatory Text**: Include explanations below diagrams when helpful

## Benefits

These visual representations provide several benefits:

1. **Improved Understanding**: Make complex share structures easier to comprehend
2. **Better Maintenance**: Help developers understand what tests are validating
3. **Documentation**: Serve as inline documentation for share formats
4. **Debugging**: Aid in debugging share-related issues
5. **Onboarding**: Help new developers understand Celestia's data structures

## Future Enhancements

Consider adding visual representations to other areas:

- Transaction encoding/decoding tests
- Namespace merkle tree construction
- Subtree root calculations
- Commitment generation processes

The visual representation format established here can be extended to other parts of the codebase where complex data structures benefit from visual explanation.