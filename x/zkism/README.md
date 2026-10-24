# `x/zkism`

## Abstract

The `x/zkism` module implements a Hyperlane Interchain Security Module (ISM) that authorizes Hyperlane message processing verified by zero-knowledge proofs. Each ISM stores a single opaque `state []byte` along with SP1 Groth16 verifier configuration; the first 32 bytes of the state are treated as the trusted state root for membership proofs, while the remainder is circuit-defined.

The module currently supports the following proof value types:

- `StateTransitionValues`: State transition (`state -> new_state`), stored as opaque bytes.
- `StateMembershipValues`: State membership of the source chain Hyperlane merkle tree (root and leaf count) at `merkle_tree_address`.

The module integrates with Hyperlane core via the keeper’s `Verify` method. This is the integration point used by the Hyperlane ISM routing mechanism.
The `Verify` method checks a merkle proof of the message ID against the stored merkle tree root.

## Concepts

- ISM: On-chain record tracking a remote chain’s trusted state (opaque bytes) and the ZK verifier configuration used to validate proof statements.
- SP1 Groth16 Proofs: Proofs are verified against the SP1 Groth16 verifier key, providing an SP1 program verifier key commitment and public values as public witness.
- Merkle Tree State: The source chain merkle tree root and leaf count most recently proven for an ISM. Relayers prove inclusion of each message against it when delivering.

Users can define any state and write circuits that encapsulate a transition from State -> New State. The circuit must output the raw bytes of `state_length_u64_as_little_endian_bytes || state || new_state`. The first 32 bytes of `state` are assumed to be the state root used when checking Hyperlane membership proofs; the module does not interpret or persist any additional fields such as heights.

## Module State

The `x/zkism` module defines the following collections used for storage of on-chain state.

- `isms`: Stores per-ISM records. Each record contains `id`, `owner`, `state` (opaque bytes, first 32 bytes used as the trusted root), `merkle_tree_address` and the SP1 Groth16 verifying key configurations. `(collections.Map[uint64, InterchainSecurityModule], types.IsmsKeyPrefix)`.
- `submissions`: Whether a membership proof has been submitted for the current state root. `(collections.Map[uint64, bool], types.MessageProofSubmittedPrefix)`.
- `merkle_tree_states`: The proven source chain merkle tree root and leaf count per ISM, overwritten by each `SubmitMessages`. `(collections.Map[uint64, MerkleTreeState], types.MerkleTreeStateKeyPrefix)`.

## Messages (Tx RPCs)

Protobuf definitions: [`proto/celestia/zkism/v1/tx.proto`](../../proto/celestia/zkism/v1/tx.proto)

- `CreateInterchainSecurityModule`: Creates an ISM with initial trusted state bytes and verifier configuration.
- `UpdateInterchainSecurityModule`: Verifies a state transition proof against the stored state and replaces `state` with the provided `new_state` (opaque bytes). Both states must be at least 32 bytes; no height is stored.
- `UpdateVerifyingKeys`: Replaces the Groth16 verifying key and both program verifying key commitments of an ISM. Only the ISM owner can submit it. Resets the submission flag so the new membership program can submit for the current state root.
- `SubmitMessages`: Verifies a state membership proof and stores the proven source chain merkle tree root and leaf count. The proof must bind to the stored state root (`state[:32]`). The public values are a fixed 104 bytes: `state_root (32) || merkle_tree_address (32) || tree_root (32) || count (u64, little-endian)`, with `count` at most `2^32 - 1`.

## SP1 Groth16 Verifier

Purpose: Verifies SP1 Groth16 proofs for any SP1 program by binding proofs to a specific verifying key and checking the program commitment and public values.

```golang
// SP1Groth16Verifier encapsulates the state required to verify Groth16 proofs
// under the SP1 scheme. It stores a verifying key and its hash prefix, which
// are used to check proof integrity and correctness.
type SP1Groth16Verifier struct {
	prefix [PrefixLen]byte
	vk     groth16.VerifyingKey
}
```

### SP1 Proof Format

- Total size: 260 bytes = 4-byte prefix + 256-byte Groth16 proof payload.
- The proof prefix must equal `sha256(vkBytes)[:PrefixLen]`, otherwise `ErrInvalidProofPrefix` is returned.
- An invalid proof length results in `ErrInvalidProofLength`.

### Public Witness Construction

In zk-SNARK systems, 𝔽ᵣ denotes the scalar field of the curve in use, i.e. the finite field of order equal to the curve's group order, for example in BN254 `r` (≈2²⁵⁴).
The function `Fr()` reduces a byte string into an element of this field.

- `vk_element = Fr(program_vk_commitment)` where `program_vk_commitment` is a 32-byte commitment for the specific SP1 program.
- `inputs_element = Fr(HashBN254(public_values_bytes))` where `HashBN254` is `sha256(public_values)`, with the top 3 bits masked and the result interpreted as a scalar in 𝔽ᵣ.
- The public witness is `[vk_element, inputs_element]` (see `groth16.NewPublicWitness`).

### Verification

The `SP1Groth16Verifier` leverages `github.com/consensys/gnark` for Groth16 proof verification. All invocations to the library are encapsulated within the `internal/groth16` package of this module.

```golang
func VerifyProof(proofBz, programVk, publicValues []byte) error
```

The `VerifyProof` method does the following:

- Validate length and prefix.
- Deserialize Groth16 proof from `proofBz[PrefixLen:]`.
- Compute `vk_element` and `inputs_element` from `programVk` and `publicValues`.
- Build a public witness and call gnark `groth16.Verify`.
- On failure, returns `ErrInvalidProof` (wrapped with the underlying error).

Reusability across SP1 programs:

- The `SP1Groth16Verifier` can verify any SP1 Groth16 program as long as you supply the correct pair `(program_vk_commitment, public_values_bytes)` for that program and the proof that was produced with the same verifying key (`vkBytes`).
- Program-specific logic lives in how you construct `public_values_bytes` off-chain; on-chain the verifier treats it as an opaque byte string that is hashed into the field.

### Public Values Encoding

- The verifier treats public values as opaque bytes that are hashed into a BN254 field element for verification. For SP1 programs, payloads are bincode‑encoded in rust using the default configuration: (little-endian, fixed-width integers, length-prefixed slices).
- Invariants checked before proof verification ensure consistency with on‑chain state, and thus must be decodable by the module in order to consume the proof values and validate them against a trusted anchor.

## Hyperlane Integration - Message Processing

- Hyperlane core calls `Verify(ism_id, metadata, message)`.
- The metadata must be exactly 1028 bytes: `leaf_index (u32, big-endian) || branch (32 × 32 bytes)`. Malformed metadata returns an error.
- The method returns `true` if `leaf_index` is below the stored leaf count and the branch proves `message.Id()` at `leaf_index` against the stored merkle tree root. Otherwise it returns `false` and Hyperlane core must not process the message.
- Replay protection is enforced by the Hyperlane mailbox, which rejects message IDs it has already received.

Thus, the expected flow is:

1) Off-chain prover generates a state transition proof; submit via `UpdateInterchainSecurityModule`.
2) Off-chain prover generates a membership proof of the source chain merkle tree root; submit via `SubmitMessages`.
3) Relayers deliver each message with a merkle proof against the latest proven root; Hyperlane core invokes `Verify` per message.

## Queries (gRPC/REST)

Protobuf definitions: [`proto/celestia/zkism/v1/query.proto`](../../proto/celestia/zkism/v1/query.proto)

- `Ism(id) -> InterchainSecurityModule` — `GET /celestia/zkism/v1/isms/{id}`
- `Isms(pagination) -> [InterchainSecurityModule]` — `GET /celestia/zkism/v1/isms`
- `MerkleTreeState(id) -> MerkleTreeState` — `GET /celestia/zkism/v1/isms/{id}/merkle_tree_state`

## Events

- `EventCreateInterchainSecurityModule` emitted on creation with all ISM fields.
- `EventUpdateInterchainSecurityModule` emitted when the stored state is replaced via a transition proof.
- `EventUpdateVerifyingKeys` emitted when the owner replaces the verifying keys.
- `EventSubmitMessages` emitted when a membership proof is accepted (includes the state root, merkle tree root and leaf count).

## Security Considerations

### Threat Model

The `x/zkism` module is designed under the assumption that:

- The on-chain verifier (`SP1Groth16Verifier`) is correct, deterministic, and cannot be subverted by malformed proofs.
- Cryptographic primitives (BN254 pairing operations, SHA-256, Groth16) are secure under their standard hardness assumptions.
- The off-chain prover is honest but untrusted: the verifier must reject any invalid or malformed proof.
- Hyperlane core will only process messages that the module explicitly authorizes.

Potential adversaries include:

- Malicious actors attempting to forge proofs or replay stale proofs.
- Entities submitting malformed or oversized inputs to trigger denial-of-service conditions.
- Adversaries attempting to exploit prover liveness delays to censor or stall message authorization.

### Security Assumptions

- Correctness of gnark's Groth16 implementation and its integration within the module.
- Collision resistance of SHA-256, used for both verifying key prefixes and hashing public values into field elements.
- Off-chain SP1 program correctness: the verifier assumes public values were produced correctly by the corresponding SP1 program, including embedding the correct state root in the first 32 bytes.
- ISM owner honesty: the owner can replace the verifying keys at any time, and therefore the programs whose proofs the ISM accepts. Anyone relying on an ISM must trust its owner.

### Invariants

The module enforces the following invariants:

- Proof prefix must match the expected verifying key hash prefix.
- Proof length must be exactly 256 bytes + the 4-byte prefix, equalling a total of 260 bytes.
- State transition proofs must include the currently stored state (minimum 32 bytes) and replace it entirely with `new_state` (minimum 32 bytes).
- Membership proofs must bind to the trusted root stored in `ism.State[:32]` and to the ISM's `merkle_tree_address`.
- Submitting a membership proof costs a constant amount of state and event data, independent of the number of messages it covers.
- Replay protection is enforced by the [Hyperlane Mailbox](https://docs.hyperlane.xyz/docs/protocol/core/mailbox) configured with the ISM.

### Liveness and Availability Risks

- If the off-chain prover is delayed or unavailable, message authorization halts. Hyperlane will be unable to process new messages until valid proofs are submitted.
- The system does not guarantee liveness independently; availability is contingent on timely prover operation and proof relay.
- While delayed proofs cannot compromise safety (invalid messages will not be authorized), they may result in service degradation if provers are prevented from submitting in time.
- Mitigation relies on redundant or decentralized prover infrastructure to reduce single points of failure.

### Additional Considerations

- Future extension to support multiple proof systems will require additional work and modification to this module.
